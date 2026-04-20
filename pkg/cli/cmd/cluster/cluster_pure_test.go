package cluster_test

import (
	"archive/tar"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errClusterPureResourceAlreadyExists = errors.New("resource already exists")
	errClusterPureGeneric               = errors.New("some error")
)

// ===========================================================================
// formatDiffTable — comprehensive table rendering tests
// ===========================================================================

func TestFormatDiffTable_EmptyDiff(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{}
	got := cluster.ExportFormatDiffTable(diff, 0)
	assert.Contains(t, got, "Component")
	assert.Contains(t, got, "Before")
	assert.Contains(t, got, "After")
	assert.Contains(t, got, "Impact")
}

func TestFormatDiffTable_InPlaceOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "replicas",
				OldValue: "1",
				NewValue: "3",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "replicas")
	assert.Contains(t, got, "1")
	assert.Contains(t, got, "3")
	assert.Contains(t, got, "🟢")
}

func TestFormatDiffTable_RebootOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "kernel",
				OldValue: "5.4",
				NewValue: "5.15",
				Category: clusterupdate.ChangeCategoryRebootRequired,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "kernel")
	assert.Contains(t, got, "🟡")
}

func TestFormatDiffTable_RecreateOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "distribution",
				OldValue: "k3s",
				NewValue: "talos",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "distribution")
	assert.Contains(t, got, "🔴")
}

func TestFormatDiffTable_MixedSeverities(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "dist",
				OldValue: "a",
				NewValue: "b",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		},
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "kern",
				OldValue: "c",
				NewValue: "d",
				Category: clusterupdate.ChangeCategoryRebootRequired,
			},
		},
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "reps",
				OldValue: "e",
				NewValue: "f",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 3)

	// Verify all fields present
	assert.Contains(t, got, "dist")
	assert.Contains(t, got, "kern")
	assert.Contains(t, got, "reps")

	// Verify all icons present
	assert.Contains(t, got, "🔴")
	assert.Contains(t, got, "🟡")
	assert.Contains(t, got, "🟢")

	// Verify header and separator are present
	assert.Contains(t, got, "─")
}

func TestFormatDiffTable_LongFieldValues(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "spec.cluster.metricsServer.config.scrapeInterval",
				OldValue: "a-very-long-before-value-that-tests-column-width",
				NewValue: "another-very-long-after-value-for-testing",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "spec.cluster.metricsServer.config.scrapeInterval")
	assert.Contains(t, got, "a-very-long-before-value-that-tests-column-width")
}

func TestFormatDiffTable_MultipleRows(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "a",
				OldValue: "1",
				NewValue: "2",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
			{
				Field:    "b",
				OldValue: "3",
				NewValue: "4",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
			{
				Field:    "c",
				OldValue: "5",
				NewValue: "6",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 3)
	assert.Contains(t, got, "a")
	assert.Contains(t, got, "b")
	assert.Contains(t, got, "c")
}

// ===========================================================================
// stripDistributionPrefix — context name prefix stripping
// ===========================================================================

func TestStripDistributionPrefix_UnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cluster.ExportStripDistributionPrefix("unknown-context"))
}

func TestStripDistributionPrefix_EmptyReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cluster.ExportStripDistributionPrefix(""))
}

// ===========================================================================
// isEmptyYAML — empty YAML detection
// ===========================================================================

func TestIsEmptyYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty file", "", true},
		{"only separators", "---\n---\n", true},
		{"only whitespace", "   \n  \n", true},
		{"mixed separators and whitespace", "---\n\n  \n---", true},
		{"has content", "apiVersion: v1\nkind: Pod", false},
		{"separator with content", "---\napiVersion: v1", false},
		{"single non-empty line", "hello", false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "test.yaml")
			require.NoError(t, os.WriteFile(path, []byte(testCase.content), 0o600))

			got := cluster.ExportIsEmptyYAML(path)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestIsEmptyYAML_NonexistentFile(t *testing.T) {
	t.Parallel()

	got := cluster.ExportIsEmptyYAML("/nonexistent/path/file.yaml")
	assert.False(t, got)
}

// ===========================================================================
// hasK3sArg — K3d config argument detection
// ===========================================================================

func TestHasK3sArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []v1alpha5.K3sArgWithNodeFilters
		flag string
		want bool
	}{
		{
			name: "flag present",
			args: []v1alpha5.K3sArgWithNodeFilters{
				{Arg: "--disable=traefik"},
				{Arg: "--disable=local-storage"},
			},
			flag: "--disable=local-storage",
			want: true,
		},
		{
			name: "flag absent",
			args: []v1alpha5.K3sArgWithNodeFilters{
				{Arg: "--disable=traefik"},
			},
			flag: "--disable=local-storage",
			want: false,
		},
		{
			name: "empty args",
			args: nil,
			flag: "--disable=traefik",
			want: false,
		},
		{
			name: "partial match is not a match",
			args: []v1alpha5.K3sArgWithNodeFilters{
				{Arg: "--disable=traefik-extra"},
			},
			flag: "--disable=traefik",
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &v1alpha5.SimpleConfig{
				Options: v1alpha5.SimpleConfigOptions{
					K3sOptions: v1alpha5.SimpleConfigOptionsK3s{
						ExtraArgs: testCase.args,
					},
				},
			}

			got := cluster.ExportHasK3sArg(config, testCase.flag)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// ===========================================================================
// validateOutputFormat — output format validation
// ===========================================================================

func TestValidateOutputFormat_ValidText(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("output", string(cluster.ExportOutputFormatText), "output format")
	assert.NoError(t, cluster.ExportValidateOutputFormat(cmd))
}

func TestValidateOutputFormat_ValidJSON(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("output", string(cluster.ExportOutputFormatJSON), "output format")
	assert.NoError(t, cluster.ExportValidateOutputFormat(cmd))
}

func TestValidateOutputFormat_Invalid(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("output", "xml", "output format")
	err := cluster.ExportValidateOutputFormat(cmd)
	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrUnsupportedOutputFormat)
}

func TestValidateOutputFormat_NilCmd(t *testing.T) {
	t.Parallel()

	// nil cmd should default to "text" and pass validation
	assert.NoError(t, cluster.ExportValidateOutputFormat(nil))
}

func TestValidateOutputFormat_NoOutputFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	// No --output flag registered, should default to text
	assert.NoError(t, cluster.ExportValidateOutputFormat(cmd))
}

// ===========================================================================
// validateTarEntry — additional edge cases
// ===========================================================================

func TestValidateTarEntry_NestedRegularFile(t *testing.T) {
	t.Parallel()

	header := &tar.Header{
		Name:     "backup/namespaces/default/pods.yaml",
		Typeflag: tar.TypeReg,
	}

	path, err := cluster.ExportValidateTarEntry(header, "/tmp/dest")
	require.NoError(t, err)
	assert.Contains(t, path, "pods.yaml")
}

func TestValidateTarEntry_DotSlashPrefix(t *testing.T) {
	t.Parallel()

	header := &tar.Header{
		Name:     "./backup/file.yaml",
		Typeflag: tar.TypeReg,
	}

	_, err := cluster.ExportValidateTarEntry(header, "/tmp/dest")
	assert.NoError(t, err)
}

func TestValidateTarEntry_TrailingDotDot(t *testing.T) {
	t.Parallel()

	header := &tar.Header{
		Name:     "backup/../../../etc/shadow",
		Typeflag: tar.TypeReg,
	}

	_, err := cluster.ExportValidateTarEntry(header, "/tmp/dest")
	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrInvalidTarPath)
}

// ===========================================================================
// classifyRestoreError — error fallback to err.Error()
// ===========================================================================

func TestClassifyRestoreError_AlreadyExistsFromErrMsg(t *testing.T) {
	t.Parallel()

	// When stderr is empty but error message says "already exists", should be nil with "none" policy
	err := cluster.ExportClassifyRestoreError(
		errClusterPureResourceAlreadyExists,
		"",
		"none",
	)
	assert.NoError(t, err)
}

func TestClassifyRestoreError_EmptyStderrWithUpdatePolicy(t *testing.T) {
	t.Parallel()

	err := cluster.ExportClassifyRestoreError(
		errClusterPureGeneric,
		"",
		"update",
	)
	assert.Error(t, err)
}

// ===========================================================================
// formatRemainingDuration — additional edge cases
// ===========================================================================

func TestFormatRemainingDuration_LargeDuration(t *testing.T) {
	t.Parallel()

	got := cluster.ExportFormatRemainingDuration(48*time.Hour + 30*time.Minute)
	assert.Equal(t, "48h 30m", got)
}

func TestFormatRemainingDuration_ExactlyZeroMinutes(t *testing.T) {
	t.Parallel()

	got := cluster.ExportFormatRemainingDuration(3 * time.Hour)
	assert.Equal(t, "3h", got)
}

// ===========================================================================
// splitYAMLDocuments — additional edge cases
// ===========================================================================

func TestSplitYAMLDocuments_MultipleTrailingSeparators(t *testing.T) {
	t.Parallel()

	docs := cluster.ExportSplitYAMLDocuments("a: 1\n---\n---\n---\nb: 2")
	require.Len(t, docs, 2)
	assert.Contains(t, docs[0], "a: 1")
	assert.Contains(t, docs[1], "b: 2")
}

func TestSplitYAMLDocuments_OnlySeparators(t *testing.T) {
	t.Parallel()

	docs := cluster.ExportSplitYAMLDocuments("---\n---\n---")
	assert.Empty(t, docs)
}

// ===========================================================================
// addLabelsToDocument — additional edge cases
// ===========================================================================

func TestAddLabelsToDocument_PreservesExistingLabels(t *testing.T) {
	t.Parallel()

	doc := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\n  labels:\n    app: foo\n    env: prod"
	got, err := cluster.ExportAddLabelsToDocument(doc, "backup-1", "restore-1")
	require.NoError(t, err)

	// Original labels should still be present
	assert.Contains(t, got, "app")
	assert.Contains(t, got, "env")
	// New labels should be added
	assert.Contains(t, got, "ksail.io/backup-name")
	assert.Contains(t, got, "ksail.io/restore-name")
}

// ===========================================================================
// allLinesContain — additional edge cases
// ===========================================================================

func TestAllLinesContain_OnlyEmptyLines(t *testing.T) {
	t.Parallel()

	got := cluster.ExportAllLinesContain("  \n  \n  ", "anything")
	assert.False(t, got)
}

func TestAllLinesContain_MultilineMatch(t *testing.T) {
	t.Parallel()

	got := cluster.ExportAllLinesContain(
		"error: already exists\nwarning: already exists\ninfo: already exists",
		"already exists",
	)
	assert.True(t, got)
}

// ===========================================================================
// matchesKindPattern — additional edge cases
// ===========================================================================

func TestMatchesKindPattern_WorkerZero(t *testing.T) {
	t.Parallel()

	got := cluster.ExportMatchesKindPattern("mycluster-worker0", "mycluster")
	assert.True(t, got)
}

func TestMatchesKindPattern_WorkerWithMixedSuffix(t *testing.T) {
	t.Parallel()

	got := cluster.ExportMatchesKindPattern("mycluster-worker1a", "mycluster")
	assert.False(t, got)
}

// ===========================================================================
// countYAMLDocuments — additional edge cases
// ===========================================================================

func TestCountYAMLDocuments_MixedListAndKind(t *testing.T) {
	t.Parallel()

	content := "- apiVersion: v1\n  kind: Pod\nkind: Service"
	got := cluster.ExportCountYAMLDocuments(content)
	assert.Equal(t, 2, got)
}

// ===========================================================================
// displayChangesSummary — validates severity ordering in table output
// ===========================================================================

func TestDisplayChangesSummary_RecreateBeforeRebootBeforeInPlace(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "in-place-field",
				OldValue: "a",
				NewValue: "b",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "reboot-field",
				OldValue: "c",
				NewValue: "d",
				Category: clusterupdate.ChangeCategoryRebootRequired,
			},
		},
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "recreate-field",
				OldValue: "e",
				NewValue: "f",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		},
	}

	got := cluster.ExportFormatDiffTable(diff, 3)

	// Verify order: recreate comes before reboot, which comes before in-place
	recreateIdx := findSubstringIndex(got, "recreate-field")
	rebootIdx := findSubstringIndex(got, "reboot-field")
	inPlaceIdx := findSubstringIndex(got, "in-place-field")

	assert.Less(t, recreateIdx, rebootIdx, "recreate should appear before reboot")
	assert.Less(t, rebootIdx, inPlaceIdx, "reboot should appear before in-place")
}

func findSubstringIndex(s, substr string) int {
	for i := range len(s) - len(substr) + 1 {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}

// ===========================================================================
// stripParenthetical — additional edge cases
// ===========================================================================

func TestStripParenthetical_SpaceBeforeOpen(t *testing.T) {
	t.Parallel()

	got := cluster.ExportStripParenthetical("Docker (local)")
	assert.Equal(t, "Docker", got)
}

func TestStripParenthetical_DoubleParens(t *testing.T) {
	t.Parallel()

	got := cluster.ExportStripParenthetical("A (b) (c)")
	assert.Equal(t, "A (b)", got)
}

// ===========================================================================
// filterExcludedTypes — additional edge cases
// ===========================================================================

func TestFilterExcludedTypes_DuplicateExclusions(t *testing.T) {
	t.Parallel()

	got := cluster.ExportFilterExcludedTypes(
		[]string{"pods", "services", "deployments"},
		[]string{"pods", "pods"},
	)
	assert.Equal(t, []string{"services", "deployments"}, got)
}

func TestFilterExcludedTypes_PreservesOrder(t *testing.T) {
	t.Parallel()

	got := cluster.ExportFilterExcludedTypes(
		[]string{"z", "a", "m", "b"},
		[]string{"m"},
	)
	assert.Equal(t, []string{"z", "a", "b"}, got)
}

// ===========================================================================
// deriveBackupName — additional edge cases
// ===========================================================================

func TestDeriveBackupName_OnlyExtension(t *testing.T) {
	t.Parallel()

	got := cluster.ExportDeriveBackupName(".tar.gz")
	assert.Empty(t, got)
}

func TestDeriveBackupName_NoDirectory(t *testing.T) {
	t.Parallel()

	got := cluster.ExportDeriveBackupName("simple.tgz")
	assert.Equal(t, "simple", got)
}

// ===========================================================================
// isNumericString — additional edge cases
// ===========================================================================

func TestIsNumericString_SingleZero(t *testing.T) {
	t.Parallel()
	assert.True(t, cluster.ExportIsNumericString("0"))
}

func TestIsNumericString_Unicode(t *testing.T) {
	t.Parallel()
	assert.False(t, cluster.ExportIsNumericString("①②③"))
}

// ===========================================================================
// isKindClusterFromNodes — additional edge cases
// ===========================================================================

func TestIsKindClusterFromNodes_MultipleWorkers(t *testing.T) {
	t.Parallel()

	nodes := []string{
		"mycluster-control-plane",
		"mycluster-worker",
		"mycluster-worker2",
		"mycluster-worker3",
	}
	assert.True(t, cluster.ExportIsKindClusterFromNodes(nodes, "mycluster"))
}

// ===========================================================================
// Permissions and constants
// ===========================================================================

func TestDirFilePerm(t *testing.T) {
	t.Parallel()
	assert.Equal(t, os.FileMode(0o750), os.FileMode(cluster.ExportDirPerm))
	assert.Equal(t, os.FileMode(0o600), os.FileMode(cluster.ExportFilePerm))
}

func TestOutputFormatConstants(t *testing.T) {
	t.Parallel()
	assert.NotEmpty(t, cluster.ExportOutputFormatJSON)
	assert.NotEmpty(t, cluster.ExportOutputFormatText)
	assert.NotEqual(t, cluster.ExportOutputFormatJSON, cluster.ExportOutputFormatText)
}
