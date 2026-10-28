//go:build !darwin && !windows

package main

import (
	"os/exec"
	"path/filepath"
)

// The Linux desktop build is not shipped with the Mammoth channel yet, so there
// is no legacy build to clean up. Keep the surface identical for main.go.

func disableLegacyAutostart(string) []string { return nil }

func revealInFileManager(path string) error {
	return exec.Command("xdg-open", filepath.Dir(path)).Run()
}
