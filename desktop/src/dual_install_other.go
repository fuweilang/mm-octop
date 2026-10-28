//go:build !darwin

package main

// detectLegacySiblingInstall is macOS-only (bundle id collision). Windows uses
// the NSIS productName 「猛犸智能体」 under the same productIdentifier.
func detectLegacySiblingInstall() string { return "" }
