package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsMigratesLegacyPreventSleepMac(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("OCTOP_HOME", temp)
	if err := os.WriteFile(
		filepath.Join(temp, "desktop-settings.json"),
		[]byte(`{"preventSleepMac":true}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if !loadSettings().PreventSleep {
		t.Fatal("legacy preventSleepMac should migrate to preventSleep")
	}
}

func TestCandidatePortsIncludeBusyStoredAlternate(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	store := &settingsStore{cur: loadSettings()}
	app := &App{store: store}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	alternate := ln.Addr().(*net.TCPAddr).Port
	saved := store.get()
	saved.AlternatePort = alternate
	if err := store.save(saved); err != nil {
		t.Fatal(err)
	}

	// The alternate is busy, so preferredPort falls back to the configured
	// port. The candidate list must still contain the alternate, otherwise its
	// leftover instance is never found and cleaned (8088 + 8089 double-run).
	preferred := app.preferredPort("127.0.0.1", 8088)
	if preferred != 8088 {
		t.Fatalf("preferred = %d, want 8088", preferred)
	}
	ports := app.candidatePorts(preferred, 8088)
	for _, port := range ports {
		if port == alternate {
			return
		}
	}
	t.Fatalf("candidates %v must include the stored alternate %d", ports, alternate)
}

func TestCandidatePortsDedupe(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	app := &App{store: &settingsStore{cur: loadSettings()}}

	ports := app.candidatePorts(8088, 8088)
	if len(ports) != 1 || ports[0] != 8088 {
		t.Fatalf("candidates = %v, want [8088]", ports)
	}
}

func TestPreferredPortKeepsAcceptedAlternate(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	store := &settingsStore{cur: loadSettings()}
	app := &App{store: store}
	alternate := freeTCPPort(t)
	saved := store.get()
	saved.AlternatePort = alternate
	if err := store.save(saved); err != nil {
		t.Fatal(err)
	}

	// 8088 is free here, but the user already accepted another port; switching
	// back would change the origin and sign them out again.
	if got := app.preferredPort("127.0.0.1", 8088); got != alternate {
		t.Fatalf("port = %d, want the accepted alternate %d", got, alternate)
	}
}

func TestPreferredPortIgnoresBusyAlternate(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	store := &settingsStore{cur: loadSettings()}
	app := &App{store: store}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	saved := store.get()
	saved.AlternatePort = ln.Addr().(*net.TCPAddr).Port
	if err := store.save(saved); err != nil {
		t.Fatal(err)
	}

	if got := app.preferredPort("127.0.0.1", 8088); got != 8088 {
		t.Fatalf("port = %d, want the configured port when the alternate is busy", got)
	}
}

func TestRememberListenPortPersistsOnlyAlternates(t *testing.T) {
	t.Setenv("OCTOP_HOME", t.TempDir())
	store := &settingsStore{cur: loadSettings()}
	app := &App{store: store}

	app.rememberListenPort(8088, 8088)
	if got := store.get().AlternatePort; got != 0 {
		t.Fatalf("default port must not be persisted, got %d", got)
	}

	app.rememberListenPort(8090, 8088)
	if got := store.get().AlternatePort; got != 8090 {
		t.Fatalf("alternate port = %d", got)
	}
	if reloaded := loadSettings(); reloaded.AlternatePort != 8090 {
		t.Fatalf("alternate port did not survive a reload: %d", reloaded.AlternatePort)
	}

	// Back on the configured port: drop the override so we do not fight the
	// canonical origin forever.
	app.rememberListenPort(8088, 8088)
	if got := store.get().AlternatePort; got != 0 {
		t.Fatalf("returning to the configured port must clear the override, got %d", got)
	}
}
