package main

import "testing"

func TestDesktopTextLooksUpLocaleWithEnglishDefault(t *testing.T) {
	if got := desktopText(LocaleZH, copyStatusReady); got != "猛犸AI 已就绪" {
		t.Fatalf("zh: %s", got)
	}
	if got := desktopText(LocaleEN, copyStatusReady); got != "猛犸AI is ready" {
		t.Fatalf("en: %s", got)
	}
	if got := desktopText(Locale(""), copyStatusReady); got != "猛犸AI is ready" {
		t.Fatalf("unknown locale should fall back to English: %s", got)
	}
	if got := desktopText(LocaleZH, "missing.key"); got != "missing.key" {
		t.Fatalf("unknown key: %s", got)
	}
}

func TestDesktopTextFormatsArgs(t *testing.T) {
	got := desktopText(LocaleEN, copyStatusBackupDatabase, "0.9.32")
	if got != "Desktop update 0.9.32 found. Backing up the database…" {
		t.Fatalf("format: %s", got)
	}
}

// copy.json is the single copy source (see desktop_copy.go). These tests guard
// the JSON side: locale parity and that every key referenced from Go or from
// the shell UI (assets/index.js) actually resolves to a message.

func TestDesktopCopyLocaleKeyParity(t *testing.T) {
	zh := desktopCopy[LocaleZH]
	en := desktopCopy[LocaleEN]
	if zh == nil || en == nil {
		t.Fatal("copy.json must define both zh and en sections")
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Fatalf("zh key %q missing in en", key)
		}
	}
	for key := range en {
		if _, ok := zh[key]; !ok {
			t.Fatalf("en key %q missing in zh", key)
		}
	}
}

func TestDesktopCopyCoversEveryGoKey(t *testing.T) {
	goKeys := []string{
		copyStatusConnecting,
		copyStatusCheckingRuntime,
		copyStatusStartingService,
		copyStatusReady,
		copyStatusUsingRuntime,
		copyStatusBackupDatabase,
		copyStatusInstallingUpdate,
		copyStatusUpdatingRuntime,
		copyStatusFirstExtract,
		copyStatusUpdateFailedKeep,
		copyStatusUsingExisting,
		copyErrorBackupFailed,
		copyErrorUpgradeFailed,
		copyErrorNoFreePort,
		copyErrorCannotBind,
		copyWait1Minute,
		copyWaitNMinutes,
		copyWait1Second,
		copyWaitNSeconds,
		copyHealthNotReady5xx,
		copyHealthNotReadyConnect,
		copyHealthNotReady,
	}
	for _, key := range goKeys {
		for _, locale := range []Locale{LocaleZH, LocaleEN} {
			if msg := desktopCopy[locale][key]; msg == "" {
				t.Fatalf("%s copy.json missing key %q", locale, key)
			}
		}
	}
}

func TestDesktopCopyCoversShellUIKeys(t *testing.T) {
	// assets/index.js reads these keys from the copy table returned by
	// App.GetCopy (applyCopy / statusWait). A missing key would render as
	// "undefined" in the settings window.
	shellKeys := []string{
		"statusWait",
		"loadingHint",
		"title",
		"subtitle",
		"lang",
		"langH",
		"auto",
		"autoH",
		"tray",
		"trayH",
		"sleep",
		"sleepH",
		"showMain",
		"quit",
		"winGroup",
		"winMin",
		"winMax",
		"winClose",
	}
	for _, key := range shellKeys {
		for _, locale := range []Locale{LocaleZH, LocaleEN} {
			if msg := desktopCopy[locale][key]; msg == "" {
				t.Fatalf("%s copy.json missing shell key %q", locale, key)
			}
		}
	}
}
