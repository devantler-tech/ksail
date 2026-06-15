package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runMakeInfoPlist invokes scripts/make-info-plist.sh with the given extra args and returns the
// generated plist content.
func runMakeInfoPlist(t *testing.T, extraArgs ...string) string {
	t.Helper()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available; skipping Info.plist generation test")
	}

	out := filepath.Join(t.TempDir(), "Info.plist")
	args := append([]string{"scripts/make-info-plist.sh", "1.2.3", out}, extraArgs...)

	cmd := exec.Command("bash", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make-info-plist.sh failed: %v\n%s", err, output)
	}

	content, err := os.ReadFile(out) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("reading generated Info.plist: %v", err)
	}

	return string(content)
}

// TestInfoPlistDefaultsMatchDeepLinkConstants guards the Go↔plist deep-link contract: the bundle
// identifier and URL scheme that scripts/make-info-plist.sh bakes into Info.plist by default must
// match the constants the running app uses for single-instance enforcement and ksail:// URL relay
// (deeplink.go). Both real callers (make-macos-app.sh and .goreleaser.desktop.yaml) rely on the
// script defaults, so the defaults ARE the contract; drift fails silently at runtime (deep links
// stop arriving), never at build time — except here.
func TestInfoPlistDefaultsMatchDeepLinkConstants(t *testing.T) {
	t.Parallel()

	plist := runMakeInfoPlist(t)

	assertions := map[string]string{
		"CFBundleIdentifier matches appUniqueID": "<key>CFBundleIdentifier</key><string>" + appUniqueID + "</string>",
		"CFBundleURLName matches appUniqueID":    "<key>CFBundleURLName</key><string>" + appUniqueID + "</string>",
		"CFBundleURLSchemes has deepLinkScheme":  "<string>" + deepLinkScheme + "</string>",
		"CFBundleVersion carries the version":    "<key>CFBundleVersion</key><string>1.2.3</string>",
	}

	for name, want := range assertions {
		if !strings.Contains(plist, want) {
			t.Errorf("%s: generated Info.plist does not contain %q\n%s", name, want, plist)
		}
	}
}

// TestInfoPlistHonorsBundleIDAndSchemeArgs covers the optional bundle-id/url-scheme parameters of
// make-info-plist.sh.
func TestInfoPlistHonorsBundleIDAndSchemeArgs(t *testing.T) {
	t.Parallel()

	plist := runMakeInfoPlist(t, "com.example.custom", "customscheme")

	for _, want := range []string{
		"<key>CFBundleIdentifier</key><string>com.example.custom</string>",
		"<key>CFBundleURLName</key><string>com.example.custom</string>",
		"<string>customscheme</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("generated Info.plist does not contain %q\n%s", want, plist)
		}
	}
}
