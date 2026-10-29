package main

// MAMMOTH-PATCH: silent runtime prefetch.
//
// A runtime update is 220 MB and used to be synchronous: the user clicked
// 「版本更新」, stared at a percentage for minutes, and could not use the client
// meanwhile. Instead we download the zip quietly in the background (resumable,
// retried, throttled to one check per mammothCheckInterval) and install it at
// the next launch, when no runtime is holding the files.
//
// Applying an update at boot must never be able to brick the client: the
// previous tree stays on disk until the new one is launch-ready, and a bad zip
// is dropped instead of retried forever.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// mammothPendingRuntime is a runtime zip downloaded during a previous session.
type mammothPendingRuntime struct {
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
	ZipPath   string `json:"zip_path"`
	FetchedAt string `json:"fetched_at"`
}

func pendingRuntimePath() string {
	return filepath.Join(mammothCacheDir(), "pending-runtime.json")
}

func readPendingRuntime() (mammothPendingRuntime, bool) {
	var pending mammothPendingRuntime
	data, err := os.ReadFile(pendingRuntimePath())
	if err != nil {
		return pending, false
	}
	if err := json.Unmarshal(data, &pending); err != nil {
		return pending, false
	}
	if strings.TrimSpace(pending.Version) == "" || strings.TrimSpace(pending.ZipPath) == "" ||
		strings.TrimSpace(pending.SHA256) == "" {
		return pending, false
	}
	return pending, true
}

func writePendingRuntime(pending mammothPendingRuntime) error {
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return os.WriteFile(pendingRuntimePath(), data, 0o644)
}

func clearPendingRuntime() {
	_ = os.Remove(pendingRuntimePath())
}

// stageRuntimeUpdate downloads artifact into the update cache and records it as
// pending. Nothing is installed here: the swap happens at the next launch.
func stageRuntimeUpdate(current string, artifact *mammothArtifact, now time.Time) error {
	if artifact == nil {
		return fmt.Errorf("no runtime artifact")
	}
	if !portableMajorCompatible(current, artifact.Version) {
		// 2.x runtime would not run under a 1.x shell; the user needs the
		// installer once instead of a silently broken client.
		return fmt.Errorf("runtime %s needs a newer client package", artifact.Version)
	}
	if pending, ok := readPendingRuntime(); ok &&
		pending.Version == artifact.Version && sha256HexEqual(pending.SHA256, artifact.SHA256) {
		if _, err := os.Stat(pending.ZipPath); err == nil {
			return nil
		}
	}
	zipPath := filepath.Join(mammothCacheDir(), "runtime-"+safeVersion(artifact.Version)+".zip")
	sum, err := downloadFileSHA256(nil, artifact.URL, zipPath, nil)
	if err != nil {
		return err
	}
	if !sha256HexEqual(sum, artifact.SHA256) {
		_ = os.Remove(zipPath)
		return fmt.Errorf("sha256 mismatch for %s", artifact.Version)
	}
	if err := writePendingRuntime(mammothPendingRuntime{
		Version:   artifact.Version,
		SHA256:    artifact.SHA256,
		URL:       artifact.URL,
		ZipPath:   zipPath,
		FetchedAt: now.UTC().Format(time.RFC3339),
	}); err != nil {
		return err
	}
	log.Printf("mammoth prefetch: staged runtime %s for the next launch", artifact.Version)
	return nil
}

// applyPendingRuntimeUpdate installs a staged runtime before the service
// starts. Returns true when the tree on disk changed. Every failure keeps the
// current runtime untouched.
func (a *App) applyPendingRuntimeUpdate(locale Locale, root string) bool {
	pending, ok := readPendingRuntime()
	if !ok {
		return false
	}
	cleanup := func() {
		clearPendingRuntime()
		_ = os.Remove(pending.ZipPath)
	}
	current := portableVersion(root)
	if !portableNeedsMammothUpdate(current, &mammothArtifact{
		Version: pending.Version,
		URL:     pending.URL,
		SHA256:  pending.SHA256,
	}) {
		// Already at (or past) that version, e.g. the shell shipped a newer
		// bundled runtime in the meantime.
		cleanup()
		return false
	}
	if _, err := os.Stat(pending.ZipPath); err != nil {
		log.Printf("mammoth prefetch: staged runtime zip missing: %v", err)
		cleanup()
		return false
	}
	sum, err := sha256File(pending.ZipPath)
	if err != nil || !sha256HexEqual(sum, pending.SHA256) {
		log.Printf("mammoth prefetch: staged runtime checksum failed")
		cleanup()
		return false
	}

	log.Printf("mammoth prefetch: applying runtime %s at boot (was %s)", pending.Version, current)
	a.setStatus(desktopText(locale, copyStatusMammothApplyingStaged, pending.Version))
	a.setProgress(-1)
	if _, err := backupSQLiteBeforeUpgrade(root, current, pending.Version); err != nil {
		// Without a database backup an unattended upgrade is not worth it;
		// keep the current runtime and try again on a later release.
		log.Printf("mammoth prefetch: database backup failed: %v", err)
		cleanup()
		return false
	}
	previous, err := replacePortableFromZip(root, pending.ZipPath)
	if err != nil {
		log.Printf("mammoth prefetch: apply %s: %v", pending.Version, err)
		cleanup()
		return false
	}
	if !launchReady(root) {
		log.Printf("mammoth prefetch: applied tree is not launch-ready; rolling back")
		if err := restorePreviousDir(root, previous); err != nil {
			log.Printf("mammoth prefetch: rollback failed: %v", err)
		}
		cleanup()
		return false
	}
	// Stamp the bundled digest so ensurePortable does not re-extract the older
	// embedded zip over the version we just installed.
	_ = writePortableBundleStamp(root)
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", root).Run()
	}
	cleanup()
	return true
}

// discardStalePendingRuntime drops a staged zip that is no longer useful.
func discardStalePendingRuntime(current string) {
	pending, ok := readPendingRuntime()
	if !ok {
		return
	}
	if !portableNeedsMammothUpdate(current, &mammothArtifact{
		Version: pending.Version,
		URL:     pending.URL,
		SHA256:  pending.SHA256,
	}) {
		_ = os.Remove(pending.ZipPath)
		clearPendingRuntime()
	}
}

// cleanupPreviousPortable removes the tree displaced by the last update, now
// that the new one has booted successfully.
func cleanupPreviousPortable(root string) {
	_ = os.RemoveAll(root + ".previous")
}

// stagedRuntimeZip returns a zip already downloaded for exactly this artifact,
// so clicking「版本更新」after a background prefetch does not pull 220 MB again.
func stagedRuntimeZip(artifact *mammothArtifact) (string, bool) {
	if artifact == nil {
		return "", false
	}
	pending, ok := readPendingRuntime()
	if !ok {
		return "", false
	}
	if pending.Version != artifact.Version || !sha256HexEqual(pending.SHA256, artifact.SHA256) {
		return "", false
	}
	if _, err := os.Stat(pending.ZipPath); err != nil {
		return "", false
	}
	return pending.ZipPath, true
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
