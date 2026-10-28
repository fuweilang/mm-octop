package main

// MAMMOTH-PATCH: Phase 1 in-app update for the green portable runtime.
// Fetches Mammoth-channel latest.json, downloads the portable zip, verifies
// sha256, replaces ~/.octop/portable, then hot-restarts Octop while keeping
// the Wails shell alive. Phase 2 (silent shell replace) is intentionally a
// stub message only.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultMammothLatestURL = "https://www.mmai.vip/downloads/mammoth-latest.json"
	mammothUpdateEventName  = "desktop:mammoth-update"
)

// desktopShellVersion is the Wails shell build id. Override at link time with
// -ldflags "-X main.desktopShellVersion=1.2.3". Phase 2 may use this for
// silent shell replace; Phase 1 only compares it for a stub message.
var desktopShellVersion = "0.0.0"

type mammothLatestFile struct {
	SchemaVersion int                             `json:"schema_version"`
	Platforms     map[string]mammothPlatformLatest `json:"platforms"`
}

type mammothPlatformLatest struct {
	Portable *mammothArtifact `json:"portable"`
	Shell    *mammothArtifact `json:"shell"`
}

type mammothArtifact struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

func mammothLatestURL() string {
	if v := strings.TrimSpace(os.Getenv("OCTOP_MAMMOTH_LATEST_URL")); v != "" {
		return v
	}
	return defaultMammothLatestURL
}

func fetchMammothLatest(client *http.Client, latestURL string) (*mammothLatestFile, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Get(latestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("latest.json HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var doc mammothLatestFile
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse latest.json: %w", err)
	}
	if len(doc.Platforms) == 0 {
		return nil, fmt.Errorf("latest.json has no platforms")
	}
	return &doc, nil
}

func platformLatestFor(doc *mammothLatestFile, plat string) (mammothPlatformLatest, error) {
	if doc == nil {
		return mammothPlatformLatest{}, fmt.Errorf("latest.json is nil")
	}
	entry, ok := doc.Platforms[plat]
	if !ok {
		return mammothPlatformLatest{}, fmt.Errorf("latest.json missing platform %q", plat)
	}
	return entry, nil
}

func portableNeedsMammothUpdate(current string, remote *mammothArtifact) bool {
	if remote == nil || strings.TrimSpace(remote.Version) == "" || strings.TrimSpace(remote.URL) == "" {
		return false
	}
	if strings.TrimSpace(remote.SHA256) == "" {
		return false
	}
	if current == "" {
		return true
	}
	return compareVersions(remote.Version, current) > 0
}

func shellNeedsMammothNotice(remote *mammothArtifact) bool {
	if remote == nil || strings.TrimSpace(remote.Version) == "" {
		return false
	}
	// Phase 1: shell build id is only meaningful once stamped via ldflags.
	// Default 0.0.0/dev would otherwise always look "older" than any published shell.
	switch strings.TrimSpace(desktopShellVersion) {
	case "", "0.0.0", "dev":
		return false
	}
	return compareVersions(remote.Version, desktopShellVersion) > 0
}

func downloadFileSHA256(client *http.Client, fileURL, dest string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := client.Get(fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".partial"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, sum), resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func sha256HexEqual(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

// replacePortableFromZip swaps ~/.octop/portable with the contents of an
// already-downloaded green zip. Same atomic rename dance as replacePortable;
// Windows and Mac share octopHome()/portable paths.
func replacePortableFromZip(root, zipPath string) error {
	next := root + ".new"
	previous := root + ".previous"
	_ = os.RemoveAll(next)
	if err := unzipGreen(zipPath, next); err != nil {
		_ = os.RemoveAll(next)
		return err
	}
	if !launchReady(next) {
		_ = os.RemoveAll(next)
		return fmt.Errorf("portable extract missing launch.py or python under %s", next)
	}
	_ = os.RemoveAll(previous)
	hadCurrent := false
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, previous); err != nil {
			_ = os.RemoveAll(next)
			return err
		}
		hadCurrent = true
	}
	if err := os.Rename(next, root); err != nil {
		if hadCurrent {
			_ = os.Rename(previous, root)
		}
		_ = os.RemoveAll(next)
		return err
	}
	_ = os.RemoveAll(previous)
	return nil
}

func (a *App) beginMammothUpdate() {
	a.mu.Lock()
	if a.quitting {
		a.mu.Unlock()
		return
	}
	if a.updating {
		a.mu.Unlock()
		return
	}
	a.updating = true
	a.mu.Unlock()
	go func() {
		defer func() {
			a.mu.Lock()
			a.updating = false
			a.mu.Unlock()
		}()
		a.runMammothUpdate()
	}()
}

func (a *App) runMammothUpdate() {
	locale := LocaleEN
	if a.store != nil {
		locale = a.store.get().Locale
	}
	previousURL := a.dashboardURL
	if a.window != nil {
		a.window.SetURL("/")
	}
	a.setStatus(desktopText(locale, copyStatusCheckingMammothUpdate))

	doc, err := fetchMammothLatest(nil, mammothLatestURL())
	if err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}
	plat := greenPlat()
	entry, err := platformLatestFor(doc, plat)
	if err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}

	root := portableDir()
	current := portableVersion(root)
	wantPortable := portableNeedsMammothUpdate(current, entry.Portable)
	shellNotice := shellNeedsMammothNotice(entry.Shell)

	if !wantPortable {
		msg := desktopText(locale, copyStatusMammothUpToDate, current)
		if current == "" {
			msg = desktopText(locale, copyStatusMammothUpToDate, "unknown")
		}
		a.setStatus(msg)
		if shellNotice {
			a.setStatus(desktopText(locale, copyStatusMammothShellNewer, entry.Shell.Version))
		}
		time.Sleep(1600 * time.Millisecond)
		a.restoreDashboard(previousURL)
		return
	}

	artifact := entry.Portable
	a.setStatus(desktopText(locale, copyStatusMammothDownloading, artifact.Version))
	cacheDir := filepath.Join(octopHome(), "cache", "mammoth-update")
	zipPath := filepath.Join(cacheDir, fmt.Sprintf("portable-%s-%s.zip", plat, safeVersion(artifact.Version)))
	sum, err := downloadFileSHA256(nil, artifact.URL, zipPath)
	if err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}
	a.setStatus(desktopText(locale, copyStatusMammothVerifying))
	if !sha256HexEqual(sum, artifact.SHA256) {
		_ = os.Remove(zipPath)
		a.failMammothUpdate(locale, previousURL, fmt.Errorf("%s", desktopText(locale, copyErrorMammothSHAMismatch)))
		return
	}

	a.setStatus(desktopText(locale, copyStatusBackupDatabase, artifact.Version))
	if _, err := backupSQLiteBeforeUpgrade(root, current, artifact.Version); err != nil {
		a.failMammothUpdate(locale, previousURL, fmt.Errorf("%s: %w", desktopText(locale, copyErrorBackupFailed), err))
		return
	}

	// Stop Octop before replacing files so Windows (and Mac) release runtime locks.
	a.mu.Lock()
	cmd := a.cmd
	a.cmd = nil
	ownsPort := a.ownsListenPort
	a.ownsListenPort = false
	a.mu.Unlock()
	stopOctop(cmd)
	host, preferred := resolveListen()
	if saved, ok := readDesktopPort(); ok {
		stopListeningOctop(host, saved)
	}
	stopListeningOctop(host, preferred)
	if ownsPort {
		clearDesktopPort()
	}

	a.setStatus(desktopText(locale, copyStatusMammothInstalling, artifact.Version))
	if err := replacePortableFromZip(root, zipPath); err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}
	// Stamp with the *bundled* digest so ensurePortable does not immediately
	// re-extract an older embedded zip at the same semver. Newer Mammoth
	// versions are kept by compareVersions(bundled, current) < 0.
	_ = writePortableBundleStamp(root)
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", root).Run()
	}
	if !launchReady(root) {
		a.failMammothUpdate(locale, previousURL, fmt.Errorf("portable extract missing launch.py or python under %s", root))
		return
	}

	a.setStatus(desktopText(locale, copyStatusMammothRestarting))
	if err := a.hotRestartOctop(locale, root, host, preferred); err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}
	if shellNotice {
		log.Printf("mammoth update: shell %s available (Phase 2 stub)", entry.Shell.Version)
		a.setStatus(desktopText(locale, copyStatusMammothShellNewer, entry.Shell.Version))
		time.Sleep(1200 * time.Millisecond)
	}
}

func (a *App) hotRestartOctop(locale Locale, root, host string, preferred int) error {
	port, err := chooseFreePort(host, preferred)
	if err != nil {
		return err
	}
	a.mu.Lock()
	cmd, err := startOctop(root, port)
	a.cmd = cmd
	a.mu.Unlock()
	if err != nil {
		return err
	}
	base := dashboardURL(host, port)
	a.setStatus(desktopText(locale, copyStatusStartingService))
	if err := waitHealth(locale, base, 2*time.Minute); err != nil {
		return err
	}
	if err := writeDesktopPort(port); err != nil {
		log.Printf("desktop port: %v", err)
	} else {
		a.mu.Lock()
		a.ownsListenPort = true
		a.mu.Unlock()
	}
	a.showDashboard(base)
	return nil
}

func (a *App) failMammothUpdate(locale Locale, previousURL string, err error) {
	msg := desktopText(locale, copyErrorMammothUpdateFailed, err.Error())
	log.Printf("mammoth update: %v", err)
	a.setStatus(msg)
	time.Sleep(1800 * time.Millisecond)
	// If we already stopped Octop (e.g. replace failed), try to bring the
	// still-on-disk portable back so the user is not stuck on the splash.
	host, preferred := resolveListen()
	if findRunningDashboard(host, preferred) == "" && launchReady(portableDir()) {
		if restartErr := a.hotRestartOctop(locale, portableDir(), host, preferred); restartErr != nil {
			log.Printf("mammoth update recovery restart: %v", restartErr)
			a.setStatus(desktopText(locale, copyErrorMammothUpdateFailed, restartErr.Error()))
			time.Sleep(1600 * time.Millisecond)
			return
		}
		return
	}
	a.restoreDashboard(previousURL)
}

func (a *App) restoreDashboard(previousURL string) {
	previousURL = strings.TrimSpace(previousURL)
	host, preferred := resolveListen()
	if previousURL != "" && octopAlreadyRunning(previousURL) {
		a.showDashboard(previousURL)
		return
	}
	if existing := findRunningDashboard(host, preferred); existing != "" {
		a.showDashboard(existing)
	}
}
