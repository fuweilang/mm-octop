//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// canTrashLegacyApp reports whether we know how to remove the app. macOS can
// always move it to the Trash; the user may still have to confirm Finder
// automation the first time.
func canTrashLegacyApp(string) bool { return true }

// trashLegacyApp routes the old .app to the user's Trash through Finder, which
// keeps it restorable. Never rm -rf: the shared bundle id means a wrong path
// would delete the running app.
func trashLegacyApp(path string) (string, error) {
	if !strings.HasSuffix(path, ".app") {
		return copyLegacyManual, fmt.Errorf("not an app bundle: %s", path)
	}
	if self, err := os.Executable(); err == nil {
		if selfApp := bundleRoot(self); selfApp != "" && samePath(selfApp, path) {
			return copyLegacySelf, fmt.Errorf("refusing to trash the running app: %s", path)
		}
	}
	script := fmt.Sprintf(`tell application "Finder" to delete POSIX file %q`, filepath.Clean(path))
	cmd := exec.Command("osascript", "-e", script)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		if err != nil {
			return copyLegacyManual, err
		}
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		return copyLegacyManual, fmt.Errorf("finder timed out moving %s", path)
	}
	return "", nil
}
