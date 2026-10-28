//go:build !darwin && !windows

package main

// The legacy 「猛犸AI」 desktop build shipped for macOS and Windows only.
// Nothing to detect, nothing to remove.

func canTrashLegacyApp(string) bool { return false }

func trashLegacyApp(string) (string, error) { return copyLegacyManual, nil }
