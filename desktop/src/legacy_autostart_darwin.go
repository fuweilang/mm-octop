//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// disableLegacyAutostart unloads and moves LaunchAgents that start the legacy
// 「猛犸AI」 app. Returns the backup paths it wrote.
//
// Login items registered through System Settings (SMAppService) are not plist
// files; those are left to the user, which is why the settings window also
// offers a "reveal in Finder" action.
func disableLegacyAutostart(legacyApp string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	agentsDir := filepath.Join(home, "Library", "LaunchAgents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}
	uid := strconv.Itoa(os.Getuid())
	var moved []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".plist") {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil || !mentionsLegacyAutostart(string(data), legacyApp) {
			continue
		}
		// Unload before moving: a still-loaded agent keeps running (and keeps
		// holding 8088) even after its plist is gone.
		_ = exec.Command("launchctl", "bootout", "gui/"+uid, path).Run()
		_ = exec.Command("launchctl", "unload", path).Run()
		dest, err := moveToLegacyAutostartBackup(path)
		if err != nil {
			continue
		}
		moved = append(moved, dest)
	}
	return moved
}

func revealInFileManager(path string) error {
	return exec.Command("open", "-R", path).Run()
}
