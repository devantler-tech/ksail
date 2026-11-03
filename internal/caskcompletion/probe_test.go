package caskcompletion_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/caskcompletion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservationRequiresAnchoredNonceAndActualBuffer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, line, phase, buffer string
		matched                   bool
	}{
		{
			name: "actual ZLE observation", line: "nonce:BUFFER:ksail cluster \r",
			phase: "BUFFER", buffer: "ksail cluster ", matched: true,
		},
		{
			name: "uncompleted prefix stays observable", line: "nonce:BUFFER:ksail cl",
			phase: "BUFFER", buffer: "ksail cl", matched: true,
		},
		{name: "ready", line: "nonce:READY\r", phase: "READY", matched: true},
		{name: "wrong nonce", line: "other:BUFFER:ksail cluster "},
		{name: "echoed widget definition", line: "function capture() { print nonce:BUFFER:ksail cluster; }"},
		{name: "quoted output", line: "echo 'nonce:BUFFER:ksail cluster '"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			phase, buffer, matched := caskcompletion.DecodeLineForTest(testCase.line, "nonce")
			assert.Equal(t, testCase.matched, matched)
			assert.Equal(t, testCase.phase, phase)
			assert.Equal(t, testCase.buffer, buffer)
		})
	}
}

// These fixtures exercise actual native zsh/ZLE, not a published cask install.
func TestNativePTYRequiresLiteralTabAndCompletionMapping(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		t.Skip("the production PTY boundary is macOS /usr/bin/script")
	}

	for _, testCase := range []struct {
		name, input string
		mapping     bool
	}{
		{name: "Tab completes", input: "ksail cl\t", mapping: true},
		{name: "omitting Tab cannot pass", input: "ksail cl", mapping: true},
		{name: "missing mapping cannot pass", input: "ksail cl\t"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			brew := fixtureBrew(t, root, testCase.mapping)

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			result, err := caskcompletion.ProbeForTest(ctx, brew, testCase.input, root)
			if testCase.mapping && strings.Contains(testCase.input, "\t") {
				require.NoError(t, err)
				assert.Equal(t, "ksail cluster ", result.ObservedBuffer)
				assert.True(t, result.TabObserved)
			} else {
				require.ErrorIs(t, err, caskcompletion.ErrTabDidNotComplete)
				assert.Equal(t, "ksail cl", result.ObservedBuffer)
				assert.False(t, result.TabObserved)
			}

			matches, err := filepath.Glob(filepath.Join(root, "ksail-zsh-tab-*"))
			require.NoError(t, err)
			assert.Empty(
				t,
				matches,
				"the child and temporary profile must be cleaned on both outcomes",
			)
		})
	}
}

func TestUnobservedOrEchoedTabCannotPass(t *testing.T) {
	t.Parallel()

	for _, transcript := range []string{
		"", "echo 'nonce:BUFFER:ksail cluster '\n", "other:BUFFER:ksail cluster \n",
		"function capture() { print nonce:BUFFER:ksail cluster; }\n",
	} {
		buffer, err := caskcompletion.ReadBufferForTest(
			t.Context(),
			strings.NewReader(transcript),
			"nonce",
		)
		require.ErrorIs(t, err, caskcompletion.ErrNoObservation)
		assert.Empty(t, buffer)
	}
}

func TestNativePTYSetupFailureIsBoundedAndCleaned(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		t.Skip("the production PTY boundary is macOS /usr/bin/script")
	}

	root := t.TempDir()
	brew := filepath.Join(root, "failing-brew")
	require.NoError(t, os.WriteFile(brew, []byte("#!/bin/bash\nexit 73\n"), 0o600))
	//nolint:gosec // This test-owned fixture must be executable.
	require.NoError(t, os.Chmod(brew, 0o700))

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	result, err := caskcompletion.ProbeForTest(ctx, brew, "ksail cl\t", root)
	require.ErrorIs(
		t,
		err,
		caskcompletion.ErrNoObservation,
		"failed shellenv must not reach the ready widget",
	)
	assert.False(t, result.TabObserved)

	matches, err := filepath.Glob(filepath.Join(root, "ksail-zsh-tab-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "cancellation must reap the terminal before removing its profile")
}

func fixtureBrew(t *testing.T, root string, mapping bool) string {
	t.Helper()

	functions := filepath.Join(root, "functions")
	bin := filepath.Join(root, "bin")

	require.NoError(t, os.MkdirAll(functions, 0o700))
	require.NoError(t, os.MkdirAll(bin, 0o700))

	if mapping {
		require.NoError(t, os.WriteFile(filepath.Join(functions, "_ksail"),
			[]byte("#compdef ksail\n_ksail() { _arguments '1:command:(cluster)' }\n"), 0o600))
	}
	// No accidental invocation can fall through to the maintainer's installed CLI.
	require.NoError(
		t,
		os.WriteFile(filepath.Join(bin, "ksail"), []byte("#!/bin/sh\nexit 99\n"), 0o600),
	)
	//nolint:gosec // This test-owned fixture must be executable.
	require.NoError(t, os.Chmod(filepath.Join(bin, "ksail"), 0o700))
	brew := filepath.Join(bin, "fixture brew")
	script := fmt.Sprintf("#!/bin/bash\n[[ \"$*\" == 'shellenv zsh' ]] || exit 99\n"+
		"printf 'fpath=(%%q /usr/share/zsh/site-functions /usr/share/zsh/5.9/functions)\\n' %s\n"+
		"printf 'export PATH=%%q\\n' %s\n", shellQuote(functions), shellQuote(bin+":/usr/bin:/bin"))
	require.NoError(t, os.WriteFile(brew, []byte(script), 0o600))
	//nolint:gosec // This test-owned fixture must be executable.
	require.NoError(t, os.Chmod(brew, 0o700))

	return brew
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
