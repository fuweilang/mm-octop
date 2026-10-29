package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

const (
	copyStatusConnecting       = "status.connecting"
	copyStatusCheckingRuntime  = "status.checking_runtime"
	copyStatusStartingService  = "status.starting_service"
	copyStatusReady            = "status.ready"
	copyStatusUsingRuntime     = "status.using_runtime"
	copyStatusBackupDatabase   = "status.backup_database"
	copyStatusInstallingUpdate = "status.installing_update"
	copyStatusUpdatingRuntime  = "status.updating_runtime"
	copyStatusFirstExtract     = "status.first_extract"
	copyStatusUpdateFailedKeep = "status.update_failed_keep"
	copyStatusUsingExisting    = "status.using_existing"
	copyStatusReplacingStale   = "status.replacing_stale"
	// MAMMOTH-PATCH: Phase 1 Mammoth-channel portable update status strings.
	copyStatusCheckingMammothUpdate = "status.checking_mammoth_update"
	copyStatusMammothUpToDate       = "status.mammoth_up_to_date"
	copyStatusMammothDownloading    = "status.mammoth_downloading"
	copyStatusMammothVerifying      = "status.mammoth_verifying"
	copyStatusMammothInstalling     = "status.mammoth_installing"
	copyStatusMammothRestarting     = "status.mammoth_restarting"
	copyStatusMammothShellNewer     = "status.mammoth_shell_newer"
	copyErrorMammothUpdateFailed    = "error.mammoth_update_failed"
	copyErrorMammothSHAMismatch     = "error.mammoth_sha_mismatch"
	copyErrorBackupFailed           = "error.backup_failed"
	copyErrorUpgradeFailed          = "error.upgrade_failed"
	copyErrorRuntimeStale           = "error.runtime_stale"
	copyErrorNoFreePort             = "error.no_free_port"
	copyErrorCannotBind             = "error.cannot_bind"
	copyErrorPortOccupied           = "error.port_occupied"
	copyWait1Minute                 = "wait.1_minute"
	copyWaitNMinutes                = "wait.n_minutes"
	copyWait1Second                 = "wait.1_second"
	copyWaitNSeconds                = "wait.n_seconds"
	copyHealthNotReady5xx           = "health.not_ready_5xx"
	copyHealthNotReadyConnect       = "health.not_ready_connect"
	copyHealthNotReady              = "health.not_ready"
	copyWarnLegacyShell             = "warn.legacy_shell"
	copyWarnLegacyAutostart         = "warn.legacy_autostart"
	copyLegacyManual                = "error.legacy_manual"
	copyLegacySelf                  = "error.legacy_self"
)

// assets/copy.json is the single source for desktop shell copy: this Go side
// (dotted keys above — status/error text emitted over the "desktop:status"
// event) and the shell UI in assets/index.js (camelCase keys — settings/loading
// labels), which pulls the same table through App.GetCopy. Keep the zh and en
// key sets identical; tests in desktop_copy_test.go guard the parity and that
// every key referenced in Go exists.
//
//go:embed assets/copy.json
var copyJSON []byte

var desktopCopy = mustLoadDesktopCopy()

func mustLoadDesktopCopy() map[Locale]map[string]string {
	var table map[Locale]map[string]string
	if err := json.Unmarshal(copyJSON, &table); err != nil {
		panic(fmt.Sprintf("parse assets/copy.json: %v", err))
	}
	if table[LocaleEN] == nil || table[LocaleZH] == nil {
		panic("assets/copy.json must define both zh and en sections")
	}
	return table
}

// GetCopy hands the full copy table to the shell UI (see assets/index.js).
func (a *App) GetCopy() map[Locale]map[string]string {
	return desktopCopy
}

func desktopText(locale Locale, key string, args ...any) string {
	msg := lookupDesktopCopy(locale, key)
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

func lookupDesktopCopy(locale Locale, key string) string {
	if msgs := desktopCopy[locale]; msgs != nil {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	if msg, ok := desktopCopy[LocaleEN][key]; ok {
		return msg
	}
	return key
}
