package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const portableBundleStampName = ".bundle-sha256"

func launchReady(root string) bool {
	if _, err := os.Stat(filepath.Join(root, "launch.py")); err != nil {
		return false
	}
	info, err := os.Stat(pythonExe(root))
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1024 {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func pythonExe(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "runtime", "python.exe")
	}
	return filepath.Join(root, "runtime", "bin", "python3")
}

func ensurePortable(locale Locale, status func(string)) (replaced bool, err error) {
	root := portableDir()
	if launchReady(root) {
		newer, sameVersionChanged := bundledRuntimeNeedsReplace(root)
		if !newer && !sameVersionChanged {
			status(desktopText(locale, copyStatusUsingRuntime))
			return false, nil
		}
		if newer {
			currentVersion := portableVersion(root)
			bundledVersion, _ := bundledPortableVersion()
			status(desktopText(locale, copyStatusBackupDatabase, bundledVersion))
			if _, err := backupSQLiteBeforeUpgrade(root, currentVersion, bundledVersion); err != nil {
				return false, fmt.Errorf("%s: %w", desktopText(locale, copyErrorBackupFailed), err)
			}
		}
		status(desktopText(locale, copyStatusUpdatingRuntime))
	} else {
		status(desktopText(locale, copyStatusFirstExtract))
	}
	if err := replacePortable(root); err != nil {
		if launchReady(root) {
			// 旧目录还在：同版本重解压失败可以继续用旧的，但新版本没换上
			// 就继续跑，用户会一直看到旧版本号（Windows 文件被占用时很常见），
			// 所以要把「仍在跑旧版本」说清楚，而不是静默继续。
			if stale, current, want := runtimeBehindBundle(root); stale {
				status(desktopText(locale, copyErrorRuntimeStale, current, want))
				recordBoot("runtime_stale", fmt.Sprintf("installed %s, bundled %s", current, want), nil)
				return false, nil
			}
			status(desktopText(locale, copyStatusUpdateFailedKeep))
			return false, nil
		}
		return false, err
	}
	_ = writePortableBundleStamp(root)
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", root).Run()
	}
	if !launchReady(root) {
		return false, fmt.Errorf("portable extract missing launch.py or python under %s", root)
	}
	return true, nil
}

// desiredRuntimeVersion is the version this shell is supposed to run: the
// bundled one when it can be read, so a failed swap (Windows file locks) can
// never make the older on-disk runtime look current.
func desiredRuntimeVersion(root string) string {
	if v, err := bundledPortableVersion(); err == nil && v != "" {
		return v
	}
	return portableVersion(root)
}

// runtimeBehindBundle reports whether the portable on disk is older than the
// runtime bundled in this shell.
func runtimeBehindBundle(root string) (stale bool, current, want string) {
	current = portableVersion(root)
	want, err := bundledPortableVersion()
	if err != nil || want == "" || current == "" {
		return false, current, want
	}
	return compareVersions(want, current) > 0, current, want
}

func bundledRuntimeNeedsReplace(root string) (newer bool, sameVersionChanged bool) {
	currentVersion := portableVersion(root)
	bundledVersion, err := bundledPortableVersion()
	if err != nil || bundledVersion == "" {
		return false, false
	}
	// MAMMOTH-PATCH: in-app PyPI「检查更新」writes official Octop wheels into
	// ~/.octop/portable/packages without refreshing .bundle-sha256. Semver +
	// stamp can still look current, so detect white-label brand drift and
	// force a re-extract of the bundled Mammoth portable.
	if !installedPortableMatchesMammothBrand(root) {
		return true, false
	}
	if currentVersion == "" || compareVersions(bundledVersion, currentVersion) > 0 {
		return true, false
	}
	want, digErr := bundledPortableDigest()
	stamp := readPortableBundleStamp(root)
	digestKnown := digErr == nil && want != ""
	stampMismatch := digestKnown && stamp != "" && stamp != want
	// Missing stamp on an equal/newer tree usually means a leftover
	// ~/.octop/portable from an older Mammoth build (1.0.2 / 1.0.8 era) that
	// never recorded .bundle-sha256 — always re-extract when the shell can
	// prove its embedded/bundled digest.
	missingStamp := digestKnown && stamp == ""
	if compareVersions(bundledVersion, currentVersion) < 0 {
		// Installed semver is newer (e.g. in-app PyPI bump). Keep it unless the
		// stamp is present and proves a different shell bundle (foreign leftover).
		// A missing stamp alone is normal after an in-app upgrade — do not wipe it.
		if stampMismatch {
			return false, true
		}
		return false, false
	}
	// Same version: any digest change OR missing stamp forces re-extract.
	if !digestKnown {
		// Cannot hash the bundle — still replace when stamp is absent so a
		// stamp-less stale tree cannot stick across upgrades.
		return false, stamp == ""
	}
	return false, stampMismatch || missingStamp
}

// installedPortableMatchesMammothBrand reports whether the extracted dashboard
// still carries Mammoth white-label branding (猛犸智能体 or legacy 猛犸AI).
// Missing index.html (dev/test fixtures) is treated as OK so brand checks do
// not force unnecessary replaces.
func installedPortableMatchesMammothBrand(root string) bool {
	index := filepath.Join(root, "packages", "octop", "dashboard", "index.html")
	data, err := os.ReadFile(index)
	if err != nil {
		return true
	}
	return bytes.Contains(data, []byte("猛犸智能体")) || bytes.Contains(data, []byte("猛犸AI"))
}

func bundledPortableDigest() (string, error) {
	if os.Getenv("OCTOP_DESKTOP_PORTABLE_ZIP") == "" && len(embeddedPortable) > 0 {
		sum := sha256.Sum256(embeddedPortable)
		return hex.EncodeToString(sum[:]), nil
	}
	zipPath, err := bundledPortableZip()
	if err != nil {
		return "", err
	}
	file, err := os.Open(zipPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func readPortableBundleStamp(root string) string {
	data, err := os.ReadFile(filepath.Join(root, portableBundleStampName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writePortableBundleStamp(root string) error {
	sum, err := bundledPortableDigest()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, portableBundleStampName), []byte(sum+"\n"), 0o644)
}

func replacePortable(root string) error {
	next := root + ".new"
	previous := root + ".previous"
	_ = os.RemoveAll(next)
	if err := extractPortable(next); err != nil {
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
		return err
	}
	_ = os.RemoveAll(previous)
	return nil
}

func portableVersion(root string) string {
	version := installedPackageVersion(root)
	data, err := os.ReadFile(filepath.Join(root, "VERSION.txt"))
	if err == nil {
		bundledVersion := versionFromText(string(data))
		if version == "" || compareVersions(bundledVersion, version) > 0 {
			version = bundledVersion
		}
	}
	return version
}

func installedPackageVersion(root string) string {
	matches, _ := filepath.Glob(filepath.Join(root, "packages", "octop-*.dist-info", "METADATA"))
	version := ""
	for _, metadata := range matches {
		data, err := os.ReadFile(metadata)
		if err != nil {
			continue
		}
		value := metadataVersion(string(data))
		if value == "" {
			continue
		}
		if version == "" || compareVersions(value, version) > 0 {
			version = value
		}
	}
	return version
}

func bundledPortableVersion() (string, error) {
	if os.Getenv("OCTOP_DESKTOP_PORTABLE_ZIP") == "" && len(embeddedPortable) > 0 {
		reader, err := zip.NewReader(bytes.NewReader(embeddedPortable), int64(len(embeddedPortable)))
		if err != nil {
			return "", err
		}
		return versionFromZip(reader.File)
	}
	zipPath, err := bundledPortableZip()
	if err != nil {
		return "", err
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	return versionFromZip(reader.File)
}

func versionFromZip(files []*zip.File) (string, error) {
	fromFile, err := zipEntryVersion(files, func(name string) bool {
		return filepath.Base(filepath.FromSlash(name)) == "VERSION.txt"
	}, versionFromText)
	if err != nil {
		return "", err
	}
	if fromFile != "" {
		return fromFile, nil
	}
	return zipEntryVersion(files, func(name string) bool {
		rel := filepath.ToSlash(name)
		return strings.Contains(rel, "/octop-") && strings.HasSuffix(rel, ".dist-info/METADATA")
	}, metadataVersion)
}

func zipEntryVersion(files []*zip.File, match func(string) bool, parse func(string) string) (string, error) {
	version := ""
	for _, file := range files {
		if !match(file.Name) {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		value := parse(string(data))
		if value == "" {
			continue
		}
		if version == "" || compareVersions(value, version) > 0 {
			version = value
		}
	}
	return version, nil
}

func versionFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "octop_version="); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func metadataVersion(text string) string {
	for _, line := range strings.Split(text, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "Version:")
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractPortable(root string) error {
	if os.Getenv("OCTOP_DESKTOP_PORTABLE_ZIP") != "" {
		zipPath, err := bundledPortableZip()
		if err != nil {
			return err
		}
		return unzipGreen(zipPath, root)
	}
	if len(embeddedPortable) > 0 {
		return unzipGreenBytes(embeddedPortable, root)
	}
	zipPath, err := bundledPortableZip()
	if err != nil {
		return err
	}
	return unzipGreen(zipPath, root)
}

func bundledPortableZip() (string, error) {
	if override := os.Getenv("OCTOP_DESKTOP_PORTABLE_ZIP"); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("bundled portable package: %w", err)
		}
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	plat := greenPlat()
	// Package-name contract: "Octop-<plat>.zip" (legacy) and
	// "Octop-portable-<plat>-*.zip" are the exact names produced by the
	// portable packaging scripts (desktop/portable/_common.sh, package.sh) and
	// by the desktop CI release assets. Brand renames must NOT touch these
	// names — this lookup matches them verbatim to find the bundled/upgrade
	// package next to the app binary.
	legacy := fmt.Sprintf("Octop-%s.zip", plat)
	searchDirs := []string{
		dir,
		filepath.Join(dir, "..", "Resources"),
	}
	for _, search := range searchDirs {
		search = filepath.Clean(search)
		legacyPath := filepath.Join(search, legacy)
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath, nil
		}
		matches, _ := filepath.Glob(filepath.Join(search, "Octop-portable-"+plat+"-*.zip"))
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1], nil
		}
	}
	return "", fmt.Errorf("bundled portable package %s not found beside application", legacy)
}

func unzipGreen(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	return unzipGreenFiles(r.File, dest)
}

func unzipGreenBytes(data []byte, dest string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	return unzipGreenFiles(r.File, dest)
}

func unzipGreenFiles(files []*zip.File, dest string) error {
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	// Zip root is Octop-<plat>/… — strip that prefix.
	for _, f := range files {
		name := f.Name
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		rel := parts[1]
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) && target != filepath.Clean(dest) {
			return fmt.Errorf("illegal zip path %s", name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			linkTarget, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(string(linkTarget), target); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func waitHealth(locale Locale, base string, timeout time.Duration) error {
	// Require 2xx before loading the UI. A 503 from get_server ("server not
	// started") must not open the webview early — that race made AuthGuard
	// treat oauth/status failures as a network offline page.
	deadline := time.Now().Add(timeout)
	url := strings.TrimRight(base, "/") + "/api/health"
	var lastErr error
	var lastStatus int
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			lastStatus = resp.StatusCode
			lastErr = nil
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		} else {
			lastErr = err
			lastStatus = 0
		}
		time.Sleep(400 * time.Millisecond)
	}
	return formatHealthWaitError(locale, base, timeout, lastErr, lastStatus)
}

func formatWaitDuration(locale Locale, d time.Duration) string {
	sec := int(d.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}
	minutes := sec%60 == 0
	n := sec
	if minutes {
		n = sec / 60
	}
	switch {
	case minutes && n == 1:
		return desktopText(locale, copyWait1Minute)
	case minutes:
		return desktopText(locale, copyWaitNMinutes, n)
	case n == 1:
		return desktopText(locale, copyWait1Second)
	default:
		return desktopText(locale, copyWaitNSeconds, n)
	}
}

func formatHealthWaitError(locale Locale, base string, timeout time.Duration, lastErr error, lastStatus int) error {
	addr := strings.TrimRight(base, "/")
	wait := formatWaitDuration(locale, timeout)
	switch {
	case lastStatus >= 500:
		return fmt.Errorf("%s", desktopText(locale, copyHealthNotReady5xx, wait, addr))
	case lastErr != nil:
		return fmt.Errorf("%s", desktopText(locale, copyHealthNotReadyConnect, wait, addr))
	default:
		return fmt.Errorf("%s", desktopText(locale, copyHealthNotReady, wait, addr))
	}
}
