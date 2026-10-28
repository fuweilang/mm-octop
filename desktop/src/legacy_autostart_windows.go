//go:build windows

package main

import (
	"os/exec"
)

// The legacy 「猛犸AI」 desktop build was macOS-only (same bundle id as the new
// app). Windows installs use the NSIS product name, so there is nothing to
// detect or un-autostart here yet — see dual_install_other.go. Keep the surface
// identical for main.go.

func disableLegacyAutostart(string) []string { return nil }

func revealInFileManager(path string) error {
	return exec.Command("explorer", "/select,"+path).Run()
}
