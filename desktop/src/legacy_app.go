package main

// MAMMOTH-PATCH: legacy 「猛犸AI」 removal.
//
// The new build and the old one share the bundle id com.tencent.octop, so both
// can sit in /Applications. We never delete silently: the user is asked, the
// app is moved to the Trash (macOS) or removed by its own uninstaller
// (Windows), and "later" is remembered in ~/.octop/config.json.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const legacyAppDismissedKey = "desktop_legacy_app_dismissed"

// legacyAppReport is what the settings window renders.
type legacyAppReport struct {
	Path     string `json:"path"`
	CanTrash bool   `json:"canTrash"`
}

func (a *App) legacyAppPath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.legacyApp
}

func (a *App) setLegacyApp(path string) {
	a.mu.Lock()
	a.legacyApp = path
	a.mu.Unlock()
}

// CheckLegacyApp re-runs detection; the settings window uses it as the
// "检查旧版" entry after the user chose "later".
func (a *App) CheckLegacyApp() legacyAppReport {
	path := detectLegacySiblingInstall()
	a.setLegacyApp(path)
	if path == "" {
		return legacyAppReport{}
	}
	return legacyAppReport{Path: path, CanTrash: canTrashLegacyApp(path)}
}

// RemoveLegacyApp moves the old app to the Trash (macOS) or runs its silent
// uninstaller (Windows). Returns "" on success, else a copy.json key telling
// the user to remove it by hand.
func (a *App) RemoveLegacyApp() string {
	path := a.legacyAppPath()
	if path == "" {
		return ""
	}
	log.Printf("desktop: removing legacy install %s", path)
	reason, err := trashLegacyApp(path)
	if err != nil {
		log.Printf("desktop: remove legacy install %s failed: %v", path, err)
		return reason
	}
	if _, statErr := os.Stat(path); statErr == nil {
		log.Printf("desktop: legacy install still present at %s", path)
		return copyLegacyManual
	}
	log.Printf("desktop: legacy install removed: %s", path)
	a.setLegacyApp("")
	return ""
}

// DismissLegacyApp remembers "later" so the prompt does not show again. The
// settings window can still re-run the check.
func (a *App) DismissLegacyApp() bool {
	if err := setConfigFlag(legacyAppDismissedKey, true); err != nil {
		log.Printf("desktop: remember legacy dismissal: %v", err)
		return false
	}
	log.Printf("desktop: user chose to handle the legacy app later")
	return true
}

// OpenLegacyAppFolder reveals the old app when it cannot be removed
// automatically (permission denied, no uninstaller found).
func (a *App) OpenLegacyAppFolder() bool {
	path := a.legacyAppPath()
	if path == "" {
		return false
	}
	if err := revealInFileManager(path); err != nil {
		log.Printf("desktop: reveal %s: %v", path, err)
		return false
	}
	return true
}

// promptLegacyApp runs detection off the boot path and, when an old install is
// still around, opens the settings window with the removal prompt. Bounded by a
// timeout so a slow Spotlight query cannot delay anything.
func (a *App) promptLegacyApp() {
	done := make(chan string, 1)
	go func() { done <- detectLegacySiblingInstall() }()

	var path string
	select {
	case path = <-done:
	case <-time.After(5 * time.Second):
		log.Printf("desktop: legacy app detection timed out")
		return
	}
	if path == "" {
		return
	}
	a.setLegacyApp(path)
	// Stop the old login item as well: otherwise the legacy build keeps
	// reclaiming 8088 even when the user is not using it.
	if moved := disableLegacyAutostart(path); len(moved) > 0 {
		log.Printf("desktop: moved legacy autostart entries to %s", strings.Join(moved, ", "))
	}
	if configFlag(legacyAppDismissedKey) {
		log.Printf("desktop: legacy install %s (removal deferred by user)", path)
		return
	}
	log.Printf("desktop: legacy install %s found; prompting", path)
	a.mu.Lock()
	win := a.settingsWindow
	a.mu.Unlock()
	if win == nil {
		return
	}
	win.Show()
	if a.app != nil {
		a.app.Event.Emit("desktop:legacy-app", path)
	}
}

// configFlag reads a boolean from ~/.octop/config.json without touching the
// other keys the runtime owns.
func configFlag(key string) bool {
	data, err := os.ReadFile(filepath.Join(octopHome(), "config.json"))
	if err != nil {
		return false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	var value bool
	if json.Unmarshal(raw[key], &value) != nil {
		return false
	}
	return value
}

// setConfigFlag merges one boolean into ~/.octop/config.json.
func setConfigFlag(key string, value bool) error {
	path := filepath.Join(octopHome(), "config.json")
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw[key] = encoded
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}
