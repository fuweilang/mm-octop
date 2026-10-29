package main

// MAMMOTH-PATCH: Phase 1 in-app update for the green portable runtime.
// Fetches Mammoth-channel latest.json, downloads the portable zip, verifies
// sha256, replaces ~/.octop/portable, then hot-restarts Octop while keeping
// the Wails shell alive. Phase 2 (silent shell replace) is intentionally a
// stub message only.

import (
	"bytes"
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
	"strconv"
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

// updateRuntime is the process side of an in-app update: stop the running
// Octop, restart it against the new tree. Kept behind a variable so tests can
// drive download → verify → swap → probe → rollback without spawning a real
// runtime or touching the user's port.
type updateRuntimeOps struct {
	stop    func(a *App) (host string, preferred int)
	restart func(a *App, locale Locale, root, host string, preferred int) (string, error)
}

var updateRuntime = updateRuntimeOps{
	stop: func(a *App) (string, int) { return a.stopRuntimeForUpdate() },
	restart: func(a *App, locale Locale, root, host string, preferred int) (string, error) {
		return a.hotRestartOctop(locale, root, host, preferred)
	},
}

type mammothLatestFile struct {
	SchemaVersion int `json:"schema_version"`
	// Dashboard is platform independent: the SPA bundle is the same bytes on
	// macOS and Windows, so it ships once at the top level.
	Dashboard *mammothArtifact `json:"dashboard"`
	// Code carries a Python-only patch for one exact runtime version; the SPA
	// keeps its own dashboard channel.
	Code      *mammothCodeArtifact             `json:"code"`
	Platforms map[string]mammothPlatformLatest `json:"platforms"`
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
	if !portableMajorCompatible(current, remote.Version) {
		return false
	}
	return compareVersions(remote.Version, current) > 0
}

// portableMajorUpgradeAvailable reports a published runtime this shell cannot
// install by itself: the user needs the new client package once.
func portableMajorUpgradeAvailable(current string, remote *mammothArtifact) bool {
	if remote == nil || strings.TrimSpace(remote.Version) == "" {
		return false
	}
	if current == "" || portableMajorCompatible(current, remote.Version) {
		return false
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

// dashboardStamp records which SPA bundle this install already applied, so a
// repeat click on "更新版本" does not download the same zip again.
type dashboardStamp struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func dashboardStampPath() string {
	return filepath.Join(octopHome(), "cache", "mammoth-update", "dashboard.json")
}

func readDashboardStamp() dashboardStamp {
	var stamp dashboardStamp
	data, err := os.ReadFile(dashboardStampPath())
	if err != nil {
		return stamp
	}
	_ = json.Unmarshal(data, &stamp)
	return stamp
}

func writeDashboardStamp(stamp dashboardStamp) error {
	path := dashboardStampPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(stamp)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// dashboardNeedsMammothUpdate decides whether the small SPA-only update applies.
// The runtime version is the floor: a dashboard bundle that is not newer than
// the installed runtime is already on disk (bundled inside it).
func dashboardNeedsMammothUpdate(remote *mammothArtifact, stamp dashboardStamp, runtimeVersion string) bool {
	if remote == nil {
		return false
	}
	if strings.TrimSpace(remote.Version) == "" || strings.TrimSpace(remote.URL) == "" {
		return false
	}
	if strings.TrimSpace(remote.SHA256) == "" {
		return false
	}
	if stamp.Version == remote.Version && sha256HexEqual(stamp.SHA256, remote.SHA256) {
		return false
	}
	floor := strings.TrimSpace(runtimeVersion)
	if floor == "" {
		return false
	}
	return compareVersions(remote.Version, floor) > 0
}

// downloadFileSHA256 fetches fileURL into dest, resuming from a previous
// “.partial“ when the server honours “Range“. onProgress (optional) is
// called at most a few times per second so the UI can show a percentage
// instead of an apparently frozen "downloading" line.
// downloadMaxAttempts covers the first try plus three retries. Server-side
// hiccups on a 220 MB portable are common enough that failing the whole update
// on the first dropped connection reads as "the client is broken".
const downloadMaxAttempts = 4

// mammothDownloadBackoff is the base delay between attempts (1s, 2s, 4s).
// Tests shrink it so retry cases stay fast.
var mammothDownloadBackoff = time.Second

// downloadFileSHA256 fetches fileURL into dest, resuming from a previous
// “.partial“ when the server honours “Range“ and retrying transient
// failures. onProgress (optional) is called at most a few times per second so
// the UI can show a percentage instead of an apparently frozen "downloading".
func downloadFileSHA256(
	client *http.Client,
	fileURL, dest string,
	onProgress func(done, total int64),
) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 1; attempt <= downloadMaxAttempts; attempt++ {
		if attempt > 1 {
			delay := mammothDownloadBackoff * time.Duration(1<<(attempt-2))
			log.Printf("mammoth download: attempt %d failed (%v); retrying in %s", attempt-1, lastErr, delay)
			time.Sleep(delay)
		}
		sum, retryable, err := downloadFileOnce(client, fileURL, dest, onProgress)
		if err == nil {
			return sum, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
	}
	return "", fmt.Errorf("%w (after %d attempts)", lastErr, downloadMaxAttempts)
}

// downloadFileOnce performs a single (resumable) download attempt. retryable
// reports whether the failure looks transient: network errors, 5xx and 429 are
// worth another try, while a 403/404 will never succeed by retrying.
func downloadFileOnce(
	client *http.Client,
	fileURL, dest string,
	onProgress func(done, total int64),
) (sum string, retryable bool, err error) {
	tmp := dest + ".partial"
	var start int64
	if info, statErr := os.Stat(tmp); statErr == nil && info.Size() > 0 {
		start = info.Size()
	}
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return "", false, err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent && start > 0:
		// keep the bytes already on disk
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// server ignored the range (or we started fresh): rewrite from zero
		start = 0
	default:
		retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return "", retry, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if start > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(tmp, flags, 0o644)
	if err != nil {
		return "", false, err
	}
	digest := sha256.New()
	if start > 0 {
		existing, readErr := os.Open(tmp)
		if readErr != nil {
			_ = out.Close()
			return "", false, readErr
		}
		_, readErr = io.Copy(digest, existing)
		_ = existing.Close()
		if readErr != nil {
			_ = out.Close()
			return "", false, readErr
		}
	}
	total := int64(0)
	if resp.ContentLength > 0 {
		total = start + resp.ContentLength
	} else if cr := resp.Header.Get("Content-Range"); cr != "" {
		// Some servers stream 206 responses without Content-Length; the total is
		// still available as "bytes start-end/total".
		if idx := strings.LastIndex(cr, "/"); idx >= 0 {
			if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(cr[idx+1:]), 10, 64); parseErr == nil {
				total = parsed
			}
		}
	}
	writer := io.MultiWriter(out, digest)
	buf := make([]byte, 256*1024)
	done := start
	lastReport := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				_ = out.Close()
				return "", false, writeErr
			}
			done += int64(n)
			if onProgress != nil && total > 0 && time.Since(lastReport) >= 200*time.Millisecond {
				onProgress(done, total)
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = out.Close()
			// Keep .partial so the next attempt resumes instead of restarting.
			return "", true, readErr
		}
	}
	if onProgress != nil && total > 0 {
		onProgress(total, total)
	}
	if err := out.Close(); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", false, err
	}
	return hex.EncodeToString(digest.Sum(nil)), false, nil
}

func sha256HexEqual(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

// replacePortableFromZip swaps ~/.octop/portable with the contents of an
// already-downloaded green zip. Same atomic rename dance as replacePortable;
// Windows and Mac share octopHome()/portable paths.
//
// The displaced tree is returned instead of deleted: if the new build cannot
// start, rollbackUpdate() puts it back so a broken download never leaves the
// client unusable.
func replacePortableFromZip(root, zipPath string) (string, error) {
	next := root + ".new"
	previous := root + ".previous"
	_ = os.RemoveAll(next)
	if err := unzipGreen(zipPath, next); err != nil {
		_ = os.RemoveAll(next)
		return "", err
	}
	if !launchReady(next) {
		_ = os.RemoveAll(next)
		return "", fmt.Errorf("portable extract missing launch.py or python under %s", next)
	}
	_ = os.RemoveAll(previous)
	hadCurrent := false
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, previous); err != nil {
			_ = os.RemoveAll(next)
			return "", err
		}
		hadCurrent = true
	}
	if err := os.Rename(next, root); err != nil {
		if hadCurrent {
			_ = os.Rename(previous, root)
		}
		_ = os.RemoveAll(next)
		return "", err
	}
	if !hadCurrent {
		return "", nil
	}
	return previous, nil
}

// dashboardDir is where the running runtime serves the SPA from.
func dashboardDir(root string) string {
	return filepath.Join(root, "packages", "octop", "dashboard")
}

// replaceDashboardFromZip swaps only the SPA bundle. The zip carries a single
// top-level folder (dashboard/…) because unzipGreen strips the first segment.
func replaceDashboardFromZip(root, zipPath string) (string, error) {
	target := dashboardDir(root)
	next := target + ".new"
	previous := target + ".previous"
	if err := unzipGreen(zipPath, next); err != nil {
		_ = os.RemoveAll(next)
		return "", err
	}
	if _, err := os.Stat(filepath.Join(next, "index.html")); err != nil {
		_ = os.RemoveAll(next)
		return "", fmt.Errorf("dashboard bundle missing index.html")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		_ = os.RemoveAll(next)
		return "", err
	}
	_ = os.RemoveAll(previous)
	hadCurrent := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, previous); err != nil {
			_ = os.RemoveAll(next)
			return "", err
		}
		hadCurrent = true
	}
	if err := os.Rename(next, target); err != nil {
		if hadCurrent {
			_ = os.Rename(previous, target)
		}
		_ = os.RemoveAll(next)
		return "", err
	}
	if !hadCurrent {
		return "", nil
	}
	return previous, nil
}

// stopRuntimeForUpdate shuts the runtime down and releases its port so files can
// be replaced (Windows locks running binaries) and the restart is clean.
func (a *App) stopRuntimeForUpdate() (string, int) {
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
	return host, preferred
}

// applyDashboardUpdate updates only the SPA (a few MB) and hot-restarts the
// runtime; the portable runtime, the database and the login session stay as is.
func (a *App) applyDashboardUpdate(locale Locale, previousURL, root string, artifact *mammothArtifact) {
	zipPath := filepath.Join(
		octopHome(), "cache", "mammoth-update",
		"dashboard-"+safeVersion(artifact.Version)+".zip",
	)
	a.setStatus(desktopText(locale, copyStatusMammothDashboardDownloading, 0))
	a.setProgress(0)
	sum, err := downloadFileSHA256(nil, artifact.URL, zipPath, func(done, total int64) {
		if total <= 0 {
			return
		}
		a.setStatus(desktopText(locale, copyStatusMammothDashboardDownloading, done*100/total))
		a.setProgress(int(done * 100 / total))
	})
	if err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}
	a.setProgress(-1)
	a.setStatus(desktopText(locale, copyStatusMammothVerifying))
	if !sha256HexEqual(sum, artifact.SHA256) {
		_ = os.Remove(zipPath)
		a.failMammothUpdate(locale, previousURL, fmt.Errorf("%s", desktopText(locale, copyErrorMammothSHAMismatch)))
		return
	}

	host, preferred := updateRuntime.stop(a)
	a.setStatus(desktopText(locale, copyStatusMammothDashboardInstalling))
	previous, err := replaceDashboardFromZip(root, zipPath)
	if err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}

	a.setStatus(desktopText(locale, copyStatusMammothRestarting))
	base, err := updateRuntime.restart(a, locale, root, host, preferred)
	if err == nil {
		err = probeDashboardServes(base)
	}
	if err != nil {
		a.rollbackUpdate(locale, previousURL, root, dashboardDir(root), previous, host, preferred, err)
		return
	}
	_ = writeDashboardStamp(dashboardStamp{Version: artifact.Version, SHA256: artifact.SHA256})
	_ = os.RemoveAll(previous)
}

// restorePreviousDir puts the tree displaced by an update back in place.
func restorePreviousDir(target, previous string) error {
	if strings.TrimSpace(previous) == "" {
		return fmt.Errorf("no previous build to restore")
	}
	if _, err := os.Stat(previous); err != nil {
		return fmt.Errorf("previous build missing: %w", err)
	}
	broken := target + ".broken"
	_ = os.RemoveAll(broken)
	hadCurrent := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, broken); err != nil {
			return err
		}
		hadCurrent = true
	}
	if err := os.Rename(previous, target); err != nil {
		if hadCurrent {
			_ = os.Rename(broken, target)
		}
		return err
	}
	_ = os.RemoveAll(broken)
	return nil
}

// probeDashboardServes is the post-restart gate for an update: /api/health can
// answer while the SPA bundle is broken (bad build, missing index.html), which
// would leave the user on a white screen with no way back.
func probeDashboardServes(base string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return fmt.Errorf("dashboard probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dashboard probe HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("dashboard probe read: %w", err)
	}
	if !bytes.Contains(body, []byte(`id="root"`)) {
		// Accept any real SPA document so a future mount-point rename does not
		// trigger a false rollback on a perfectly good build.
		lower := bytes.ToLower(body)
		if !bytes.Contains(lower, []byte("<!doctype html")) || !bytes.Contains(lower, []byte("<script")) {
			return fmt.Errorf("dashboard probe: index.html not served")
		}
	}
	return nil
}

// rollbackUpdate restores the build displaced by a failed update and brings the
// runtime back up, so a bad package degrades to "old version still works"
// instead of "client is broken until someone reinstalls".
func (a *App) rollbackUpdate(
	locale Locale,
	previousURL, root, target, previous, host string,
	preferred int,
	cause error,
) {
	log.Printf("desktop: update failed (%v); rolling back %s", cause, target)
	a.setStatus(desktopText(locale, copyStatusMammothRollingBack))
	updateRuntime.stop(a)
	if err := restorePreviousDir(target, previous); err != nil {
		a.failMammothUpdate(locale, previousURL, fmt.Errorf("%w (rollback failed: %v)", cause, err))
		return
	}
	if _, err := updateRuntime.restart(a, locale, root, host, preferred); err != nil {
		a.failMammothUpdate(locale, previousURL, fmt.Errorf("%w (rollback restart failed: %v)", cause, err))
		return
	}
	a.setStatus(desktopText(locale, copyStatusMammothRolledBack))
	time.Sleep(1600 * time.Millisecond)
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

	if !wantPortable && portableMajorUpgradeAvailable(current, entry.Portable) {
		// Cross-major releases ship a new shell + runtime + schema; installing
		// the new runtime into this shell would break it. Point at the installer
		// instead of downloading 200+ MB that cannot run.
		a.setStatus(desktopText(locale, copyStatusMammothMajorNewer, entry.Portable.Version))
		time.Sleep(2000 * time.Millisecond)
		a.restoreDashboard(previousURL)
		return
	}

	if !wantPortable {
		// The runtime is current; only the SPA may be newer. Downloading the
		// 8 MB dashboard bundle instead of the 220 MB portable keeps a routine
		// release to seconds.
		if dashboardNeedsMammothUpdate(doc.Dashboard, readDashboardStamp(), current) {
			a.applyDashboardUpdate(locale, previousURL, root, doc.Dashboard)
			return
		}
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
	a.setProgress(-1)
	cacheDir := filepath.Join(octopHome(), "cache", "mammoth-update")
	zipPath := filepath.Join(cacheDir, fmt.Sprintf("portable-%s-%s.zip", plat, safeVersion(artifact.Version)))
	var sum string
	if stagedPath, ok := stagedRuntimeZip(artifact); ok {
		// The background prefetch already fetched this exact build; verifying it
		// takes a couple of seconds, downloading 220 MB again does not.
		zipPath = stagedPath
		sum, err = sha256File(zipPath)
	} else {
		sum, err = downloadFileSHA256(nil, artifact.URL, zipPath, func(done, total int64) {
			if total <= 0 {
				return
			}
			a.setStatus(desktopText(locale, copyStatusMammothDownloadingPercent, artifact.Version, done*100/total))
			a.setProgress(int(done * 100 / total))
		})
	}
	if err != nil {
		a.failMammothUpdate(locale, previousURL, err)
		return
	}
	a.setProgress(-1)
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
	host, preferred := updateRuntime.stop(a)

	a.setStatus(desktopText(locale, copyStatusMammothInstalling, artifact.Version))
	previous, err := replacePortableFromZip(root, zipPath)
	if err != nil {
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
		a.rollbackUpdate(locale, previousURL, root, root, previous, host, preferred,
			fmt.Errorf("portable extract missing launch.py or python under %s", root))
		return
	}

	a.setStatus(desktopText(locale, copyStatusMammothRestarting))
	base, err := updateRuntime.restart(a, locale, root, host, preferred)
	if err == nil {
		err = probeDashboardServes(base)
	}
	if err != nil {
		a.rollbackUpdate(locale, previousURL, root, root, previous, host, preferred, err)
		return
	}
	_ = os.RemoveAll(previous)
	if shellNotice {
		log.Printf("mammoth update: shell %s available (Phase 2 stub)", entry.Shell.Version)
		a.setStatus(desktopText(locale, copyStatusMammothShellNewer, entry.Shell.Version))
		time.Sleep(1200 * time.Millisecond)
	}
}

func (a *App) hotRestartOctop(locale Locale, root, host string, preferred int) (string, error) {
	configured := preferred
	preferred = a.preferredPort(host, configured)
	// allowAlternate=true: an update restart must not be blocked by a port
	// conflict (the target port itself is still tried first).
	port, err := chooseStartPort(host, preferred, true)
	if err != nil {
		return "", err
	}
	if port != configured {
		log.Printf(
			"desktop: update restart moved to port %d (configured %d is busy); the dashboard origin changed",
			port,
			configured,
		)
	}
	a.mu.Lock()
	cmd, err := startOctop(root, port)
	a.cmd = cmd
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	base := dashboardURL(host, port)
	a.setStatus(desktopText(locale, copyStatusStartingService))
	if err := waitHealth(locale, base, 2*time.Minute); err != nil {
		return base, err
	}
	if err := writeDesktopPort(port); err != nil {
		log.Printf("desktop port: %v", err)
	} else {
		a.mu.Lock()
		a.ownsListenPort = true
		a.mu.Unlock()
	}
	a.rememberListenPort(port, configured)
	a.showDashboard(base)
	return base, nil
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
		if _, restartErr := updateRuntime.restart(a, locale, portableDir(), host, preferred); restartErr != nil {
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
