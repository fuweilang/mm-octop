//go:build darwin

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const mammothBundleID = "com.tencent.octop"

// detectLegacySiblingInstall looks for another .app with the same bundle id
// (legacy 「猛犸AI」 vs 「猛犸智能体」). Logs + returns a one-line warning; does
// not delete anything. Installers should overwrite the same productIdentifier.
func detectLegacySiblingInstall() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	selfApp := bundleRoot(self)
	found := findBundleIDApps(mammothBundleID)
	var others []string
	for _, app := range found {
		if selfApp != "" && samePath(app, selfApp) {
			continue
		}
		base := filepath.Base(app)
		// Warn when the other install still uses the old display name, or any
		// second copy of the same bundle id is present.
		if strings.Contains(base, "猛犸AI") || !strings.Contains(base, "猛犸智能体") || len(found) > 1 {
			others = append(others, app)
		}
	}
	if len(others) == 0 {
		return ""
	}
	log.Printf("desktop: legacy/sibling install(s) with %s: %s", mammothBundleID, strings.Join(others, ", "))
	return others[0]
}

func bundleRoot(exe string) string {
	// .../Foo.app/Contents/MacOS/Foo → .../Foo.app
	dir := filepath.Clean(exe)
	for i := 0; i < 4; i++ {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}

func findBundleIDApps(bundleID string) []string {
	// mdfind is the practical way to locate same-id apps without walking all volumes.
	out, err := exec.Command("mdfind", "kMDItemCFBundleIdentifier == '"+bundleID+"'").Output()
	if err != nil {
		return scanCommonAppDirs(bundleID)
	}
	var apps []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".app") {
			continue
		}
		apps = append(apps, line)
	}
	if len(apps) == 0 {
		return scanCommonAppDirs(bundleID)
	}
	return apps
}

func scanCommonAppDirs(bundleID string) []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		"/Applications",
		filepath.Join(home, "Applications"),
	}
	var apps []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if !ent.IsDir() || !strings.HasSuffix(ent.Name(), ".app") {
				continue
			}
			app := filepath.Join(dir, ent.Name())
			if readBundleID(app) == bundleID {
				apps = append(apps, app)
			}
		}
	}
	return apps
}

func readBundleID(app string) string {
	data, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	// Minimal parse: avoid pulling in a plist dependency for a one-line warning.
	text := string(data)
	key := "<key>CFBundleIdentifier</key>"
	idx := strings.Index(text, key)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(key):]
	start := strings.Index(rest, "<string>")
	end := strings.Index(rest, "</string>")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(rest[start+len("<string>"):end])
}
