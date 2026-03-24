package cluster_test

import (
	"fmt"
	"io"
	"path/filepath"
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestoreErrorConstants verifies that all sentinel error variables
// defined in restore.go are non-nil and have meaningful messages.
func TestRestoreErrorConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
		wantMsg  string
	}{
		{
			name:     "ErrInvalidResourcePolicy is defined",
			sentinel: clusterpkg.ErrInvalidResourcePolicy,
			wantMsg:  "invalid existing-resource-policy",
		},
		{
			name:     "ErrRestoreFailed is defined",
			sentinel: clusterpkg.ErrRestoreFailed,
			wantMsg:  "resource restore failed",
		},
		{
			name:     "ErrInvalidTarPath is defined",
			sentinel: clusterpkg.ErrInvalidTarPath,
			wantMsg:  "invalid tar entry path",
		},
		{
			name:     "ErrSymlinkInArchive is defined",
			sentinel: clusterpkg.ErrSymlinkInArchive,
			wantMsg:  "symbolic and hard links are not supported",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, testCase.sentinel)
			assert.Contains(t, testCase.sentinel.Error(), testCase.wantMsg)
		})
	}
}

// TestRestoreErrors_AreDistinct verifies that all restore error sentinels
// are distinct from one another so errors.Is behaves correctly.
func TestRestoreErrors_AreDistinct(t *testing.T) {
	t.Parallel()

	allErrors := []error{
		clusterpkg.ErrInvalidResourcePolicy,
		clusterpkg.ErrRestoreFailed,
		clusterpkg.ErrInvalidTarPath,
		clusterpkg.ErrSymlinkInArchive,
	}

	for index := range allErrors {
		for innerIndex := index + 1; innerIndex < len(allErrors); innerIndex++ {
			assert.NotErrorIs(
				t,
				allErrors[index], allErrors[innerIndex],
				"errors at index %d and %d should be distinct",
				index, innerIndex,
			)
		}
	}
}

// TestRestoreErrors_CanBeWrapped verifies that sentinel errors can be wrapped
// with fmt.Errorf and still be detected via errors.Is.
func TestRestoreErrors_CanBeWrapped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
	}{
		{
			name:     "ErrInvalidResourcePolicy can be wrapped",
			sentinel: clusterpkg.ErrInvalidResourcePolicy,
		},
		{
			name:     "ErrRestoreFailed can be wrapped",
			sentinel: clusterpkg.ErrRestoreFailed,
		},
		{
			name:     "ErrInvalidTarPath can be wrapped",
			sentinel: clusterpkg.ErrInvalidTarPath,
		},
		{
			name:     "ErrSymlinkInArchive can be wrapped",
			sentinel: clusterpkg.ErrSymlinkInArchive,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("context: %w", testCase.sentinel)
			assert.ErrorIs(t, wrapped, testCase.sentinel)
		})
	}
}

// TestNewRestoreCmd_FlagsExistWithCorrectDefaults verifies that NewRestoreCmd
// registers all expected flags with the correct default values.
func TestNewRestoreCmd_FlagsExistWithCorrectDefaults(t *testing.T) {
	t.Parallel()

	flagTests := []struct {
		flagName     string
		shorthand    string
		defaultValue string
	}{
		{
			flagName:     "input",
			shorthand:    "i",
			defaultValue: "",
		},
		{
			flagName:     "existing-resource-policy",
			shorthand:    "",
			defaultValue: "none",
		},
		{
			flagName:     "dry-run",
			shorthand:    "",
			defaultValue: "false",
		},
	}

	for _, flagTest := range flagTests {
		t.Run(flagTest.flagName, func(t *testing.T) {
			t.Parallel()

			restoreCmd := clusterpkg.NewRestoreCmd(nil)
			require.NotNil(t, restoreCmd)

			flag := restoreCmd.Flags().Lookup(flagTest.flagName)
			require.NotNil(t, flag, "flag %q should be registered", flagTest.flagName)
			assert.Equal(t, flagTest.defaultValue, flag.DefValue,
				"flag %q should have default value %q", flagTest.flagName, flagTest.defaultValue)

			if flagTest.shorthand != "" {
				assert.Equal(t, flagTest.shorthand, flag.Shorthand,
					"flag %q should have shorthand %q", flagTest.flagName, flagTest.shorthand)
			}
		})
	}
}

// TestNewRestoreCmd_InputFlagIsRequired verifies that --input is marked required.
func TestNewRestoreCmd_InputFlagIsRequired(t *testing.T) {
	t.Parallel()

	restoreCmd := clusterpkg.NewRestoreCmd(nil)
	require.NotNil(t, restoreCmd)

	restoreCmd.SetOut(io.Discard)
	restoreCmd.SetErr(io.Discard)
	restoreCmd.SetArgs([]string{"--existing-resource-policy", "none"})

	err := restoreCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "input")
}

// TestRestoreCmd_InvalidResourcePolicy verifies that an invalid
// existing-resource-policy value returns ErrInvalidResourcePolicy.
// The policy validation in runRestore happens before kubeconfig and
// file access, so we do not need a real cluster or archive for this test.
func TestRestoreCmd_InvalidResourcePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
	}{
		{name: "unknown policy value", policy: "unknown"},
		{name: "capitalised none", policy: "None"},
		{name: "capitalised update", policy: "Update"},
		{name: "unsupported policy value 'skip'", policy: "skip"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			restoreCmd := clusterpkg.NewRestoreCmd(nil)
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", "dummy.tar.gz",
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, clusterpkg.ErrInvalidResourcePolicy,
				"expected ErrInvalidResourcePolicy, got: %v", err,
			)
		})
	}
}

// TestRestoreCmd_ValidPoliciesPassValidation verifies that "none" and "update"
// are accepted as valid policy values. The command will fail later when trying
// to open the nonexistent --input archive, NOT at the policy check.
func TestRestoreCmd_ValidPoliciesPassValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
	}{
		{name: "none policy", policy: "none"},
		{name: "update policy", policy: "update"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			nonexistentArchive := filepath.Join(t.TempDir(), "nonexistent.tar.gz")

			restoreCmd := clusterpkg.NewRestoreCmd(nil)
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", nonexistentArchive,
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(
				t, err,
				"expected a later error (archive not found), not ErrInvalidResourcePolicy",
			)
			assert.NotErrorIs(t, err, clusterpkg.ErrInvalidResourcePolicy,
				"valid policy %q should not return ErrInvalidResourcePolicy", testCase.policy,
			)
		})
	}
}

// TestRestoreCmd_Metadata verifies basic command metadata.
func TestRestoreCmd_Metadata(t *testing.T) {
	t.Parallel()

	restoreCmd := clusterpkg.NewRestoreCmd(nil)
	require.NotNil(t, restoreCmd)

	assert.Equal(t, "restore", restoreCmd.Use)
	assert.NotEmpty(t, restoreCmd.Short)
	assert.NotEmpty(t, restoreCmd.Long)
	assert.True(t, restoreCmd.SilenceUsage)
}

// TestDeriveBackupName_ExtensionStripping verifies the extension stripping
// logic for .tar.gz and .tgz archives.
func TestDeriveBackupName_ExtensionStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tar.gz with path",
			input:    "/backups/cluster-backup.tar.gz",
			expected: "cluster-backup",
		},
		{
			name:     "tgz with path",
			input:    "/backups/cluster-backup.tgz",
			expected: "cluster-backup",
		},
		{
			name:     "simple filename",
			input:    "my-backup.tar.gz",
			expected: "my-backup",
		},
		{
			name:     "no extension",
			input:    "my-backup",
			expected: "my-backup",
		},
		{
			name:     "other extension preserved",
			input:    "my-backup.zip",
			expected: "my-backup.zip",
		},
		{
			name:     "timestamped name",
			input:    "/mnt/ksail-backup-2026-03-21T10:00:00Z.tar.gz",
			expected: "ksail-backup-2026-03-21T10:00:00Z",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clusterpkg.ExportDeriveBackupName(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestAllLinesContain_EdgeCases tests additional edge cases for the
// allLinesContain helper used in restore's "already exists" detection.
func TestAllLinesContain_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		substr   string
		expected bool
	}{
		{
			name:     "single matching non-empty line",
			output:   "already exists",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "line with surrounding whitespace",
			output:   "  already exists  \n",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "mixed matching and non-matching",
			output:   "already exists\nother error",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "completely empty output",
			output:   "",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "all empty lines",
			output:   "\n\n\n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "multiple all-matching lines",
			output:   "error: resource already exists\nerror: configmap already exists\nerror: secret already exists",
			substr:   "already exists",
			expected: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clusterpkg.ExportAllLinesContain(testCase.output, testCase.substr)
			assert.Equal(t, testCase.expected, result)
		})
	}
}
