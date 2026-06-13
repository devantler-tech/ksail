package cluster_test

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The restore engine (archive extraction, security guards, metadata parsing,
// error classification) lives in pkg/svc/backup and is tested there. These
// tests cover the cmd-package sentinels, command wiring, and the cobra-wrapper
// print helpers that remain in the cmd package.

// TestRestoreErrorConstants verifies that all sentinel error variables
// re-exported by restore.go are non-nil and have meaningful messages.
func TestRestoreErrorConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
		wantMsg  string
	}{
		{
			name:     "ErrInvalidResourcePolicy is defined",
			sentinel: cluster.ErrInvalidResourcePolicy,
			wantMsg:  "invalid existing-resource-policy",
		},
		{
			name:     "ErrRestoreFailed is defined",
			sentinel: cluster.ErrRestoreFailed,
			wantMsg:  "resource restore failed",
		},
		{
			name:     "ErrInvalidTarPath is defined",
			sentinel: cluster.ErrInvalidTarPath,
			wantMsg:  "invalid tar entry path",
		},
		{
			name:     "ErrSymlinkInArchive is defined",
			sentinel: cluster.ErrSymlinkInArchive,
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
		cluster.ErrInvalidResourcePolicy,
		cluster.ErrRestoreFailed,
		cluster.ErrInvalidTarPath,
		cluster.ErrSymlinkInArchive,
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
			sentinel: cluster.ErrInvalidResourcePolicy,
		},
		{
			name:     "ErrRestoreFailed can be wrapped",
			sentinel: cluster.ErrRestoreFailed,
		},
		{
			name:     "ErrInvalidTarPath can be wrapped",
			sentinel: cluster.ErrInvalidTarPath,
		},
		{
			name:     "ErrSymlinkInArchive can be wrapped",
			sentinel: cluster.ErrSymlinkInArchive,
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

			restoreCmd := cluster.NewRestoreCmd()
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

	restoreCmd := cluster.NewRestoreCmd()
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

			restoreCmd := cluster.NewRestoreCmd()
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", "dummy.tar.gz",
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, cluster.ErrInvalidResourcePolicy,
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

			restoreCmd := cluster.NewRestoreCmd()
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
			assert.NotErrorIs(t, err, cluster.ErrInvalidResourcePolicy,
				"valid policy %q should not return ErrInvalidResourcePolicy", testCase.policy,
			)
		})
	}
}

// TestRestoreCmd_Metadata verifies basic command metadata.
func TestRestoreCmd_Metadata(t *testing.T) {
	t.Parallel()

	restoreCmd := cluster.NewRestoreCmd()
	require.NotNil(t, restoreCmd)

	assert.Equal(t, "restore", restoreCmd.Use)
	assert.NotEmpty(t, restoreCmd.Short)
	assert.NotEmpty(t, restoreCmd.Long)
	assert.True(t, restoreCmd.SilenceUsage)
}

// TestPrintRestoreHeader verifies that printRestoreHeader writes the expected
// lines including the input path, policy, and (when dry-run) the dry-run note.
func TestPrintRestoreHeader( //nolint:funlen // Table-driven test with multiple comprehensive cases
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name      string
		inputPath string
		policy    string
		dryRun    bool
		wantLines []string
		noLines   []string
	}{
		{
			name:      "standard header without dry-run",
			inputPath: "/backups/cluster.tar.gz",
			policy:    "none",
			dryRun:    false,
			wantLines: []string{
				"Starting cluster restore",
				"/backups/cluster.tar.gz",
				"none",
				"Extracting backup archive",
			},
			noLines: []string{"dry-run"},
		},
		{
			name:      "header with dry-run enabled",
			inputPath: "/backups/cluster.tar.gz",
			policy:    "update",
			dryRun:    true,
			wantLines: []string{
				"Starting cluster restore",
				"/backups/cluster.tar.gz",
				"update",
				"dry-run",
			},
		},
		{
			name:      "header with update policy",
			inputPath: "relative/path/backup.tar.gz",
			policy:    "update",
			dryRun:    false,
			wantLines: []string{
				"update",
				"relative/path/backup.tar.gz",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cluster.ExportPrintRestoreHeader(
				&buf, testCase.inputPath, testCase.policy, testCase.dryRun,
			)
			output := buf.String()

			for _, want := range testCase.wantLines {
				assert.Contains(t, output, want,
					"output should contain %q", want)
			}

			for _, noWant := range testCase.noLines {
				assert.NotContains(t, output, noWant,
					"output should not contain %q", noWant)
			}
		})
	}
}

// TestPrintRestoreMetadata verifies that printRestoreMetadata correctly outputs
// all metadata fields, including optional Distribution and Provider.
func TestPrintRestoreMetadata( //nolint:funlen // Table-driven test with multiple comprehensive cases
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name      string
		metadata  *cluster.BackupMetadata
		wantLines []string
		noLines   []string
	}{
		{
			name: "full metadata with distribution and provider",
			metadata: &cluster.BackupMetadata{
				Version:       "v1",
				Timestamp:     time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
				ClusterName:   "my-cluster",
				Distribution:  "Vanilla",
				Provider:      "Docker",
				ResourceCount: 42,
			},
			wantLines: []string{
				"v1",
				"2026-03-15",
				"my-cluster",
				"Vanilla",
				"Docker",
				"42",
			},
		},
		{
			name: "metadata without optional distribution and provider",
			metadata: &cluster.BackupMetadata{
				Version:       "v1",
				Timestamp:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				ClusterName:   "bare-cluster",
				ResourceCount: 5,
			},
			wantLines: []string{
				"v1",
				"bare-cluster",
				"5",
			},
			noLines: []string{"Distribution:", "Provider:"},
		},
		{
			name: "zero resource count is printed",
			metadata: &cluster.BackupMetadata{
				Version:     "v1",
				ClusterName: "empty-cluster",
			},
			wantLines: []string{"Resources: 0"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cluster.ExportPrintRestoreMetadata(&buf, testCase.metadata)
			output := buf.String()

			for _, want := range testCase.wantLines {
				assert.Contains(t, output, want,
					"output should contain %q", want)
			}

			for _, noWant := range testCase.noLines {
				assert.NotContains(t, output, noWant,
					"output should not contain %q", noWant)
			}
		})
	}
}
