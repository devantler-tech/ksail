package flux_test

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// testCase represents a test case for flux commands.
type testCase struct {
	args    []string
	flags   map[string]string
	wantErr bool
	errMsg  string
}

// setupFluxCommand creates and configures a flux command for testing.
func setupFluxCommand(outBuf *bytes.Buffer) *cobra.Command {
	client := setupTestClientWithStreams(outBuf)

	createCmd := client.CreateCreateCommand("")
	createCmd.SetOut(outBuf)
	createCmd.SetErr(&bytes.Buffer{})

	return createCmd
}

// findSourceCommand finds the source command from the create command.
func findSourceCommand(t *testing.T, createCmd *cobra.Command) *cobra.Command {
	t.Helper()

	var sourceCmd *cobra.Command

	for _, subCmd := range createCmd.Commands() {
		if subCmd.Use == sourceCommandName {
			sourceCmd = subCmd

			break
		}
	}

	require.NotNil(t, sourceCmd, "source command not found")

	return sourceCmd
}

// findSubCommand finds a specific subcommand by its Use string.
func findSubCommand(t *testing.T, parentCmd *cobra.Command, use string) *cobra.Command {
	t.Helper()

	var cmd *cobra.Command

	for _, subCmd := range parentCmd.Commands() {
		if subCmd.Use == use {
			cmd = subCmd

			break
		}
	}

	require.NotNil(t, cmd, "subcommand '%s' not found in parent command", use)

	return cmd
}

// testMissingRequiredFlag tests that a command fails when a required flag is missing.
func testMissingRequiredFlag(t *testing.T, cmdPath []string, args []string) {
	t.Helper()
	testCommandError(t, cmdPath, args, "required flag(s)")
}

// testCommandError tests that a command fails with a specific error message.
func testCommandError(t *testing.T, cmdPath []string, args []string, expectedErrMsg string) {
	t.Helper()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	fullArgs := make([]string, 0, len(cmdPath)+len(args))
	fullArgs = append(fullArgs, cmdPath...)
	fullArgs = append(fullArgs, args...)
	createCmd.SetArgs(fullArgs)

	err := createCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), expectedErrMsg)
}

// testCommandSuccess tests that a command executes successfully and produces YAML output.
func testCommandSuccess(t *testing.T, args []string) {
	t.Helper()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	createCmd.SetArgs(args)

	err := createCmd.Execute()
	require.NoError(t, err)

	output := outBuf.String()
	require.NotEmpty(t, output, "output should not be empty")
	require.Contains(t, output, "metadata:")
	require.Contains(t, output, "spec:")
}

// countFlagElements returns the number of elements a flag will add to the command line.
// Boolean flags (value "true") add 1 element (just the flag name).
// Non-boolean flags add 2 elements (flag name + value).
func countFlagElements(flagValue string) int {
	if flagValue == "true" {
		return 1
	}

	return 2
}

// appendFlag appends a flag and its value to the command line slice.
// Boolean flags (value "true") only append the flag name.
// Non-boolean flags append both the flag name and value.
func appendFlag(cmdLine []string, flagKey, flagValue string) []string {
	if flagValue == "true" {
		return append(cmdLine, "--"+flagKey)
	}

	return append(cmdLine, "--"+flagKey, flagValue)
}

// runFluxCommandTest executes a flux command test with the given parameters.
func runFluxCommandTest(t *testing.T, cmdPath []string, testCase testCase) {
	t.Helper()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	// Build command line with accurate capacity calculation
	// Pre-calculate exact capacity by counting flag elements
	flagElems := 0

	var namespaceValue string

	hasNamespace := false

	for flagKey, flagValue := range testCase.flags {
		if flagKey == "namespace" {
			// Namespace is a persistent flag that must be prepended before the subcommand
			namespaceValue = flagValue
			hasNamespace = true
			// Namespace flag always takes 2 elements: flag name (--namespace) + value
			flagElems += 2

			continue
		}

		flagElems += countFlagElements(flagValue)
	}

	cmdLine := make([]string, 0, len(cmdPath)+len(testCase.args)+flagElems)

	// Prepend namespace flag first if present (persistent flag requirement)
	if hasNamespace {
		cmdLine = append(cmdLine, "--namespace", namespaceValue)
	}

	cmdLine = append(cmdLine, cmdPath...)
	cmdLine = append(cmdLine, testCase.args...)

	// Add remaining flags
	for flagKey, flagValue := range testCase.flags {
		if flagKey == "namespace" {
			continue
		}

		cmdLine = appendFlag(cmdLine, flagKey, flagValue)
	}

	createCmd.SetArgs(cmdLine)
	err := createCmd.Execute()

	if testCase.wantErr {
		require.Error(t, err)

		if testCase.errMsg != "" {
			require.Contains(t, err.Error(), testCase.errMsg)
		}

		return
	}

	require.NoError(t, err)

	// Validate YAML output
	output := outBuf.String()
	require.NotEmpty(t, output, "output should not be empty")
	require.Contains(t, output, "metadata:")
	require.Contains(t, output, "spec:")
}
