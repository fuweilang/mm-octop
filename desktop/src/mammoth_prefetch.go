package main

// MAMMOTH-PATCH: quiet dashboard auto-update.
//
// Clicking "检查更新" is the only way an installed client moves forward, so in
// practice customers sit on old builds for months. A full runtime update is too
// heavy to force (220 MB + restart), but the SPA bundle is ~8 MB: prefetch it in
// the background and swap it in at the next launch, when no runtime is holding
// the files and nobody is mid-conversation.
//
// Deliberately no mid-session restart: a chat in progress must never be
// interrupted by an update.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// mammothCheckInterval throttles the background check so a client that is
// restarted repeatedly does not hammer the download host.
const mammothCheckInterval = 8 * time.Hour

type mammothPendingUpdate struct {
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
	ZipPath   string `json:"zip_path"`
	FetchedAt string `json:"fetched_at"`
}

func mammothCacheDir() string {
	return filepath.Join(octopHome(), "cache", "mammoth-update")
}

func pendingUpdatePath() string {
	return filepath.Join(mammothCacheDir(), "pending-dashboard.json")
}

func mammothLastCheckPath() string {
	return filepath.Join(mammothCacheDir(), "last-check.txt")
}

func readPendingUpdate() (mammothPendingUpdate, bool) {
	var pending mammothPendingUpdate
	data, err := os.ReadFile(pendingUpdatePath())
	if err != nil {
		return pending, false
	}
	if err := json.Unmarshal(data, &pending); err != nil {
		return pending, false
	}
	if strings.TrimSpace(pending.Version) == "" || strings.TrimSpace(pending.ZipPath) == "" {
		return pending, false
	}
	return pending, true
}

func writePendingUpdate(pending mammothPendingUpdate) error {
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return os.WriteFile(pendingUpdatePath(), data, 0o644)
}

func clearPendingUpdate() {
	_ = os.Remove(pendingUpdatePath())
}

// mammothCheckDue reports whether the background check may run again. Missing
// or unreadable timestamps count as due.
func mammothCheckDue(now time.Time) bool {
	data, err := os.ReadFile(mammothLastCheckPath())
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	return now.Sub(last) >= mammothCheckInterval
}

func markMammothChecked(now time.Time) error {
	if err := os.MkdirAll(mammothCacheDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(
		mammothLastCheckPath(),
		[]byte(now.UTC().Format(time.RFC3339)+"\n"),
		0o644,
	)
}

// autoUpdateDisabled lets support pin a client to manual updates.
func autoUpdateDisabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("OCTOP_DESKTOP_AUTO_UPDATE"))) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// prefetchMammothUpdate keeps an installed client moving without the user
// clicking anything: a newer runtime is downloaded in the background and
// installed at the next launch, and a newer SPA-only bundle is staged the same
// way. Every failure is logged and swallowed: a flaky network must never affect
// the running client.
func (a *App) prefetchMammothUpdate(locale Locale) {
	if autoUpdateDisabled() {
		return
	}
	now := time.Now()
	if !mammothCheckDue(now) {
		return
	}
	root := portableDir()
	runtimeVersion := portableVersion(root)
	if runtimeVersion == "" || !launchReady(root) {
		return
	}
	doc, err := fetchMammothLatest(nil, mammothLatestURL())
	if err != nil {
		log.Printf("mammoth prefetch: latest.json: %v", err)
		return
	}
	// Runtime first: it is the big one, and staging it now means the next launch
	// is instant instead of a five-minute download in front of the user.
	if entry, err := platformLatestFor(doc, greenPlat()); err == nil {
		if portableNeedsMammothUpdate(runtimeVersion, entry.Portable) {
			if err := stageRuntimeUpdate(runtimeVersion, entry.Portable, now); err != nil {
				log.Printf("mammoth prefetch: runtime: %v", err)
			} else {
				_ = markMammothChecked(now)
				return
			}
		}
	}
	if codePatchNeedsUpdate(doc.Code, readCodePatchStamp(), runtimeVersion) {
		if err := stageCodePatch(now, doc.Code, runtimeVersion); err != nil {
			log.Printf("mammoth prefetch: code: %v", err)
		} else {
			_ = markMammothChecked(now)
			return
		}
	}
	if !dashboardNeedsMammothUpdate(doc.Dashboard, readDashboardStamp(), runtimeVersion) {
		_ = markMammothChecked(now)
		return
	}
	artifact := doc.Dashboard
	zipPath := filepath.Join(mammothCacheDir(), "dashboard-"+safeVersion(artifact.Version)+".zip")
	sum, err := downloadFileSHA256(nil, artifact.URL, zipPath, nil)
	if err != nil {
		log.Printf("mammoth prefetch: download %s: %v", artifact.Version, err)
		return
	}
	if !sha256HexEqual(sum, artifact.SHA256) {
		_ = os.Remove(zipPath)
		log.Printf("mammoth prefetch: sha256 mismatch for %s", artifact.Version)
		return
	}
	pending := mammothPendingUpdate{
		Version:   artifact.Version,
		SHA256:    artifact.SHA256,
		URL:       artifact.URL,
		ZipPath:   zipPath,
		FetchedAt: now.UTC().Format(time.RFC3339),
	}
	if err := writePendingUpdate(pending); err != nil {
		log.Printf("mammoth prefetch: write pending: %v", err)
		return
	}
	_ = markMammothChecked(now)
	log.Printf("mammoth prefetch: staged dashboard %s for the next launch", artifact.Version)
}

// applyPendingDashboardUpdate swaps a staged SPA in before the runtime starts.
// Returns true when the staged bundle was applied. Any problem leaves the
// current dashboard untouched — a background update must never block boot.
func applyPendingDashboardUpdate(root string) bool {
	pending, ok := readPendingUpdate()
	if !ok {
		return false
	}
	defer clearPendingUpdate()
	if !launchReady(root) {
		return false
	}
	if !dashboardNeedsMammothUpdate(&mammothArtifact{
		Version: pending.Version,
		URL:     pending.URL,
		SHA256:  pending.SHA256,
	}, readDashboardStamp(), portableVersion(root)) {
		_ = os.Remove(pending.ZipPath)
		return false
	}
	if _, err := os.Stat(pending.ZipPath); err != nil {
		log.Printf("mammoth prefetch: staged zip missing: %v", err)
		return false
	}
	previous, err := replaceDashboardFromZip(root, pending.ZipPath)
	if err != nil {
		log.Printf("mammoth prefetch: apply %s: %v", pending.Version, err)
		return false
	}
	if err := writeDashboardStamp(dashboardStamp{
		Version: pending.Version,
		SHA256:  pending.SHA256,
	}); err != nil {
		log.Printf("mammoth prefetch: stamp: %v", err)
	}
	// The displaced bundle is kept until the next successful launch; deleting it
	// here would leave nothing to roll back to if the new SPA does not load.
	log.Printf("mammoth prefetch: applied dashboard %s (previous kept at %s)", pending.Version, previous)
	return true
}

// discardStalePendingUpdate drops a staged bundle that no longer matches the
// installed runtime, e.g. after a manual in-app update installed something newer.
func discardStalePendingUpdate(root string) {
	pending, ok := readPendingUpdate()
	if !ok {
		return
	}
	if !dashboardNeedsMammothUpdate(&mammothArtifact{
		Version: pending.Version,
		URL:     pending.URL,
		SHA256:  pending.SHA256,
	}, readDashboardStamp(), portableVersion(root)) {
		_ = os.Remove(pending.ZipPath)
		clearPendingUpdate()
	}
}
