package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
	got, err := downloadFileSHA256(server.Client(), server.URL, dest)
	if err != nil {
		t.Fatal(err)
	}
	if !sha256HexEqual(got, want) {
		t.Fatalf("sha got %s want %s", got, want)
	}

	root := portableDir()
	if err := replacePortableFromZip(root, dest); err != nil {
		t.Fatal(err)
	}
	if !launchReady(root) {
		t.Fatal("replaced portable not launch-ready")
	}
	if portableVersion(root) != "2.0.0" {
		t.Fatalf("version = %q", portableVersion(root))
	}
}
