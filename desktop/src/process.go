package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const freePortScanLimit = 100

type noFreePortError struct {
	From int
	To   int
}

func (e noFreePortError) Error() string {
	return fmt.Sprintf("no free port from %d to %d", e.From, e.To)
}

type cannotBindError struct {
	Host string
	Port int
	Err  error
}

func (e cannotBindError) Error() string {
	return e.Err.Error()
}

func (e cannotBindError) Unwrap() error { return e.Err }

// portOccupiedError means another program owns the configured port. We refuse
// to move to the next port silently: a different port is a different origin,
// which throws away the dashboard login (localStorage is origin-scoped).
type portOccupiedError struct {
	Port int
}

func (e portOccupiedError) Error() string {
	return fmt.Sprintf("port %d is in use by another program", e.Port)
}

func mustEnv(cmd *exec.Cmd, extra map[string]string) {
	cmd.Env = overlayEnv(os.Environ(), extra)
}

func overlayEnv(base []string, extra map[string]string) []string {
	out := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := extra[key]; replaced {
				continue
			}
		}
		out = append(out, entry)
	}
	for key, value := range extra {
		out = append(out, key+"="+value)
	}
	return out
}

func startOctop(root string, port int) (*exec.Cmd, error) {
	py := pythonExe(root)
	launch := filepath.Join(root, "launch.py")
	// Omit --host/--port. `octop run` writes those flags back to config.json.
	// OCTOP_PORT selects the free port for this process and is not saved.
	cmd := exec.Command(py, launch, "run")
	cmd.Dir = root
	issuer := strings.TrimSpace(os.Getenv("OCTOP_MAMMOTH_OIDC_ISSUER"))
	if issuer == "" {
		issuer = "https://www.mmai.vip/console/api/oidc"
	}
	mustEnv(cmd, map[string]string{
		"OCTOP_HOME":                octopHome(),
		"OCTOP_GREEN_PACKAGES":      filepath.Join(root, "packages"),
		"OCTOP_PORT":                strconv.Itoa(port),
		"OCTOP_MAMMOTH_OIDC_ISSUER": issuer,
		"PYTHONNOUSERSITE":          "1",
		"PYTHONPATH":                "",
	})
	configureProcGroup(cmd)
	if runtime.GOOS == "linux" {
		// The Linux desktop release has no server terminal; the shell owns status
		// presentation just like the Windows GUI executable.
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	} else if runtime.GOOS != "windows" {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopOctop(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	killProcessTree(cmd)
}

const desktopPortFileName = "desktop-port"

func desktopPortPath() string {
	return filepath.Join(octopHome(), desktopPortFileName)
}

func readDesktopPort() (int, bool) {
	data, err := os.ReadFile(desktopPortPath())
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

func writeDesktopPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid desktop port %d", port)
	}
	return os.WriteFile(desktopPortPath(), []byte(strconv.Itoa(port)), 0o644)
}

func clearDesktopPort() {
	_ = os.Remove(desktopPortPath())
}

// findRunningDashboard returns a live Octop URL. The saved port wins. When that
// port is not Octop, the port this shell last bound (desktop-port) is checked
// so a second launch attaches instead of starting another server.
func findRunningDashboard(host string, preferred int) string {
	preferredURL := dashboardURL(host, preferred)
	if octopAlreadyRunning(preferredURL) {
		return preferredURL
	}
	port, ok := readDesktopPort()
	if !ok || port == preferred {
		return ""
	}
	remembered := dashboardURL(host, port)
	if octopAlreadyRunning(remembered) {
		return remembered
	}
	return ""
}

func resolveDashboardURL() string {
	host, port := resolveListen()
	return dashboardURL(host, port)
}

func resolveListen() (string, int) {
	host := "127.0.0.1"
	port := 8088
	if data, err := os.ReadFile(filepath.Join(octopHome(), "config.json")); err == nil {
		var raw struct {
			BindHost string `json:"bind_host"`
			Host     string `json:"host"`
			Port     *int   `json:"port"`
		}
		if json.Unmarshal(data, &raw) == nil {
			switch {
			case raw.BindHost != "":
				host = raw.BindHost
			case raw.Host != "":
				host = raw.Host
			}
			if raw.Port != nil && *raw.Port > 0 && *raw.Port <= 65535 {
				port = *raw.Port
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("OCTOP_BIND_HOST")); v != "" {
		host = v
	}
	if v := strings.TrimSpace(os.Getenv("OCTOP_PORT")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 65535 {
			port = parsed
		}
	}
	return host, port
}

func chooseFreePort(host string, start int) (int, error) {
	return firstFreePort(start, func(port int) (bool, error) {
		inUse, err := listenProbe(host, port)
		if err != nil {
			return false, cannotBindError{Host: probeHost(host), Port: port, Err: err}
		}
		return inUse, nil
	})
}

// chooseStartPort prefers the configured port. When another program owns it we
// stop instead of silently shifting to 8089+: the dashboard login lives in
// localStorage, which is keyed by origin, so a port change signs the user out.
// “allowAlternate“ is set by the explicit "start on another port" button (and
// by the update restart, which must not be blocked by a port conflict).
func chooseStartPort(host string, preferred int, allowAlternate bool) (int, error) {
	inUse, err := listenProbe(host, preferred)
	if err != nil {
		return 0, cannotBindError{Host: probeHost(host), Port: preferred, Err: err}
	}
	if !inUse {
		return preferred, nil
	}
	if !allowAlternate {
		return 0, portOccupiedError{Port: preferred}
	}
	return chooseFreePort(host, preferred+1)
}

func firstFreePort(start int, probe func(int) (bool, error)) (int, error) {
	if start < 1 || start > 65535 {
		start = 8088
	}
	last := start + freePortScanLimit - 1
	if last > 65535 {
		last = 65535
	}
	for port := start; port <= last; port++ {
		inUse, err := probe(port)
		if err != nil {
			return 0, err
		}
		if !inUse {
			return port, nil
		}
	}
	return 0, noFreePortError{From: start, To: last}
}

func listenProbe(host string, port int) (bool, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(probeHost(host), strconv.Itoa(port)))
	if err == nil {
		_ = ln.Close()
		return false, nil
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true, nil
	}
	return false, err
}

type octopHealthPayload struct {
	OK            bool            `json:"ok"`
	UsersLoaded   json.RawMessage `json:"users_loaded"`
	AgentsRunning json.RawMessage `json:"agents_running"`
	Version       string          `json:"version"`
}

// probeOctopHealth is strict on purpose (unlike waitHealth in download.go):
// only a fully initialized 2xx + {ok, users_loaded, agents_running} payload
// counts as "already running" — this decides single-instance takeover, so a
// half-started or foreign service on the port must not be mistaken for us.
func probeOctopHealth(base string) (octopHealthPayload, bool) {
	var payload octopHealthPayload
	client := &http.Client{Timeout: 800 * time.Millisecond}
	url := strings.TrimRight(base, "/") + "/api/health"
	resp, err := client.Get(url)
	if err != nil {
		return payload, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return payload, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return payload, false
	}
	if json.Unmarshal(body, &payload) != nil {
		return payload, false
	}
	if !payload.OK || len(payload.UsersLoaded) == 0 || len(payload.AgentsRunning) == 0 {
		return payload, false
	}
	return payload, true
}

func octopAlreadyRunning(base string) bool {
	_, ok := probeOctopHealth(base)
	return ok
}

// dashboardVersion returns the runtime version reported by /api/health, or ""
// when the instance predates the field (1.0.x) or the probe fails.
func dashboardVersion(base string) string {
	payload, ok := probeOctopHealth(base)
	if !ok {
		return ""
	}
	return strings.TrimSpace(payload.Version)
}

// dashboardIsCurrent reports whether the instance at base may be reused for
// this portable tree. A missing version means an old build (stale), an older
// version means the portable was upgraded under a running process (stale), and
// a newer-or-equal version means the process is fine (in-app PyPI bump keeps
// the running server).
func dashboardIsCurrent(base, wantVersion string) bool {
	if strings.TrimSpace(wantVersion) == "" {
		return true // cannot judge from disk; keep the previous behaviour
	}
	got := dashboardVersion(base)
	if got == "" {
		return false
	}
	return compareVersions(got, wantVersion) >= 0
}

// portFromDashboardURL extracts the port used by a running dashboard URL.
func portFromDashboardURL(base string) (int, bool) {
	parsed, err := url.Parse(base)
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

func probeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	if host == "" {
		return "127.0.0.1"
	}
	return host
}

func dashboardURL(host string, port int) string {
	switch host {
	case "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%d/", host, port)
}

// stopListeningOctop terminates processes on host:port that provably belong to
// this user's Mammoth install: either a healthy Octop (strict /api/health) or a
// python started from ~/.octop/portable/runtime. The second case matters —
// a half-started 1.0.x leftover never answers /api/health, so the old check
// could not kill it and the shell ended up running 8088 + 8089 side by side.
// Foreign programs on the port are never touched.
func stopListeningOctop(host string, port int) {
	if port < 1 || port > 65535 {
		return
	}
	pids := listeningPIDs(port)
	if len(pids) == 0 {
		return
	}
	healthy := octopAlreadyRunning(dashboardURL(host, port))
	root := portableDir()
	killed := 0
	for _, pid := range pids {
		if !healthy && !processRunsFrom(pid, root) {
			continue
		}
		terminatePID(pid)
		killed++
	}
	if killed == 0 {
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		inUse, err := listenProbe(host, port)
		if err != nil || !inUse {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// processRunsFrom reports whether pid was started from the given directory.
// Used to recognise our own portable python even when it is not serving yet.
func processRunsFrom(pid int, dir string) bool {
	if pid <= 0 {
		return false
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	command := strings.ToLower(processCommandLine(pid))
	if command == "" {
		return false
	}
	needle := strings.ToLower(filepath.Clean(dir))
	return strings.Contains(command, needle+strings.ToLower(string(os.PathSeparator))) ||
		strings.Contains(command, needle+"/") ||
		strings.Contains(command, needle+"\\")
}

// portRunsFrom reports whether every process listening on port was started
// from dir. Used before cleaning one of our own candidate ports so a service
// that merely happens to be an Octop (docker proxy, dev checkout) is left alone.
func portRunsFrom(port int, dir string) bool {
	pids := listeningPIDs(port)
	if len(pids) == 0 {
		return false
	}
	for _, pid := range pids {
		if !processRunsFrom(pid, dir) {
			return false
		}
	}
	return true
}

// stopOurInstances terminates instances of ours on the candidate ports except
// keepPort, and reports whether none is left behind. If one of our processes is
// still up the report is false, so the caller keeps remembering the port rather
// than dropping it from the candidate set forever.
func stopOurInstances(host string, candidates []int, keepPort int, root string) bool {
	cleared := true
	for _, candidate := range candidates {
		if candidate == keepPort || candidate < 1 || candidate > 65535 {
			continue
		}
		if portRunsFrom(candidate, root) {
			stopListeningOctop(host, candidate)
		}
		for _, pid := range listeningPIDs(candidate) {
			if processRunsFrom(pid, root) {
				cleared = false
			}
		}
	}
	return cleared
}
