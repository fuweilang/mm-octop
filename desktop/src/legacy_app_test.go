package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dismissal flag lives in the runtime's config.json; it must not disturb
// the keys the server owns.
func TestConfigFlagPreservesOtherKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte("{\"port\": 8088}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if configFlag(legacyAppDismissedKey) {
		t.Fatal("dismissal should default to false")
	}
	if err := setConfigFlag(legacyAppDismissedKey, true); err != nil {
		t.Fatal(err)
	}
	if !configFlag(legacyAppDismissedKey) {
		t.Fatal("dismissal flag was not persisted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"port\"") || !strings.Contains(string(data), "8088") {
		t.Fatalf("config.json lost the runtime keys: %s", data)
	}
}