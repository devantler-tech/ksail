package cluster_test

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/testutil/rootcheck"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errClusterPureResourceAlreadyExists = errors.New("resource already exists")
	errClusterPureGeneric               = errors.New("some error")
	errClusterPureTalosConfigEmpty      = errors.New("talos config file is empty")
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

func TestFormatDiffTable_UnknownBaselineOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		UnknownBaseline: []clusterupdate.Change{
			{
				Field:    "cluster.cni",
				OldValue: clusterupdate.UnknownBaselineValue,
				NewValue: "Cilium",
				Category: clusterupdate.ChangeCategoryUnknown,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 0)

	assert.Contains(t, got, "cluster.cni")
	assert.Contains(t, got, "Unknown")
	assert.Contains(t, got, "Cilium")
	assert.Contains(t, got, "⚪")
	// The summary line must not claim confident configuration changes.
	assert.Contains(t, got, "could not be read")
	assert.NotContains(t, got, "Detected 0 configuration changes")
}

func TestFormatDiffTable_RealAndUnknownTogether(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "cluster.workers",
				OldValue: "1",
				NewValue: "3",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
		UnknownBaseline: []clusterupdate.Change{
			{
				Field:    "cluster.gitOpsEngine",
				OldValue: clusterupdate.UnknownBaselineValue,
				NewValue: "Flux",
				Category: clusterupdate.ChangeCategoryUnknown,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)

	assert.Contains(t, got, "cluster.workers")
	assert.Contains(t, got, "cluster.gitOpsEngine")
	assert.Contains(t, got, "🟢")
	assert.Contains(t, got, "⚪")
	assert.Contains(t, got, "unknown baseline")
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

func TestStripDistributionPrefix_K3kReturnsClusterName(t *testing.T) {
	t.Parallel()
	// Nested K3s clusters (k3k operator on the Kubernetes provider) use a "k3k-"
	// context prefix rather than the standalone "k3d-"; they must still be stripped
	// to the bare cluster name so they appear in completion and the picker.
	assert.Equal(t, "nested", cluster.ExportStripDistributionPrefix("k3k-nested"))
}

// ===========================================================================
// resolveContextName — cluster name → kubeconfig context resolution
// ===========================================================================

func TestResolveContextName_K3kNestedK3sResolvesDeterministically(t *testing.T) {
	t.Parallel()

	// Nested K3s clusters (k3k operator on the Kubernetes provider) use a "k3k-"
	// context prefix rather than the standalone "k3d-". The explicit k3k- candidate
	// must win so resolution stays deterministic instead of falling back to substring
	// matching — which would also match the unrelated context below and report the
	// name as ambiguous.
	got, err := cluster.ExportResolveContextName(
		[]string{"k3k-nested", "external-nested-ctx"},
		"nested",
	)

	require.NoError(t, err)
	assert.Equal(t, "k3k-nested", got)
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

// ===========================================================================
// refreshAndVerifyKubeconfig — best-effort kubeconfig refresh
// ===========================================================================

// mockKubeconfigRefresher is a test double for clusterprovisioner.KubeconfigRefresher.
type mockKubeconfigRefresher struct {
	err    error
	called bool
	onCall func() // optional side-effect (e.g., create the kubeconfig file)
}

func (m *mockKubeconfigRefresher) RefreshKubeconfig(_ context.Context, _ string) error {
	m.called = true
	if m.onCall != nil {
		m.onCall()
	}

	return m.err
}

// newTestCmd returns a minimal *cobra.Command suitable for unit tests.
func newTestCmd(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	return cmd
}

// newTestClusterCfg returns a *v1alpha1.Cluster whose kubeconfig path is set to
// the given absolute path with a fixed test context name.
func newTestClusterCfg(kubeconfigPath string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath
	cfg.Spec.Cluster.Connection.Context = "test-context"

	return cfg
}

// TestRefreshAndVerifyKubeconfig_ValidKubeconfigSkipsRefresh verifies that when a
// valid kubeconfig already exists (staleness check returns false), the refresh is
// skipped entirely and no error is returned.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_ValidKubeconfigSkipsRefresh(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(kcPath, []byte("placeholder"), 0o600))

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return false })
	defer restore()

	refresher := &mockKubeconfigRefresher{}
	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.NoError(t, err)
	assert.False(t, refresher.called, "refresher should not be called when kubeconfig is valid")
}

// TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshSucceeds verifies that when no
// kubeconfig exists and the refresh succeeds (and creates the file), no error is returned.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshSucceeds(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	// File does not exist yet.

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{
		onCall: func() {
			// Simulate the provisioner writing the kubeconfig file.
			_ = os.WriteFile(kcPath, []byte("placeholder"), 0o600)
		},
	}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.NoError(t, err)
	assert.True(t, refresher.called)
}

// TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshFails verifies that when no
// kubeconfig exists and the refresh also fails, a hard error is returned.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshFails(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	// File does not exist.

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{err: errClusterPureTalosConfigEmpty}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to refresh kubeconfig")
	require.ErrorContains(t, err, "talos config file is empty")
	assert.True(t, refresher.called)
}

// TestRefreshAndVerifyKubeconfig_StaleKubeconfigRefreshFailsWarns verifies that when
// a stale kubeconfig already exists and the refresh fails, the function warns but
// returns nil so that downstream operations can still attempt to use the existing file.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_StaleKubeconfigRefreshFailsWarns(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(kcPath, []byte("placeholder"), 0o600))

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{err: errClusterPureTalosConfigEmpty}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.NoError(t, err, "should warn and proceed when stale file exists but refresh fails")
	assert.True(t, refresher.called)
}

// TestRefreshAndVerifyKubeconfig_StatPermissionError verifies that non-ENOENT
// os.Stat errors (e.g. permission denied) are returned immediately rather than
// being misinterpreted as "file missing".
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_StatPermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if rootcheck.IsRootUser() {
		t.Skip("running as root — permission checks are bypassed")
	}

	dir := t.TempDir()
	// Create a directory that we can't read (os.Stat on a file inside it will
	// fail with EACCES on most Unix systems).
	noAccessDir := filepath.Join(dir, "noaccess")
	require.NoError(t, os.MkdirAll(noAccessDir, 0o000))

	t.Cleanup(
		func() { _ = os.Chmod(noAccessDir, 0o750) }, //nolint:gosec // Restore access for cleanup.
	)

	kcPath := filepath.Join(noAccessDir, "config")

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to stat kubeconfig")
	assert.False(t, refresher.called, "should not attempt refresh on permission error")
}

// ===========================================================================
// runDiagnoseTextReport — output formatting for text output mode
// ===========================================================================

func TestRunDiagnoseTextReport_HealthyCluster(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "my-cluster",
		HealthScore: 100,
		Findings:    []k8s.DiagnoseFinding{},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseTextReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"my-cluster"`)
	assert.Contains(t, out, "looks healthy")
	assert.Contains(t, out, "Health score: 100/100")
}

func TestRunDiagnoseTextReport_WithFindings(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "broken-cluster",
		HealthScore: 75,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityCritical,
				Resource:    "pod/crash-pod (default)",
				Reason:      "CrashLoopBackOff",
				Remediation: "Check pod logs for errors.",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseTextReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"broken-cluster"`)
	assert.Contains(t, out, "Health score: 75/100")
	assert.Contains(t, out, "critical")
	assert.Contains(t, out, "pod/crash-pod (default)")
	assert.Contains(t, out, "CrashLoopBackOff")
	assert.Contains(t, out, "Suggested fix: Check pod logs for errors.")
}

func TestRunDiagnoseTextReport_FindingWithoutRemediation(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "cluster",
		HealthScore: 90,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityWarning,
				Resource:    "pvc/data-pvc (default)",
				Reason:      "Pending",
				Remediation: "",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseTextReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "warning")
	assert.Contains(t, out, "pvc/data-pvc (default)")
	assert.NotContains(t, out, "Suggested fix")
}

// ===========================================================================
// runDiagnoseJSONReport — JSON serialisation
// ===========================================================================

func TestRunDiagnoseJSONReport_HealthyCluster(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "my-cluster",
		HealthScore: 100,
		Findings:    []k8s.DiagnoseFinding{},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseJSONReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"clusterName": "my-cluster"`)
	assert.Contains(t, out, `"healthScore": 100`)
	assert.Contains(t, out, `"findings": []`)
}

// TestRunDiagnoseJSONReport_DoesNotEscapeHTML verifies the fix for the JSON
// HTML-escaping issue: '<', '>', '&' (e.g. in remediation hints like "<name>")
// appear literally instead of being </>/&-escaped.
func TestRunDiagnoseJSONReport_DoesNotEscapeHTML(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "broken-cluster",
		HealthScore: 90,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityWarning,
				Resource:    "pvc/stuck (default)",
				Reason:      "PVC is stuck in Pending phase",
				Remediation: "Run 'ksail workload describe pvc/<name> -n <namespace>'.",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseJSONReport(report, &buf)
	require.NoError(t, err)

	out := buf.String()
	// With HTML-escaping disabled, '<', '>' and '&' appear literally. If
	// escaping were enabled they would be emitted as their unicode escape
	// sequences instead, so the literal substring assertions below would fail.
	assert.Contains(t, out, "<name>")
	assert.Contains(t, out, "<namespace>")
}

func TestRunDiagnoseJSONReport_WithFindings(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "broken-cluster",
		HealthScore: 75,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityCritical,
				Resource:    "pod/crash-pod (default)",
				Reason:      "CrashLoopBackOff",
				Remediation: "Check pod logs for errors.",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseJSONReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"clusterName": "broken-cluster"`)
	assert.Contains(t, out, `"healthScore": 75`)
	assert.Contains(t, out, `"severity": "critical"`)
	assert.Contains(t, out, `"resource": "pod/crash-pod (default)"`)
	assert.Contains(t, out, `"reason": "CrashLoopBackOff"`)
	assert.Contains(t, out, `"remediation": "Check pod logs for errors."`)
}

func TestResolveCreatedContextName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		clusterName  string
		expected     string
	}{
		{
			name:         "k3s on kubernetes provider uses k3k prefix",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderKubernetes,
			clusterName:  "nested-k3s",
			expected:     "k3k-nested-k3s",
		},
		{
			name:         "k3s on docker provider uses standalone k3d prefix",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			clusterName:  "my-cluster",
			expected:     "k3d-my-cluster",
		},
		{
			name:         "vanilla on kubernetes provider uses standalone kind prefix",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderKubernetes,
			clusterName:  "nested-vanilla",
			expected:     "kind-nested-vanilla",
		},
		{
			name:         "empty cluster name returns empty",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderKubernetes,
			clusterName:  "",
			expected:     "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportResolveCreatedContextName(
				testCase.distribution,
				testCase.provider,
				testCase.clusterName,
			)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

// ===========================================================================
// normalizePinnedVersion — pinned-version normalization & downgrade guard
// ===========================================================================

const (
	// testPinCurrent is the running version used as the baseline in the
	// normalizePinnedVersion cases; testPinNewer is a pin strictly newer than it.
	testPinCurrent = "v1.8.0"
	testPinNewer   = "v1.9.0"
)

func TestNormalizePinnedVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rawPinned   string
		current     string
		wantVersion string
		wantReason  cluster.ExportPinnedVersionSkipReason
	}{
		{
			name:        "newer pin proceeds with upgrade",
			rawPinned:   testPinNewer,
			current:     testPinCurrent,
			wantVersion: testPinNewer,
			wantReason:  cluster.ExportPinnedVersionProceed,
		},
		{
			name:        "missing v prefix is normalized",
			rawPinned:   "1.9.0",
			current:     testPinCurrent,
			wantVersion: testPinNewer,
			wantReason:  cluster.ExportPinnedVersionProceed,
		},
		{
			name:        "pin equal to current is a no-op",
			rawPinned:   testPinCurrent,
			current:     testPinCurrent,
			wantVersion: testPinCurrent,
			wantReason:  cluster.ExportPinnedVersionAlreadyAtIt,
		},
		{
			// Regression for the VCluster phantom-upgrade bug: an unprefixed SDK pin
			// ("0.34.1", VCluster's ChartVersion()) must still match an unprefixed
			// current of the same version via parsed-semver equality, not raw strings.
			name:        "unprefixed pin equal to unprefixed current is a no-op",
			rawPinned:   "0.34.1",
			current:     "0.34.1",
			wantVersion: "v0.34.1",
			wantReason:  cluster.ExportPinnedVersionAlreadyAtIt,
		},
		{
			// Cross-prefix equality: pin normalizes to "v..." while current is raw.
			name:        "unprefixed pin equal to v-prefixed current is a no-op",
			rawPinned:   "0.34.1",
			current:     "v0.34.1",
			wantVersion: "v0.34.1",
			wantReason:  cluster.ExportPinnedVersionAlreadyAtIt,
		},
		{
			name:        "older pin than current skips the downgrade",
			rawPinned:   "v1.7.0",
			current:     testPinCurrent,
			wantVersion: "v1.7.0",
			wantReason:  cluster.ExportPinnedVersionNewer,
		},
		{
			name:        "unparseable current version falls through to proceed",
			rawPinned:   testPinNewer,
			current:     "",
			wantVersion: testPinNewer,
			wantReason:  cluster.ExportPinnedVersionProceed,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotVersion, gotReason, err := cluster.ExportNormalizePinnedVersion(
				testCase.rawPinned, testCase.current,
			)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantVersion, gotVersion)
			assert.Equal(t, testCase.wantReason, gotReason)
		})
	}
}

func TestNormalizePinnedVersion_EmptyPinReturnsError(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   "} {
		gotVersion, gotReason, err := cluster.ExportNormalizePinnedVersion(raw, testPinCurrent)
		require.ErrorIs(t, err, cluster.ErrEmptyPinnedVersion)
		assert.Empty(t, gotVersion)
		assert.Equal(t, cluster.ExportPinnedVersionProceed, gotReason)
	}
}

func TestNormalizePinnedVersion_InvalidPinReturnsError(t *testing.T) {
	t.Parallel()

	gotVersion, gotReason, err := cluster.ExportNormalizePinnedVersion(
		"not-a-version", testPinCurrent,
	)
	require.Error(t, err)
	require.NotErrorIs(t, err, cluster.ErrEmptyPinnedVersion)
	assert.Contains(t, err.Error(), "invalid pinned Talos version")
	assert.Empty(t, gotVersion)
	assert.Equal(t, cluster.ExportPinnedVersionProceed, gotReason)
}
