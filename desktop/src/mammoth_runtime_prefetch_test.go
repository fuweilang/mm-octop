package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sha256OfFile(t *testing.T, path string) string {
	t.Helper()
	sum, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func writeStagedRuntime(t *testing.T, version string) (zipPath, digest string) {
	t.Helper()
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath = filepath.Join(mammothCacheDir(), "runtime-"+version+".zip")
	writeTestGreenZip(t, zipPath, version)
	digest = sha256OfFile(t, zipPath)
	if err := writePendingRuntime(mammothPendingRuntime{
		Version:   version,
		SHA256:    digest,
		URL:       "https://example/runtime.zip",
		ZipPath:   zipPath,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	return zipPath, digest
}

func TestPendingRuntimeRoundTrip(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	if _, ok := readPendingRuntime(); ok {
		t.Fatal("expected no pending runtime initially")
	}
	want := mammothPendingRuntime{
		Version: "1.1.5",
		SHA256:  "abc",
		URL:     "https://x",
		ZipPath: "/tmp/x.zip",
	}
	if err := writePendingRuntime(want); err != nil {
		t.Fatal(err)
	}
	got, ok := readPendingRuntime()
	if !ok || got.Version != want.Version || got.ZipPath != want.ZipPath {
		t.Fatalf("pending = %#v, ok = %v", got, ok)
	}
	clearPendingRuntime()
	if _, ok := readPendingRuntime(); ok {
		t.Fatal("pending runtime should be cleared")
	}
}

// A runtime staged yesterday must install before the service starts, so the
// user never waits through a download again.
func TestApplyPendingRuntimeUpdateInstallsStaged(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "1.1.3")
	zipPath, _ := writeStagedRuntime(t, "1.1.4")

	a := &App{}
	if !a.applyPendingRuntimeUpdate(LocaleZH, root) {
		t.Fatal("expected the staged runtime to be applied")
	}
	if got := portableVersion(root); got != "1.1.4" {
		t.Fatalf("installed version = %q, want 1.1.4", got)
	}
	if _, ok := readPendingRuntime(); ok {
		t.Fatal("pending marker should be consumed")
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatal("staged zip should be removed after a successful apply")
	}
	if _, err := os.Stat(root + ".previous"); err != nil {
		t.Fatalf("previous tree should be kept until the next successful boot: %v", err)
	}
	cleanupPreviousPortable(root)
	if _, err := os.Stat(root + ".previous"); !os.IsNotExist(err) {
		t.Fatal("previous tree should be cleaned up once boot succeeded")
	}
}

// Checksums are the only thing standing between a truncated download and a
// broken install; a mismatch must leave the current runtime alone.
func TestApplyPendingRuntimeUpdateRejectsBadChecksum(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "1.1.3")
	zipPath, _ := writeStagedRuntime(t, "1.1.4")
	if err := writePendingRuntime(mammothPendingRuntime{
		Version: "1.1.4",
		SHA256:  "deadbeef",
		URL:     "https://example/runtime.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if a.applyPendingRuntimeUpdate(LocaleZH, root) {
		t.Fatal("a bad checksum must never be installed")
	}
	if got := portableVersion(root); got != "1.1.3" {
		t.Fatalf("version = %q, want the untouched 1.1.3", got)
	}
	if _, ok := readPendingRuntime(); ok {
		t.Fatal("a corrupt stage should be dropped, not retried forever")
	}
}

// 2.x runtime under a 1.x shell is not a valid update; the user needs the new
// installer instead.
func TestApplyPendingRuntimeUpdateRefusesMajorJump(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "1.1.4")
	zipPath, _ := writeStagedRuntime(t, "2.0.0")
	if err := writePendingRuntime(mammothPendingRuntime{
		Version: "2.0.0",
		SHA256:  sha256OfFile(t, zipPath),
		URL:     "https://example/runtime.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if a.applyPendingRuntimeUpdate(LocaleZH, root) {
		t.Fatal("a major runtime jump must never be applied silently")
	}
	if got := portableVersion(root); got != "1.1.4" {
		t.Fatalf("version = %q, want 1.1.4", got)
	}
}

func TestStageRuntimeUpdateRefusesMajorJump(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	err := stageRuntimeUpdate("1.1.4", &mammothArtifact{
		Version: "2.0.0",
		URL:     "https://example/runtime.zip",
		SHA256:  "abc",
	}, time.Now())
	if err == nil {
		t.Fatal("staging a cross-major runtime must fail before downloading")
	}
	if _, ok := readPendingRuntime(); ok {
		t.Fatal("nothing should be staged")
	}
}

func TestDiscardStalePendingRuntime(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	zipPath, _ := writeStagedRuntime(t, "1.1.4")
	discardStalePendingRuntime("1.1.4")
	if _, ok := readPendingRuntime(); ok {
		t.Fatal("an already-installed stage should be dropped")
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatal("its zip should be removed too")
	}
}
