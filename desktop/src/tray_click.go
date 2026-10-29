package main

// trayLeftClickShowsSettings reports whether a primary click on the system
// tray / menu-bar extra should open the settings popover.
//
// On macOS the icon lives in the menu bar; a left click is the normal way to
// open that popover. Windows/Linux keep left-click for showing the main
// window and use right-click for settings.
func trayLeftClickShowsSettings(goos string) bool {
	return goos == "darwin"
}

// trayNeedsQuitMenu reports whether the tray must carry an explicit menu with
// a quit item. Windows has no keyboard/Dock equivalent of ⌘Q, and closing the
// window used to hide the app, so users had no discoverable way to exit.
func trayNeedsQuitMenu(goos string) bool {
	return goos == "windows"
}
