package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDashboardZip(t *testing.T, path, indexHTML string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create("dashboard/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(indexHTML)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// readyPortable lays down a launch-ready runtime so prefetch/apply can run.
func readyPortable(t *testing.T, version string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "green.zip")
	writeTestGreenZip(t, zipPath, version)
	root := portableDir()
	if _, err := replacePortableFromZip(root, zipPath); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMammothCheckThrottle(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	now := time.Now()

	if !mammothCheckDue(now) {
		t.Fatal("first check must be due")
	}
	if err := markMammothChecked(now); err != nil {
		t.Fatal(err)
	}
	if mammothCheckDue(now.Add(time.Hour)) {
		t.Fatal("check within the interval must be skipped")
	}
	if !mammothCheckDue(now.Add(mammothCheckInterval + time.Minute)) {
		t.Fatal("check after the interval must be due again")
	}
}

func TestMammothCheckDueToleratesGarbage(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mammothLastCheckPath(), []byte("not a timestamp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mammothCheckDue(time.Now()) {
		t.Fatal("unreadable timestamp should be treated as due")
	}
}

func TestPendingUpdateRoundTrip(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	if _, ok := readPendingUpdate(); ok {
		t.Fatal("expected no pending update initially")
	}
	want := mammothPendingUpdate{Version: "1.1.4.1", SHA256: "abc", URL: "https://x", ZipPath: "/tmp/x.zip"}
	if err := writePendingUpdate(want); err != nil {
		t.Fatal(err)
	}
	got, ok := readPendingUpdate()
	if !ok || got.Version != want.Version || got.ZipPath != want.ZipPath {
		t.Fatalf("pending = %#v, ok = %v", got, ok)
	}
	clearPendingUpdate()
	if _, ok := readPendingUpdate(); ok {
		t.Fatal("pending update should be cleared")
	}
}

func TestApplyPendingDashboardUpdate(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "2.0.0")

	oldDir := dashboardDir(root)
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(mammothCacheDir(), "dashboard-2.0.0.1.zip")
	writeDashboardZip(t, zipPath, "new")
	if err := writePendingUpdate(mammothPendingUpdate{
		Version: "2.0.0.1",
		SHA256:  "abc",
		URL:     "https://example/dashboard.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}

	if !applyPendingDashboardUpdate(root) {
		t.Fatal("expected the staged dashboard to be applied")
	}
	body, err := os.ReadFile(filepath.Join(dashboardDir(root), "index.html"))
	if err != nil || string(body) != "new" {
		t.Fatalf("index.html = %q, err %v", body, err)
	}
	if stamp := readDashboardStamp(); stamp.Version != "2.0.0.1" {
		t.Fatalf("stamp = %#v", stamp)
	}
	if _, ok := readPendingUpdate(); ok {
		t.Fatal("pending marker should be consumed")
	}
}

func TestApplyPendingDashboardUpdateSkipsMissingZip(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "2.0.0")
	if err := writePendingUpdate(mammothPendingUpdate{
		Version: "2.0.0.1",
		SHA256:  "abc",
		URL:     "https://example/dashboard.zip",
		ZipPath: filepath.Join(mammothCacheDir(), "gone.zip"),
	}); err != nil {
		t.Fatal(err)
	}
	if applyPendingDashboardUpdate(root) {
		t.Fatal("missing zip must not be applied")
	}
}

func TestApplyPendingDashboardUpdateSkipsAlreadyApplied(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "2.0.0")
	zipPath := filepath.Join(mammothCacheDir(), "dashboard-2.0.0.1.zip")
	writeDashboardZip(t, zipPath, "new")
	if err := writeDashboardStamp(dashboardStamp{Version: "2.0.0.1", SHA256: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := writePendingUpdate(mammothPendingUpdate{
		Version: "2.0.0.1",
		SHA256:  "abc",
		URL:     "https://example/dashboard.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}

	if applyPendingDashboardUpdate(root) {
		t.Fatal("an already applied bundle must not be re-applied")
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatal("stale staged zip should be removed")
	}
}

func TestAutoUpdateDisabled(t *testing.T) {
	t.Setenv("OCTOP_DESKTOP_AUTO_UPDATE", "0")
	if !autoUpdateDisabled() {
		t.Fatal("OCTOP_DESKTOP_AUTO_UPDATE=0 must disable prefetch")
	}
	t.Setenv("OCTOP_DESKTOP_AUTO_UPDATE", "1")
	if autoUpdateDisabled() {
		t.Fatal("default must keep prefetch enabled")
	}
}
