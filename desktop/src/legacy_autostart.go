package main

// MAMMOTH-PATCH: legacy 「猛犸AI」 login items.
//
// The old and new apps share one bundle id, so both can be installed side by
// side and the old one keeps reclaiming 8088 after the new shell replaced its
// runtime. We stop the old login item — never the app itself: entries are moved
// into ~/.octop/backups/legacy-autostart so the user can restore them.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const legacyAutostartBackupDirName = "legacy-autostart"

func legacyAutostartBackupDir() string {
	return filepath.Join(octopHome(), "backups", legacyAutostartBackupDirName)
}

// moveToLegacyAutostartBackup moves path into the backup directory. It never
// deletes: a same-named entry gets a timestamp suffix instead of being clobbered.
func moveToLegacyAutostartBackup(path string) (string, error) {
	dir := legacyAutostartBackupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Base(path)
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); err == nil {
		dest = filepath.Join(dir, fmt.Sprintf("%s.%d", name, time.Now().Unix()))
	}
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// mentionsLegacyAutostart reports whether an autostart entry launches the old
// build. Both apps share com.tencent.octop, so the bundle id alone proves
// nothing: only the legacy display name or the legacy app path count.
func mentionsLegacyAutostart(entry, legacyApp string) bool {
	if strings.Contains(entry, "猛犸AI") {
		return true
	}
	if legacyApp != "" && strings.Contains(entry, legacyApp) {
		return true
	}
	return false
}
