package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The boot log must keep exactly two generations so a broken client cannot
// fill the disk.
func TestBootLogRotatesOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	current := bootLogPath(bootLogName)
	if err := os.WriteFile(current, []byte(strings.Repeat("x", bootLogMaxSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendBootLog(bootLogEntry{ID: "new", Phase: "ready"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bootLogPath(bootLogRotatedName)); err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	entries := readBootLog()
	if len(entries) != 1 || entries[0].ID != "new" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	// A second rotation replaces the old generation: two files max, and the
	// oldest record is gone rather than growing the directory forever.
	if err := os.WriteFile(current, []byte(strings.Repeat("y", bootLogMaxSize)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendBootLog(bootLogEntry{ID: "newer", Phase: "ready"}); err != nil {
		t.Fatal(err)
	}
	entries = readBootLog()
	if len(entries) != 1 || entries[0].ID != "newer" {
		t.Fatalf("expected the old generation to be dropped, got %+v", entries)
	}
	names, err := filepath.Glob(filepath.Join(home, "logs", "client-boot*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("want 2 generations, got %v", names)
	}
}

func TestBootLogMarksUploaded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCTOP_HOME", home)
	if err := appendBootLog(bootLogEntry{ID: "a", Phase: "shell_start"}); err != nil {
		t.Fatal(err)
	}
	if err := appendBootLog(bootLogEntry{ID: "b", Phase: "ready"}); err != nil {
		t.Fatal(err)
	}
	markBootUploaded(map[string]bool{"a": true})

	entries := readBootLog()
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %+v", entries)
	}
	if !entries[0].Uploaded || entries[1].Uploaded {
		t.Fatalf("unexpected uploaded flags: %+v", entries)
	}

	// The pending line is what the next boot retries.
	pending := 0
	for _, entry := range entries {
		if !entry.Uploaded {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("want exactly one pending line, got %d", pending)
	}
}

func TestScrubBootText(t *testing.T) {
	got := scrubBootText("Authorization: Bearer abc.def token=xyz 很长的内容", 400)
	if strings.Contains(got, "abc.def") || strings.Contains(got, "xyz") {
		t.Fatalf("credential survived scrubbing: %s", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("no redaction marker: %s", got)
	}
	if short := scrubBootText("abcdefghij", 4); short != "abc…" {
		t.Fatalf("clip = %q", short)
	}
}