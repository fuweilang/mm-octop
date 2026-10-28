package main

// MAMMOTH-PATCH: 桌面启动报错回传.
//
// Boot diagnostics are written to ~/.octop/logs/client-boot.jsonl before
// anything is uploaded, so a machine that never reaches the network still has
// the evidence. Upload is best effort: undelivered lines keep uploaded=false
// and are retried on the next boot. The file rotates at 1MB and keeps one
// previous generation.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	bootLogName        = "client-boot.jsonl"
	bootLogRotatedName = "client-boot.1.jsonl"
	bootLogMaxSize     = 1 << 20 // 1MB, then rotate once and drop the oldest
	bootLogUploadLimit = 40
	bootLogPostTimeout = 5 * time.Second
)

var (
	bootLogMu     sync.Mutex
	bootLogSecret = regexp.MustCompile(`(?i)\b(authorization|token|cookie|password|secret)(\s*[:=]\s*)\S+`)
	// "Bearer abc.def" has no colon after the scheme; drop the credential itself.
	bootLogBearer = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
)

type bootLogEntry struct {
	ID             string `json:"id"`
	TS             string `json:"ts"`
	ShellVersion   string `json:"shell_version"`
	RuntimeVersion string `json:"runtime_version"`
	Platform       string `json:"platform"`
	Phase          string `json:"phase"`
	LastError      string `json:"last_error,omitempty"`
	Probes         any    `json:"probes,omitempty"`
	Uploaded       bool   `json:"uploaded"`
}

func bootLogPath(name string) string {
	return filepath.Join(octopHome(), "logs", name)
}

// recordBoot appends one phase line. Never fails the caller: a log write must
// not stop the app from starting.
func recordBoot(phase, lastError string, probes any) {
	entry := bootLogEntry{
		ID:             fmt.Sprintf("%d-%s", time.Now().UnixNano(), phase),
		TS:             time.Now().UTC().Format(time.RFC3339),
		ShellVersion:   desktopShellVersion,
		RuntimeVersion: portableVersion(portableDir()),
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		Phase:          phase,
		LastError:      scrubBootText(lastError, 400),
		Probes:         probes,
	}
	if err := appendBootLog(entry); err != nil {
		log.Printf("desktop: boot log append: %v", err)
	}
}

func scrubBootText(text string, limit int) string {
	cleaned := bootLogBearer.ReplaceAllString(strings.Join(strings.Fields(text), " "), "Bearer [redacted]")
	cleaned = bootLogSecret.ReplaceAllString(cleaned, "$1$2[redacted]")
	if limit > 0 && len(cleaned) > limit {
		cleaned = cleaned[:limit-1] + "…"
	}
	return cleaned
}

func appendBootLog(entry bootLogEntry) error {
	bootLogMu.Lock()
	defer bootLogMu.Unlock()

	dir := filepath.Dir(bootLogPath(bootLogName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := bootLogPath(bootLogName)
	if info, err := os.Stat(path); err == nil && info.Size() >= bootLogMaxSize {
		if err := rotateBootLog(); err != nil {
			log.Printf("desktop: boot log rotate: %v", err)
		}
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(encoded, '\n'))
	return err
}

// rotateBootLog keeps exactly two generations: the live file and one previous.
func rotateBootLog() error {
	logs := filepath.Dir(bootLogPath(bootLogName))
	previous := filepath.Join(logs, bootLogRotatedName)
	if err := os.Remove(previous); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(bootLogPath(bootLogName), previous)
}

// readBootLog returns the rotated generation first, then the live one, so the
// upload retries in chronological order.
func readBootLog() []bootLogEntry {
	bootLogMu.Lock()
	defer bootLogMu.Unlock()

	var entries []bootLogEntry
	for _, name := range []string{bootLogRotatedName, bootLogName} {
		data, err := os.ReadFile(bootLogPath(name))
		if err != nil {
			continue
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var entry bootLogEntry
			if json.Unmarshal(line, &entry) == nil {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

// uploadBootLog sends undelivered lines to the local service. Stops on the
// first failure: the remaining lines stay uploaded=false for the next boot.
func uploadBootLog(base string) {
	if base == "" {
		return
	}
	sent := map[string]bool{}
	attempted := 0
	for _, entry := range readBootLog() {
		if entry.Uploaded || entry.ID == "" {
			continue
		}
		if attempted >= bootLogUploadLimit {
			break
		}
		if err := postBootLog(base, entry); err != nil {
			log.Printf("desktop: boot log upload deferred: %v", err)
			break
		}
		sent[entry.ID] = true
		attempted++
	}
	if len(sent) > 0 {
		markBootUploaded(sent)
		log.Printf("desktop: uploaded %d boot log line(s)", len(sent))
	}
}

func postBootLog(base string, entry bootLogEntry) error {
	payload := map[string]any{
		"shell_version":   entry.ShellVersion,
		"runtime_version": entry.RuntimeVersion,
		"platform":        entry.Platform,
		"phase":           entry.Phase,
		"last_error":      entry.LastError,
		"probes":          entry.Probes,
		"ts":              entry.TS,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: bootLogPostTimeout}
	resp, err := client.Post(strings.TrimRight(base, "/")+"/api/client-log", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("client-log status %d", resp.StatusCode)
	}
	return nil
}

func markBootUploaded(sent map[string]bool) {
	bootLogMu.Lock()
	defer bootLogMu.Unlock()

	for _, name := range []string{bootLogRotatedName, bootLogName} {
		path := bootLogPath(name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var entry bootLogEntry
			if json.Unmarshal([]byte(line), &entry) == nil && sent[entry.ID] {
				entry.Uploaded = true
				if encoded, err := json.Marshal(entry); err == nil {
					line = string(encoded)
				}
			}
			out = append(out, line)
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
			log.Printf("desktop: mark boot log uploaded: %v", err)
			continue
		}
		if err := os.Rename(tmp, path); err != nil {
			log.Printf("desktop: replace boot log: %v", err)
		}
	}
}
