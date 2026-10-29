package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCodeZip(t *testing.T, path, marker string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range map[string]string{
		"octop/__main__.py":       "# " + marker,
		"octop/infra/__init__.py": "# " + marker,
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
}

// readyCodeTree lays down packages/octop with an SPA next to the code, the way
// a portable runtime ships it.
func readyCodeTree(t *testing.T, root, marker string) string {
	t.Helper()
	codeDir := filepath.Join(root, "packages", "octop")
	if err := os.MkdirAll(filepath.Join(codeDir, "dashboard"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "__main__.py"), []byte("# "+marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "dashboard", "index.html"), []byte("<html>"+marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return codeDir
}

func TestCodePatchNeedsUpdate(t *testing.T) {
	art := &mammothCodeArtifact{
		Version: "1.1.5.1",
		URL:     "https://example/code.zip",
		SHA256:  "abc",
		Runtime: "1.1.5",
	}
	if !codePatchNeedsUpdate(art, codePatchStamp{}, "1.1.5") {
		t.Fatal("a newer patch for this runtime should apply")
	}
	if codePatchNeedsUpdate(art, codePatchStamp{}, "1.1.4") {
		t.Fatal("a patch built for 1.1.5 must not land on 1.1.4")
	}
	if codePatchNeedsUpdate(art, codePatchStamp{Version: "1.1.5.1", SHA256: "abc"}, "1.1.5") {
		t.Fatal("an already applied patch must not be downloaded again")
	}
	if codePatchNeedsUpdate(&mammothCodeArtifact{Version: "1.1.5", URL: "https://x", SHA256: "abc"}, codePatchStamp{}, "1.1.5") {
		t.Fatal("a patch that is not newer than the runtime is already bundled")
	}
	if codePatchNeedsUpdate(&mammothCodeArtifact{Version: "1.1.5.1", URL: "https://x"}, codePatchStamp{}, "1.1.5") {
		t.Fatal("missing sha256 must refuse the patch")
	}
	if codePatchNeedsUpdate(nil, codePatchStamp{}, "1.1.5") {
		t.Fatal("no code entry must be a no-op")
	}
}

func TestPendingCodeRoundTrip(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	if _, ok := readPendingCode(); ok {
		t.Fatal("expected no pending code initially")
	}
	want := mammothPendingCode{
		Version: "1.1.5.1",
		Runtime: "1.1.5",
		SHA256:  "abc",
		URL:     "https://x",
		ZipPath: "/tmp/code.zip",
	}
	if err := writePendingCode(want); err != nil {
		t.Fatal(err)
	}
	got, ok := readPendingCode()
	if !ok || got.Version != want.Version || got.Runtime != want.Runtime {
		t.Fatalf("pending = %#v, ok = %v", got, ok)
	}
	clearPendingCode()
	if _, ok := readPendingCode(); ok {
		t.Fatal("pending code should be cleared")
	}
}

func TestApplyPendingCodePatchSwapsCodeAndKeepsSPA(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "1.1.5")
	codeDir := readyCodeTree(t, root, "old")
	zipPath := filepath.Join(mammothCacheDir(), "code-1.1.5.1.zip")
	writeCodeZip(t, zipPath, "new")
	digest := sha256OfFile(t, zipPath)
	if err := writePendingCode(mammothPendingCode{
		Version: "1.1.5.1",
		Runtime: "1.1.5",
		SHA256:  digest,
		URL:     "https://example/code.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}

	a := &App{}
	applied, previous := a.applyPendingCodePatch(LocaleZH, root)
	if !applied {
		t.Fatal("expected the staged code patch to be applied")
	}
	if _, err := os.Stat(previous); err != nil {
		t.Fatalf("previous code tree should be kept for rollback: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(codeDir, "__main__.py"))
	if err != nil || string(body) != "# new" {
		t.Fatalf("__main__.py = %q, err %v", body, err)
	}
	// The SPA ships on its own channel and must survive a code-only swap.
	spa, err := os.ReadFile(filepath.Join(codeDir, "dashboard", "index.html"))
	if err != nil || string(spa) != "<html>old" {
		t.Fatalf("dashboard/index.html = %q, err %v", spa, err)
	}
	if stamp := readCodePatchStamp(); stamp.Version != "1.1.5.1" {
		t.Fatalf("stamp = %#v", stamp)
	}
	if _, ok := readPendingCode(); ok {
		t.Fatal("pending marker should be consumed")
	}
}

func TestApplyPendingCodePatchRefusesOtherRuntime(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "1.1.4")
	codeDir := readyCodeTree(t, root, "old")
	zipPath := filepath.Join(mammothCacheDir(), "code-1.1.5.1.zip")
	writeCodeZip(t, zipPath, "new")
	if err := writePendingCode(mammothPendingCode{
		Version: "1.1.5.1",
		Runtime: "1.1.5",
		SHA256:  sha256OfFile(t, zipPath),
		URL:     "https://example/code.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if applied, _ := a.applyPendingCodePatch(LocaleZH, root); applied {
		t.Fatal("a patch for 1.1.5 must not install on 1.1.4")
	}
	body, err := os.ReadFile(filepath.Join(codeDir, "__main__.py"))
	if err != nil || string(body) != "# old" {
		t.Fatalf("__main__.py = %q, err %v", body, err)
	}
}

func TestApplyPendingCodePatchRejectsBadChecksum(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	root := readyPortable(t, "1.1.5")
	readyCodeTree(t, root, "old")
	zipPath := filepath.Join(mammothCacheDir(), "code-1.1.5.1.zip")
	writeCodeZip(t, zipPath, "new")
	if err := writePendingCode(mammothPendingCode{
		Version: "1.1.5.1",
		Runtime: "1.1.5",
		SHA256:  "deadbeef",
		URL:     "https://example/code.zip",
		ZipPath: zipPath,
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if applied, _ := a.applyPendingCodePatch(LocaleZH, root); applied {
		t.Fatal("a bad checksum must never be installed")
	}
	if _, ok := readPendingCode(); ok {
		t.Fatal("a corrupt stage should be dropped")
	}
}

func TestStageCodePatchSkipsWhenNotNeeded(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	art := &mammothCodeArtifact{Version: "1.1.5", URL: "https://x", SHA256: "abc", Runtime: "1.1.5"}
	if err := stageCodePatch(time.Now(), art, "1.1.5"); err != nil {
		t.Fatal(err)
	}
	if _, ok := readPendingCode(); ok {
		t.Fatal("nothing should be staged when the runtime already bundles the code")
	}
}
