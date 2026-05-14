package main

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/buildmeta"
	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// customExitError is used in tests to verify exit code handling.
type customExitError struct {
	code int
}

func (e *customExitError) Error() string {
	return "custom exit error"
}

func (e *customExitError) ExitCode() int {
	return e.code
}

// errPlain is a sentinel error used in tests to verify that plain errors
// do not produce a custom exit code.
var errPlain = errors.New("plain error")

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestVersionVariables(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "dev", buildmeta.Version)
	assert.Equal(t, "none", buildmeta.Commit)
	assert.Equal(t, "unknown", buildmeta.Date)
}

func TestRunWithArgsScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "root", want: 0},
		{name: "help", args: []string{"--help"}, want: 0},
		{name: "version", args: []string{"--version"}, want: 0},
		{name: "invalid", args: []string{"invalid-command"}, want: 1},
		{name: "cluster-help", args: []string{"cluster", "--help"}, want: 0},
		{name: "workload-help", args: []string{"workload", "--help"}, want: 0},
		{
			name: "cluster-invalid-subcommand",
			args: []string{"cluster", "invalid-subcommand"},
			want: 1,
		},
		{
			name: "workload-invalid-subcommand",
			args: []string{"workload", "invalid-subcommand"},
			want: 1,
		},
		{name: "cluster-init-help", args: []string{"cluster", "init", "--help"}, want: 0},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, runWithArgs(testCase.args))
		})
	}
}

func TestRunSafelyRecoversFromPanic(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	exitCode := runSafely(nil, func([]string) int {
		panic("test panic")
	}, &output)

	assert.Equal(t, 1, exitCode)

	// Panic output contains stack traces which vary between runs,
	// so we just verify the key message is present
	outputStr := output.String()
	assert.Contains(t, outputStr, "test panic")
	assert.Contains(t, outputStr, "TestRunSafelyRecoversFromPanic")
}

func TestRunSafelyExecutesRunWithArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "success", want: 0},
		{name: "error", args: []string{"invalid-command"}, want: 1},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var errOutput bytes.Buffer

			assert.Equal(t, testCase.want, runSafely(testCase.args, runWithArgs, &errOutput))
		})
	}
}

func TestRunSafelyPropagatesRunnerExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runner func([]string) int
		want   int
	}{
		{
			name:   "success",
			runner: func([]string) int { return 0 },
			want:   0,
		},
		{
			name:   "failure",
			runner: func([]string) int { return 2 },
			want:   2,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			assert.Equal(t, testCase.want, runSafely(nil, testCase.runner, &output))
		})
	}
}

func TestRunWithArgsHandlesCustomExitCode(t *testing.T) {
	t.Parallel()

	// Test that exitCodeFromError correctly extracts custom exit codes from errors
	// implementing ExitCode() int, as used by runWithArgs for DriftExitError etc.
	tests := []struct {
		name       string
		customCode int
	}{
		{name: "drift_exit_code_2", customCode: 2},
		{name: "custom_exit_code_42", customCode: 42},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			code, ok := exitCodeFromError(&customExitError{code: testCase.customCode})

			assert.True(t, ok)
			assert.Equal(t, testCase.customCode, code)
		})
	}
}

func TestExitCodeFromErrorReturnsFalseForPlainErrors(t *testing.T) {
	t.Parallel()

	code, ok := exitCodeFromError(errPlain)

	assert.False(t, ok)
	assert.Equal(t, 0, code)
}
