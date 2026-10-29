package main

// MAMMOTH-PATCH: code-only patches.
//
// Most releases change Python code and the SPA but no dependencies, yet the
// only update path used to be the 220 MB portable zip. This adds a third,
// smaller channel next to "portable" and "dashboard":
//
//   code: { version, runtime, url, sha256 }
//
// The zip carries `packages/octop` (minus the SPA, which has its own channel)
// and applies only when the installed runtime is exactly `runtime`, so a patch
// can never land on a runtime missing the packages it imports. Like the
// runtime, it is downloaded in the background and installed at the next launch;
// if the patched code cannot serve /api/health, boot rolls it back.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mammothCodeArtifact struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	// Runtime is the single runtime version this patch is built for.
	Runtime string `json:"runtime"`
}

type codePatchStamp struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Runtime string `json:"runtime"`
}

func codePatchStampPath() string {
	return filepath.Join(mammothCacheDir(), "code.json")
}

func readCodePatchStamp() codePatchStamp {
	var stamp codePatchStamp
	data, err := os.ReadFile(codePatchStampPath())
	if err != nil {
		return stamp
	}
	_ = json.Unmarshal(data, &stamp)
	return stamp
}

func writeCodePatchStamp(stamp codePatchStamp) error {
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(stamp)
	if err != nil {
		return err
	}
	return os.WriteFile(codePatchStampPath(), data, 0o644)
}

func clearCodePatchStamp() {
	_ = os.Remove(codePatchStampPath())
}

type mammothPendingCode struct {
	Version   string `json:"version"`
	Runtime   string `json:"runtime"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
	ZipPath   string `json:"zip_path"`
	FetchedAt string `json:"fetched_at"`
}

func pendingCodePath() string {
	return filepath.Join(mammothCacheDir(), "pending-code.json")
}

func readPendingCode() (mammothPendingCode, bool) {
	var pending mammothPendingCode
	data, err := os.ReadFile(pendingCodePath())
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

func writePendingCode(pending mammothPendingCode) error {
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return os.WriteFile(pendingCodePath(), data, 0o644)
}

func clearPendingCode() {
	_ = os.Remove(pendingCodePath())
}

// codePatchNeedsUpdate mirrors dashboardNeedsMammothUpdate: the runtime version
// is the floor, the stamp prevents re-downloading the same build, and a patch
// built for another runtime is never considered.
func codePatchNeedsUpdate(remote *mammothCodeArtifact, stamp codePatchStamp, runtimeVersion string) bool {
	if remote == nil {
		return false
	}
	if strings.TrimSpace(remote.Version) == "" || strings.TrimSpace(remote.URL) == "" ||
		strings.TrimSpace(remote.SHA256) == "" {
		return false
	}
	floor := strings.TrimSpace(runtimeVersion)
	if floor == "" {
		return false
	}
	if want := strings.TrimSpace(remote.Runtime); want != "" && want != floor {
		return false
	}
	if stamp.Version == remote.Version && sha256HexEqual(stamp.SHA256, remote.SHA256) {
		return false
	}
	return compareVersions(remote.Version, floor) > 0
}

// stageCodePatch downloads a code patch and records it for the next launch.
func stageCodePatch(now time.Time, artifact *mammothCodeArtifact, runtimeVersion string) error {
	if artifact == nil {
		return fmt.Errorf("no code artifact")
	}
	if !codePatchNeedsUpdate(artifact, readCodePatchStamp(), runtimeVersion) {
		return nil
	}
	if pending, ok := readPendingCode(); ok &&
		pending.Version == artifact.Version && sha256HexEqual(pending.SHA256, artifact.SHA256) {
		if _, err := os.Stat(pending.ZipPath); err == nil {
			return nil
		}
	}
	zipPath := filepath.Join(mammothCacheDir(), "code-"+safeVersion(artifact.Version)+".zip")
	sum, err := downloadFileSHA256(nil, artifact.URL, zipPath, nil)
	if err != nil {
		return err
	}
	if !sha256HexEqual(sum, artifact.SHA256) {
		_ = os.Remove(zipPath)
		return fmt.Errorf("sha256 mismatch for %s", artifact.Version)
	}
	if err := writePendingCode(mammothPendingCode{
		Version:   artifact.Version,
		Runtime:   runtimeVersion,
		SHA256:    artifact.SHA256,
		URL:       artifact.URL,
		ZipPath:   zipPath,
		FetchedAt: now.UTC().Format(time.RFC3339),
	}); err != nil {
		return err
	}
	log.Printf("mammoth prefetch: staged code patch %s for the next launch", artifact.Version)
	return nil
}

// applyPendingCodePatch swaps packages/octop for a staged patch before the
// service starts. Returns whether it changed the tree and, if so, the displaced
// directory so boot can roll back when the patched code cannot serve.
func (a *App) applyPendingCodePatch(locale Locale, root string) (bool, string) {
	pending, ok := readPendingCode()
	if !ok {
		return false, ""
	}
	runtimeVersion := portableVersion(root)
	cleanup := func() {
		clearPendingCode()
		_ = os.Remove(pending.ZipPath)
	}
	if !codePatchNeedsUpdate(&mammothCodeArtifact{
		Version: pending.Version,
		URL:     pending.URL,
		SHA256:  pending.SHA256,
		Runtime: pending.Runtime,
	}, readCodePatchStamp(), runtimeVersion) {
		cleanup()
		return false, ""
	}
	if _, err := os.Stat(pending.ZipPath); err != nil {
		log.Printf("mammoth prefetch: staged code zip missing: %v", err)
		cleanup()
		return false, ""
	}
	sum, err := sha256File(pending.ZipPath)
	if err != nil || !sha256HexEqual(sum, pending.SHA256) {
		log.Printf("mammoth prefetch: staged code checksum failed")
		cleanup()
		return false, ""
	}

	codeDir := filepath.Join(root, "packages", "octop")
	next := codeDir + ".new"
	previous := codeDir + ".previous"
	_ = os.RemoveAll(next)
	if err := unzipGreen(pending.ZipPath, next); err != nil {
		_ = os.RemoveAll(next)
		log.Printf("mammoth prefetch: extract code %s: %v", pending.Version, err)
		cleanup()
		return false, ""
	}
	if _, err := os.Stat(filepath.Join(next, "__main__.py")); err != nil {
		_ = os.RemoveAll(next)
		log.Printf("mammoth prefetch: code zip is not a package tree")
		cleanup()
		return false, ""
	}
	// The SPA ships on its own channel; keep whatever dashboard the runtime
	// already has instead of regressing it to whatever this zip carried.
	if src := filepath.Join(codeDir, "dashboard"); dirExists(src) {
		if err := copyTree(src, filepath.Join(next, "dashboard")); err != nil {
			_ = os.RemoveAll(next)
			log.Printf("mammoth prefetch: keep dashboard: %v", err)
			cleanup()
			return false, ""
		}
	}
	_ = os.RemoveAll(previous)
	if err := os.Rename(codeDir, previous); err != nil {
		_ = os.RemoveAll(next)
		log.Printf("mammoth prefetch: displace code dir: %v", err)
		cleanup()
		return false, ""
	}
	if err := os.Rename(next, codeDir); err != nil {
		_ = os.Rename(previous, codeDir)
		_ = os.RemoveAll(next)
		log.Printf("mammoth prefetch: install code dir: %v", err)
		cleanup()
		return false, ""
	}
	if err := writeCodePatchStamp(codePatchStamp{
		Version: pending.Version,
		SHA256:  pending.SHA256,
		Runtime: runtimeVersion,
	}); err != nil {
		log.Printf("mammoth prefetch: code stamp: %v", err)
	}
	log.Printf("mammoth prefetch: applied code patch %s at boot", pending.Version)
	cleanup()
	return true, previous
}

// rollbackCodePatch restores the code directory displaced at boot and starts
// the service again. Returns true only when the previous code serves health.
func (a *App) rollbackCodePatch(locale Locale, root string, port int, base, previous string) bool {
	if strings.TrimSpace(previous) == "" {
		return false
	}
	log.Printf("desktop: code patch did not serve /api/health; rolling back")
	a.setStatus(desktopText(locale, copyStatusMammothRollingBack))
	a.mu.Lock()
	cmd := a.cmd
	a.cmd = nil
	a.mu.Unlock()
	stopOctop(cmd)
	if err := restorePreviousDir(filepath.Join(root, "packages", "octop"), previous); err != nil {
		log.Printf("desktop: code patch rollback failed: %v", err)
		return false
	}
	clearCodePatchStamp()
	restarted, err := startOctop(root, port)
	if err != nil {
		log.Printf("desktop: restart after code rollback failed: %v", err)
		return false
	}
	a.mu.Lock()
	a.cmd = restarted
	a.mu.Unlock()
	if err := waitHealth(locale, base, 2*time.Minute); err != nil {
		log.Printf("desktop: previous code did not serve either: %v", err)
		return false
	}
	a.setStatus(desktopText(locale, copyStatusMammothRolledBack))
	recordBoot("code_patch_rolled_back", "", nil)
	return true
}

// discardStalePendingCode drops a staged patch built for another runtime or one
// that is already installed.
func discardStalePendingCode(runtimeVersion string) {
	pending, ok := readPendingCode()
	if !ok {
		return
	}
	if !codePatchNeedsUpdate(&mammothCodeArtifact{
		Version: pending.Version,
		URL:     pending.URL,
		SHA256:  pending.SHA256,
		Runtime: pending.Runtime,
	}, readCodePatchStamp(), runtimeVersion) {
		_ = os.Remove(pending.ZipPath)
		clearPendingCode()
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// copyTree copies a directory tree; used to carry the SPA across a code swap.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}
