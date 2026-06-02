//go:build darwin

package main

import (
	"os"
	"testing"
)

func TestParseEnvDump(t *testing.T) {
	t.Parallel()

	out := "noise before the block\n" +
		envStartMarker + "\n" +
		"HCLOUD_TOKEN=secret-value\n" +
		"PATH=/usr/local/bin:/usr/bin\n" +
		"EMPTY=\n" +
		"not an env line\n" +
		"123BAD=skip\n" +
		envEndMarker + "\n" +
		"AFTER=ignored\n"

	got := parseEnvDump(out)

	if got["HCLOUD_TOKEN"] != "secret-value" {
		t.Errorf("HCLOUD_TOKEN = %q, want %q", got["HCLOUD_TOKEN"], "secret-value")
	}

	if got["PATH"] != "/usr/local/bin:/usr/bin" {
		t.Errorf("PATH = %q, want %q", got["PATH"], "/usr/local/bin:/usr/bin")
	}

	value, ok := got["EMPTY"]
	if !ok || value != "" {
		t.Errorf("EMPTY = %q (present=%v), want \"\" present=true", value, ok)
	}

	if _, ok := got["AFTER"]; ok {
		t.Error("AFTER is outside the marker block and must be ignored")
	}

	if _, ok := got["123BAD"]; ok {
		t.Error("123BAD is not a valid env name and must be skipped")
	}
}

func TestIsEnvName(t *testing.T) {
	t.Parallel()

	valid := []string{"PATH", "HCLOUD_TOKEN", "_X", "A1", "aB_3"}
	for _, name := range valid {
		if !isEnvName(name) {
			t.Errorf("isEnvName(%q) = false, want true", name)
		}
	}

	invalid := []string{"", "1A", "A-B", "A B", "A.B"}
	for _, name := range invalid {
		if isEnvName(name) {
			t.Errorf("isEnvName(%q) = true, want false", name)
		}
	}
}

func TestMergePath(t *testing.T) {
	t.Parallel()

	got := mergePath("/usr/bin:/bin", "/opt/homebrew/bin:/usr/bin:/usr/local/bin")
	want := "/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin"

	if got != want {
		t.Errorf("mergePath = %q, want %q", got, want)
	}

	if got := mergePath("", "/opt/bin"); got != "/opt/bin" {
		t.Errorf("mergePath with empty current = %q, want %q", got, "/opt/bin")
	}
}

func TestMergeShellEnvDoesNotOverwriteExisting(t *testing.T) {
	t.Setenv("KSAIL_TEST_EXISTING", "original")

	mergeShellEnv(map[string]string{
		"KSAIL_TEST_EXISTING": "from-shell",
		"KSAIL_TEST_NEW":      "added",
	})

	if got := os.Getenv("KSAIL_TEST_EXISTING"); got != "original" {
		t.Errorf("existing var was overwritten: got %q, want %q", got, "original")
	}

	if got := os.Getenv("KSAIL_TEST_NEW"); got != "added" {
		t.Errorf("new var was not added: got %q, want %q", got, "added")
	}
}
