//go:build darwin

package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	envProbeTimeout = 5 * time.Second
	envStartMarker  = "__KSAIL_ENV_START__"
	envEndMarker    = "__KSAIL_ENV_END__"
)

// hydrateLoginShellEnv imports environment variables from the user's interactive login shell when
// the app is launched from the macOS GUI (Finder, Dock, Spotlight, or `open -a KSail`, which is how
// the `ksail open desktop` command starts it). Such launches go through LaunchServices/launchd, which do
// not source the user's shell profile, so variables exported there — HCLOUD_TOKEN,
// OMNI_SERVICE_ACCOUNT_KEY, KUBECONFIG, PATH additions, … — are missing. The cluster providers that
// depend on them then silently report nothing (e.g. Hetzner clusters never appear), even though
// `ksail open web` from a terminal works because the shell already exported them.
//
// Variables already present are never overwritten (PATH is merged), so it is effectively a no-op
// when the environment is already populated. It is skipped entirely unless the process was launched
// from a bundle, so running the raw binary from a terminal pays no startup cost.
func hydrateLoginShellEnv() {
	if !launchedFromBundle() {
		return
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), envProbeTimeout)
	defer cancel()

	// -i sources ~/.zshrc / ~/.bashrc and -l sources ~/.zprofile / ~/.profile, the two places users
	// export credentials. The markers fence the env dump so any shell startup banner is ignored.
	script := "echo " + envStartMarker + "; /usr/bin/env; echo " + envEndMarker

	var stdout bytes.Buffer

	//nolint:gosec // Runs the user's own login shell to import its environment; same trust as a terminal.
	cmd := exec.CommandContext(ctx, shell, "-ilc", script)
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return // Best effort: a missing token surfaces as "no clusters", never a crash.
	}

	mergeShellEnv(parseEnvDump(stdout.String()))
}

// launchedFromBundle reports whether the process was started from a macOS application bundle.
// LaunchServices sets __CFBundleIdentifier in the environment for bundle launches; it is absent when
// the binary is run directly from a shell, which already inherits the shell environment.
func launchedFromBundle() bool {
	return os.Getenv("__CFBundleIdentifier") != ""
}

// parseEnvDump extracts KEY=VALUE pairs printed by `env` between the markers.
func parseEnvDump(out string) map[string]string {
	env := make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(out))

	inBlock := false

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == envStartMarker:
			inBlock = true
		case line == envEndMarker:
			inBlock = false
		case inBlock:
			key, value, found := strings.Cut(line, "=")
			if found && isEnvName(key) {
				env[key] = value
			}
		}
	}

	return env
}

// isEnvName reports whether s is a plausible environment variable name. It guards against multi-line
// values (such as exported shell functions) being mistaken for new variables.
func isEnvName(s string) bool {
	if s == "" {
		return false
	}

	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}

	return true
}

// mergeShellEnv applies shell-sourced variables to the process without clobbering any already set.
// PATH is merged so GUI launches can still find CLIs (docker, kubectl, …) on the user's shell PATH.
func mergeShellEnv(shellEnv map[string]string) {
	for key, value := range shellEnv {
		if key == "PATH" {
			_ = os.Setenv("PATH", mergePath(os.Getenv("PATH"), value))

			continue
		}

		_, present := os.LookupEnv(key)
		if !present {
			_ = os.Setenv(key, value)
		}
	}
}

// mergePath appends shell PATH entries missing from the current PATH, keeping current precedence.
func mergePath(current, shell string) string {
	if current == "" {
		return shell
	}

	seen := make(map[string]bool)
	for dir := range strings.SplitSeq(current, ":") {
		seen[dir] = true
	}

	merged := []string{current}

	for dir := range strings.SplitSeq(shell, ":") {
		if dir != "" && !seen[dir] {
			merged = append(merged, dir)
			seen[dir] = true
		}
	}

	return strings.Join(merged, ":")
}
