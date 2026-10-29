package main

// End-to-end smoke for the layered (SPA-only) update.
//
// One command:
//   cd desktop/src && go test -run TestMammothUpdateEndToEnd ./...
//
// It exercises the real chain — latest.json → platform entry → download →
// sha256 → swap dashboard dir → restart → probe — plus the rollback branch
// when the freshly installed SPA does not serve. Only the process side
// (stop/restart Octop) is faked, via updateRuntime, so the test never spawns
// a runtime or takes over a real port.

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dashboardE2EOldHTML = `<!doctype html><html><body><div id="root">old-build</div></body></html>`

// dashboardZip builds a release-shaped dashboard zip (single top-level
// "dashboard/" folder, as unzipGreen strips the first segment).
func dashboardZip(t *testing.T, html string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("dashboard/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(html)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// e2eEnv is one isolated install: OCTOP_HOME, a portable root carrying the
// current runtime version, and the release server the update fetches from.
type e2eEnv struct {
	root    string
	dash    string
	server  *httptest.Server
	publish func(version, html string)
}

func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	// Isolate ~/.octop so the download cache and dashboard stamp land in tmp.
	t.Setenv("OCTOP_HOME", t.TempDir())

	root := portableDir()
	dash := dashboardDir(root)
	if err := os.MkdirAll(dash, 0o755); err != nil {
		t.Fatal(err)
	}
	// Atomic writes replace VERSION.txt at runtime; keep the path stable.
	writeFile(t, filepath.Join(root, "VERSION.txt"), "octop_version=1.0.0\n")
	writeFile(t, filepath.Join(dash, "index.html"), dashboardE2EOldHTML)

	env := &e2eEnv{root: root, dash: dash}

	// Serve whatever is currently on disk so the restart seam can be an
	// httptest server and the post-swap probe hits the real files.
	static := httptest.NewServer(http.FileServer(http.Dir(dash)))
	t.Cleanup(static.Close)

	prev := updateRuntime
	updateRuntime = updateRuntimeOps{
		stop: func(*App) (string, int) { return "127.0.0.1", 18088 },
		restart: func(*App, Locale, string, string, int) (string, error) {
			return static.URL, nil
		},
	}
	t.Cleanup(func() { updateRuntime = prev })

	mux := http.NewServeMux()
	env.server = httptest.NewServer(mux)
	t.Cleanup(env.server.Close)

	var latest []byte
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(latest)
	})
	// Every zip is registered per version by the test.
	zips := map[string][]byte{}
	mux.HandleFunc("/dashboard/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dashboard/")
		blob, ok := zips[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(blob)
	})

	// publish registers one dashboard artifact and rewrites latest.json.
	env.publish = func(version, html string) {
		blob := dashboardZip(t, html)
		sum := sha256.Sum256(blob)
		file := version + ".zip"
		zips[file] = blob
		doc := mammothLatestFile{
			SchemaVersion: 1,
			Dashboard: &mammothArtifact{
				Version: version,
				URL:     env.server.URL + "/dashboard/" + file,
				SHA256:  hex.EncodeToString(sum[:]),
			},
			Platforms: map[string]mammothPlatformLatest{greenPlat(): {}},
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		latest = raw
		t.Setenv("OCTOP_MAMMOTH_LATEST_URL", env.server.URL+"/latest.json")
	}
	return env
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestMammothUpdateEndToEnd(t *testing.T) {
	env := newE2EEnv(t)
	app := &App{dashboardURL: "http://127.0.0.1:18088/"}

	t.Run("installs the newer SPA and stamps it", func(t *testing.T) {
		const nextHTML = `<!doctype html><html><body><div id="root">new-build</div></body></html>`
		env.publish("1.0.1", nextHTML)

		app.runMammothUpdate()

		if got := readFile(t, filepath.Join(env.dash, "index.html")); got != nextHTML {
			t.Fatalf("dashboard not swapped, got %q", got)
		}
		if _, err := os.Stat(env.dash + ".previous"); !os.IsNotExist(err) {
			t.Fatalf("successful update must drop the rollback copy (err=%v)", err)
		}
		stamp := readDashboardStamp()
		if stamp.Version != "1.0.1" {
			t.Fatalf("stamp version = %q, want 1.0.1", stamp.Version)
		}
	})

	t.Run("keeps the same version out of the download path", func(t *testing.T) {
		// Same version + same sha as the stamp above: nothing to do.
		app.runMammothUpdate()
		if got := readFile(t, filepath.Join(env.dash, "index.html")); !strings.Contains(got, "new-build") {
			t.Fatalf("repeat check must not touch the dashboard, got %q", got)
		}
	})

	t.Run("rolls back when the new bundle does not serve", func(t *testing.T) {
		// No #root and no <script>: probeDashboardServes must reject it.
		env.publish("2.0.0", "not an spa bundle")

		app.runMammothUpdate()

		got := readFile(t, filepath.Join(env.dash, "index.html"))
		if !strings.Contains(got, "new-build") {
			t.Fatalf("rollback did not restore the last good dashboard, got %q", got)
		}
		if stamp := readDashboardStamp(); stamp.Version != "1.0.1" {
			t.Fatalf("failed update must not advance the stamp, got %q", stamp.Version)
		}
		if _, err := os.Stat(env.dash + ".new"); !os.IsNotExist(err) {
			t.Fatalf("failed update left a .new tree behind (err=%v)", err)
		}
	})
}