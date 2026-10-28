package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMentionsLegacyAutostartByDisplayName(t *testing.T) {
	entry := `<?xml version="1.0"?><string>/Applications/猛犸AI.app/Contents/MacOS/猛犸AI</string>`
	if !mentionsLegacyAutostart(entry, "") {
		t.Fatal("the legacy display name must match")
	}
}

func TestMentionsLegacyAutostartByLegacyPath(t *testing.T) {
	entry := `open "/Applications/猛犸AI.app"`
	if !mentionsLegacyAutostart(entry, "/Applications/猛犸AI.app") {
		t.Fatal("the legacy app path must match")
	}
}

func TestMentionsLegacyAutostartIgnoresSharedBundleID(t *testing.T) {
	// Both apps share com.tencent.octop, so a bundle-id-only entry proves
	// nothing — matching it would disable the *new* app's own login item.
	entry := `<string>com.tencent.octop</string>`
	if mentionsLegacyAutostart(entry, "") {
		t.Fatal("a bare bundle id must not be treated as the legacy app")
	}
}

func TestMoveToLegacyAutostartBackupKeepsContent(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "com.tencent.octop.legacy.plist")
	if err := os.WriteFile(source, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	dest, err := moveToLegacyAutostartBackup(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("the original entry must be moved, not copied")
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "original" {
		t.Fatalf("backup content = %q, err = %v", data, err)
	}
	if !strings.HasPrefix(dest, legacyAutostartBackupDir()) {
		t.Fatalf("backup %q is outside %q", dest, legacyAutostartBackupDir())
	}
}

func TestMoveToLegacyAutostartBackupDoesNotOverwrite(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	first := filepath.Join(t.TempDir(), "entry.plist")
	second := filepath.Join(t.TempDir(), "entry.plist")
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	firstDest, err := moveToLegacyAutostartBackup(first)
	if err != nil {
		t.Fatal(err)
	}
	secondDest, err := moveToLegacyAutostartBackup(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDest == secondDest {
		t.Fatal("a second entry with the same name must not clobber the first")
	}
}
