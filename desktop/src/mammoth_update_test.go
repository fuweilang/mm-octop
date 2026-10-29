package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPortableNeedsMammothUpdate(t *testing.T) {
	art := &mammothArtifact{Version: "1.0.1", URL: "https://example/x.zip", SHA256: "abc"}
	if !portableNeedsMammothUpdate("1.0.0", art) {
		t.Fatal("expected newer remote to need update")
	}
	if portableNeedsMammothUpdate("1.0.1", art) {
		t.Fatal("same version should not need update")
	}
	if portableNeedsMammothUpdate("1.0.2", art) {
		t.Fatal("newer local should not need update")
	}
	if portableNeedsMammothUpdate("1.0.0", &mammothArtifact{Version: "1.0.1", URL: "https://x", SHA256: ""}) {
		t.Fatal("missing sha256 must refuse update")
	}
}

// A 1.x client must not treat 2.x as a normal update: the shell, Python runtime
// and schema ship together and only the new installer can carry them.
func TestPortableNeedsMammothUpdateBlocksMajorJump(t *testing.T) {
	art := &mammothArtifact{Version: "2.0.0", URL: "https://example/x.zip", SHA256: "abc"}
	if portableNeedsMammothUpdate("1.1.4", art) {
		t.Fatal("a major runtime jump must not look like a routine update")
	}
	if !portableMajorUpgradeAvailable("1.1.4", art) {
		t.Fatal("a major jump should be reported as needing a new client")
	}
	if portableMajorUpgradeAvailable("1.1.4", &mammothArtifact{Version: "1.1.5", URL: "https://x", SHA256: "abc"}) {
		t.Fatal("a minor upgrade is a normal update, not a new client")
	}

	t.Setenv("OCTOP_DESKTOP_ALLOW_MAJOR_UPDATE", "1")
	t.Cleanup(func() { t.Setenv("OCTOP_DESKTOP_ALLOW_MAJOR_UPDATE", "") })
	if !portableNeedsMammothUpdate("1.1.4", art) {
		t.Fatal("the override must still allow cross-major installs for test builds")
	}
}

func TestShellNeedsMammothNotice(t *testing.T) {
	prev := desktopShellVersion
	t.Cleanup(func() { desktopShellVersion = prev })

	desktopShellVersion = "0.0.0"
	if shellNeedsMammothNotice(&mammothArtifact{Version: "9.9.9"}) {
		t.Fatal("unstamped shell version must not notice")
	}

	desktopShellVersion = "1.0.0"
	if !shellNeedsMammothNotice(&mammothArtifact{Version: "1.0.1"}) {
		t.Fatal("expected shell notice")
	}
	if shellNeedsMammothNotice(&mammothArtifact{Version: "1.0.0"}) {
		t.Fatal("same shell version should not notice")
	}
}

func TestFetchMammothLatestAndPlatform(t *testing.T) {
	doc := mammothLatestFile{
		SchemaVersion: 1,
		Platforms: map[string]mammothPlatformLatest{
			"darwin-arm64": {
				Portable: &mammothArtifact{
					Version: "9.9.9",
					URL:     "https://www.mmai.vip/downloads/mammoth-portable-darwin-arm64-9.9.9.zip",
					SHA256:  "deadbeef",
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(server.Close)

	got, err := fetchMammothLatest(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := platformLatestFor(got, "darwin-arm64")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Portable == nil || entry.Portable.Version != "9.9.9" {
		t.Fatalf("portable = %#v", entry.Portable)
	}
	if _, err := platformLatestFor(got, "windows-amd64"); err == nil {
		t.Fatal("expected missing platform error")
	}
}

func TestDownloadFileSHA256AndReplacePortableFromZip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	zipPath := filepath.Join(t.TempDir(), "green.zip")
	writeTestGreenZip(t, zipPath, "2.0.0")
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(home, "cache", "portable.zip")
	got, err := downloadFileSHA256(server.Client(), server.URL, dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sha256HexEqual(got, want) {
		t.Fatalf("sha got %s want %s", got, want)
	}

	root := portableDir()
	if _, err := replacePortableFromZip(root, dest); err != nil {
		t.Fatal(err)
	}
	if !launchReady(root) {
		t.Fatal("replaced portable not launch-ready")
	}
	if portableVersion(root) != "2.0.0" {
		t.Fatalf("version = %q", portableVersion(root))
	}
}

func TestDashboardNeedsMammothUpdate(t *testing.T) {
	remote := &mammothArtifact{
		Version: "1.1.4.1",
		URL:     "https://example/dashboard.zip",
		SHA256:  "abc",
	}
	if !dashboardNeedsMammothUpdate(remote, dashboardStamp{}, "1.1.4") {
		t.Fatal("newer dashboard should need update")
	}
	if dashboardNeedsMammothUpdate(remote, dashboardStamp{Version: "1.1.4.1", SHA256: "abc"}, "1.1.4") {
		t.Fatal("same version and digest should be skipped")
	}
	if !dashboardNeedsMammothUpdate(remote, dashboardStamp{Version: "1.1.4.1", SHA256: "old"}, "1.1.4") {
		t.Fatal("changed digest at same version should re-apply")
	}
	if dashboardNeedsMammothUpdate(&mammothArtifact{Version: "1.1.4", URL: "https://x", SHA256: "abc"}, dashboardStamp{}, "1.1.4") {
		t.Fatal("dashboard not newer than runtime should be skipped")
	}
	if dashboardNeedsMammothUpdate(&mammothArtifact{Version: "1.1.5", URL: "https://x"}, dashboardStamp{}, "1.1.4") {
		t.Fatal("missing sha256 must refuse update")
	}
	if dashboardNeedsMammothUpdate(nil, dashboardStamp{}, "1.1.4") {
		t.Fatal("nil artifact must be skipped")
	}
}

func TestDownloadFileSHA256ResumesPartial(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	payload := []byte(strings.Repeat("mammoth-resume-", 4096))
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])

	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		if rng := r.Header.Get("Range"); rng != "" {
			var start int
			if _, err := fmt.Sscanf(rng, "bytes=%d-", &start); err != nil {
				t.Errorf("bad range %q", rng)
				start = 0
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:])
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(home, "cache", "dashboard.zip")
	half := len(payload) / 2
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest+".partial", payload[:half], 0o644); err != nil {
		t.Fatal(err)
	}

	var lastDone, lastTotal int64
	got, err := downloadFileSHA256(server.Client(), server.URL, dest, func(done, total int64) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sha256HexEqual(got, want) {
		t.Fatalf("sha got %s want %s", got, want)
	}
	if len(ranges) != 1 || ranges[0] != fmt.Sprintf("bytes=%d-", half) {
		t.Fatalf("range requests = %#v", ranges)
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("progress = %d/%d", lastDone, lastTotal)
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatal("partial file should be renamed away after success")
	}
}

func TestReplaceDashboardFromZip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	root := portableDir()
	oldDir := dashboardDir(root)
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "dashboard.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range map[string]string{
		"dashboard/index.html":    "<html>new</html>",
		"dashboard/assets/app.js": "console.log(1)",
	} {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := replaceDashboardFromZip(root, zipPath); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(oldDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<html>new</html>" {
		t.Fatalf("index.html = %q", body)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "assets", "app.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldDir + ".previous"); err != nil {
		t.Fatalf("displaced dashboard should be kept for rollback: %v", err)
	}
}

func TestReplaceDashboardFromZipRejectsMissingIndex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	root := portableDir()
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create("dashboard/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := replaceDashboardFromZip(root, zipPath); err == nil {
		t.Fatal("expected missing index.html to fail")
	}
}

func TestReplaceDashboardFromZipKeepsPreviousForRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	root := portableDir()
	target := dashboardDir(root)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "dashboard.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.Create("dashboard/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	previous, err := replaceDashboardFromZip(root, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if previous == "" {
		t.Fatal("expected the displaced dashboard to be kept")
	}
	if body, err := os.ReadFile(filepath.Join(previous, "index.html")); err != nil || string(body) != "old" {
		t.Fatalf("previous dashboard = %q, err %v", body, err)
	}

	if err := restorePreviousDir(target, previous); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(target, "index.html")); err != nil || string(body) != "old" {
		t.Fatalf("restored dashboard = %q, err %v", body, err)
	}
	if _, err := os.Stat(target + ".broken"); !os.IsNotExist(err) {
		t.Fatal("broken dir should be cleaned up after rollback")
	}
}

func TestReplacePortableFromZipKeepsPreviousForRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	root := portableDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "green.zip")
	writeTestGreenZip(t, zipPath, "3.0.0")

	previous, err := replacePortableFromZip(root, zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if previous == "" {
		t.Fatal("expected the displaced portable to be kept")
	}
	if _, err := os.Stat(filepath.Join(previous, "stale.txt")); err != nil {
		t.Fatalf("previous portable missing: %v", err)
	}

	if err := restorePreviousDir(root, previous); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "stale.txt")); err != nil {
		t.Fatalf("restored portable missing: %v", err)
	}
}

func TestRestorePreviousDirWithoutPreviousFails(t *testing.T) {
	if err := restorePreviousDir(t.TempDir()+"/target", ""); err == nil {
		t.Fatal("expected an error when there is no previous build")
	}
}

func TestProbeDashboardServes(t *testing.T) {
	withRoot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="root"></div></body></html>`))
	}))
	t.Cleanup(withRoot.Close)
	if err := probeDashboardServes(withRoot.URL); err != nil {
		t.Fatalf("healthy dashboard rejected: %v", err)
	}

	whiteScreen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("moved"))
	}))
	t.Cleanup(whiteScreen.Close)
	if err := probeDashboardServes(whiteScreen.URL); err == nil {
		t.Fatal("expected a bundle without #root to be rejected")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	if err := probeDashboardServes(failing.URL); err == nil {
		t.Fatal("expected HTTP 500 to be rejected")
	}
}

func TestDownloadFileSHA256RetriesTransientFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	prevBackoff := mammothDownloadBackoff
	mammothDownloadBackoff = time.Millisecond
	t.Cleanup(func() { mammothDownloadBackoff = prevBackoff })

	payload := []byte(strings.Repeat("retry-me-", 1024))
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(home, "cache", "portable.zip")
	got, err := downloadFileSHA256(server.Client(), server.URL, dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sha256HexEqual(got, want) {
		t.Fatalf("sha got %s want %s", got, want)
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestDownloadFileSHA256DoesNotRetryMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	prevBackoff := mammothDownloadBackoff
	mammothDownloadBackoff = time.Millisecond
	t.Cleanup(func() { mammothDownloadBackoff = prevBackoff })

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(home, "cache", "missing.zip")
	if _, err := downloadFileSHA256(server.Client(), server.URL, dest, nil); err == nil {
		t.Fatal("expected 404 to fail")
	}
	if calls != 1 {
		t.Fatalf("404 must not be retried, got %d attempts", calls)
	}
}

func TestDownloadFileSHA256GivesUpAfterMaxAttempts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)

	prevBackoff := mammothDownloadBackoff
	mammothDownloadBackoff = time.Millisecond
	t.Cleanup(func() { mammothDownloadBackoff = prevBackoff })

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	dest := filepath.Join(home, "cache", "down.zip")
	if _, err := downloadFileSHA256(server.Client(), server.URL, dest, nil); err == nil {
		t.Fatal("expected persistent 502 to fail")
	}
	if calls != downloadMaxAttempts {
		t.Fatalf("attempts = %d, want %d", calls, downloadMaxAttempts)
	}
}
