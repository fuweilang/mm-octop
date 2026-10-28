//go:build windows

package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// canTrashLegacyApp reports whether an uninstaller was found. Windows has no
// Trash for programs: either the old build registered one, or we tell the user
// to remove it from Settings → Apps.
func canTrashLegacyApp(string) bool {
	_, _, ok := legacyUninstaller()
	return ok
}

// trashLegacyApp runs the uninstaller registered by the old build, silently.
// Directories are never deleted by hand.
func trashLegacyApp(path string) (string, error) {
	uninstall, args, ok := legacyUninstaller()
	if !ok {
		return copyLegacyManual, fmt.Errorf("no uninstaller registered for %s", path)
	}
	cmdArgs := strings.Fields(args)
	if len(cmdArgs) == 0 {
		// NSIS/Squirrel silent switches; msiexec needs /qn instead.
		if strings.Contains(strings.ToLower(uninstall), "msiexec") {
			cmdArgs = []string{"/qn"}
		} else {
			cmdArgs = []string{"/S"}
		}
	}
	log.Printf("desktop: running legacy uninstaller %s %v", uninstall, cmdArgs)
	cmd := exec.Command(uninstall, cmdArgs...)
	hideConsole(cmd)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		if err != nil {
			return copyLegacyManual, err
		}
	case <-time.After(5 * time.Minute):
		_ = cmd.Process.Kill()
		return copyLegacyManual, fmt.Errorf("uninstaller timed out: %s", uninstall)
	}
	return "", nil
}

// legacyUninstaller looks for a per-machine or per-user entry whose display
// name is the old 「猛犸AI」 build (the new one is 「猛犸智能体」).
func legacyUninstaller() (string, string, bool) {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			if uninstall, args, ok := scanUninstallKey(root, view); ok {
				return uninstall, args, true
			}
		}
	}
	return "", "", false
}

func scanUninstallKey(root registry.Key, view uint32) (string, string, bool) {
	const base = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	key, err := registry.OpenKey(root, base, registry.READ|view)
	if err != nil {
		return "", "", false
	}
	defer key.Close()
	names, err := key.ReadSubKeyNames(0)
	if err != nil {
		return "", "", false
	}
	for _, name := range names {
		sub, err := registry.OpenKey(root, base+`\`+name, registry.READ|view)
		if err != nil {
			continue
		}
		display, _, _ := sub.GetStringValue("DisplayName")
		uninstall, _, _ := sub.GetStringValue("UninstallString")
		args, _, _ := sub.GetStringValue("QuietUninstallString")
		_ = sub.Close()
		if uninstall == "" || !isLegacyDisplayName(display) {
			continue
		}
		if strings.TrimSpace(args) != "" {
			// QuietUninstallString already carries its switches.
			return args, "", true
		}
		return uninstall, "", true
	}
	return "", "", false
}

func isLegacyDisplayName(name string) bool {
	return strings.Contains(name, "猛犸AI") && !strings.Contains(name, "猛犸智能体")
}
