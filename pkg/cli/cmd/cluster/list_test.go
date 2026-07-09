package cluster_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	errTestListClusters = errors.New("failed to list clusters")
	errTestFactoryError = errors.New("factory error")
)

// runningDockerStatus reports every cluster as Running, so STATUS-column snapshots are deterministic
// without a live Docker daemon (the real probe is exercised by clusterdiscovery's own tests).
func runningDockerStatus(
	_ context.Context,
	_ v1alpha1.Distribution,
	_ string,
) clusterdiscovery.RunState {
	return clusterdiscovery.RunStateRunning
}

// fakeProvisionerWithClusters returns a list of clusters for testing.
type fakeProvisionerWithClusters struct {
	clusters []string
	listErr  error
}

func (f *fakeProvisionerWithClusters) Create(context.Context, string) error { return nil }

func (f *fakeProvisionerWithClusters) Delete(context.Context, string) error { return nil }

func (f *fakeProvisionerWithClusters) Start(context.Context, string) error { return nil }

func (f *fakeProvisionerWithClusters) Stop(context.Context, string) error { return nil }

func (f *fakeProvisionerWithClusters) List(context.Context) ([]string, error) {
	return f.clusters, f.listErr
}

func (f *fakeProvisionerWithClusters) Exists(context.Context, string) (bool, error) {
	return len(f.clusters) > 0, nil
}

// fakeFactoryWithClusters creates a provisioner that returns clusters.
type fakeFactoryWithClusters struct {
	clusters []string
	listErr  error
}

func (f fakeFactoryWithClusters) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeProvisionerWithClusters{clusters: f.clusters, listErr: f.listErr}, cfg, nil
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_NoClusterFound_DockerProvider(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{}}
		},
	}

	// Filter to Docker provider - no output for empty list
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_SingleClusterFound_DockerProvider(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
		DockerStatusFunc: runningDockerStatus,
	}

	// Filter to Docker provider
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_MultipleClustersFound_DockerProvider(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				clusters: []string{"cluster-1", "cluster-2", "cluster-3"},
			}
		},
		DockerStatusFunc: runningDockerStatus,
	}

	// Filter to Docker provider
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

func TestListCmd_AllProviders(t *testing.T) {
	// Clear HCLOUD_TOKEN to ensure Hetzner provider is skipped
	t.Setenv("HCLOUD_TOKEN", "")

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	// Create a mock factory that returns test-cluster for all distributions
	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
		DockerStatusFunc: runningDockerStatus,
		// Point unmanaged discovery at a nonexistent kubeconfig so the snapshot stays deterministic
		// regardless of the test machine's real kubeconfig contexts.
		KubeconfigPathFunc: func() string { return filepath.Join(t.TempDir(), "no-kubeconfig") },
	}

	// No filter = list all providers (default behavior)
	// Hetzner will be skipped since HCLOUD_TOKEN is cleared
	err := cluster.HandleListRunE(cmd, "", deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

// writeListKubeconfig writes a minimal kubeconfig with the given context names to a temp file for the
// list command's unmanaged-discovery wiring tests, returning its path.
func writeListKubeconfig(t *testing.T, contextNames ...string) string {
	t.Helper()

	var buf bytes.Buffer

	buf.WriteString("apiVersion: v1\nkind: Config\nclusters:\n")

	for _, name := range contextNames {
		fmt.Fprintf(&buf,
			"  - name: %s\n    cluster:\n      server: https://127.0.0.1:6443\n", name)
	}

	buf.WriteString("contexts:\n")

	for _, name := range contextNames {
		fmt.Fprintf(&buf,
			"  - name: %s\n    context:\n      cluster: %s\n      user: %s\n", name, name, name)
	}

	buf.WriteString("users:\n")

	for _, name := range contextNames {
		fmt.Fprintf(&buf, "  - name: %s\n    user: {}\n", name)
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	return path
}

func TestListCmd_AllProviders_SurfacesUnmanaged(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	// "kind-test-cluster" detects to the managed cluster "test-cluster" (deduped); "colleague-cluster"
	// is an external context ksail never provisioned → surfaced as Unmanaged.
	kubeconfig := writeListKubeconfig(t, "kind-test-cluster", "colleague-cluster")

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
		DockerStatusFunc:   runningDockerStatus,
		KubeconfigPathFunc: func() string { return kubeconfig },
	}

	require.NoError(t, cluster.HandleListRunE(cmd, "", deps))

	out := buf.String()
	assert.Contains(t, out, "colleague-cluster", "the unmanaged context should be listed")
	assert.Contains(t, out, "Unmanaged", "the unmanaged context should carry STATUS=Unmanaged")
	assert.NotContains(t, out, "kind-test-cluster",
		"a managed cluster's kubeconfig context must be deduped, not re-surfaced as unmanaged")
}

func TestListCmd_OutputJSON_SurfacesUnmanaged(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "")

	kubeconfig := writeListKubeconfig(t, "colleague-cluster")

	cmd, buf := newListCmdWithJSONOutput(t)

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{}}
		},
		KubeconfigPathFunc: func() string { return kubeconfig },
	}

	require.NoError(t, cluster.HandleListRunE(cmd, "", deps))

	var rows []jsonListRow

	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "colleague-cluster", rows[0].Name)
	assert.Equal(t, "Unmanaged", rows[0].Status)
	assert.Empty(t, rows[0].Provider, "an unmanaged cluster has no provider")
	assert.Empty(t, rows[0].Distribution, "an unmanaged cluster has no distribution")
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_ListError(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				listErr: fmt.Errorf("test error: %w", errTestListClusters),
			}
		},
	}

	// List errors per distribution are silently skipped - command succeeds with no clusters found
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	// Since all distributions fail, no clusters found
	require.Contains(t, outBuf.String(), "No clusters found")
}

//nolint:paralleltest // uses t.Chdir
func TestHandleListRunE_Success(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test"}}
		},
	}

	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_FactoryError(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithErrors{}
		},
	}

	// Factory errors per distribution are silently skipped - command succeeds with no clusters found
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	// Since all distributions fail, no clusters found
	require.Contains(t, outBuf.String(), "No clusters found")
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_InvalidProviderFilter(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{}

	// Invalid provider filter is logged as warning, command still succeeds
	err := cluster.HandleListRunE(cmd, "InvalidProvider", deps)
	require.NoError(t, err)

	// Output should show no clusters found since invalid provider returns nothing
	require.Contains(t, buf.String(), "No clusters found")
}

// fakeFactoryWithErrors creates a provisioner that returns an error.
type fakeFactoryWithErrors struct{}

func (fakeFactoryWithErrors) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return nil, nil, fmt.Errorf("test error: %w", errTestFactoryError)
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.Provisioner = (*fakeProvisionerWithClusters)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactoryWithClusters)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactoryWithErrors)(nil)
)

func TestDisplayListResults_WithTTL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionVanilla,
			"cluster-1",
			nil,
		),
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionK3s,
			"dev-cluster",
			// Use 5h buffer so minute-boundary drift on slow CI is irrelevant.
			&state.TTLInfo{ExpiresAt: time.Now().Add(5*time.Hour + 30*time.Second)},
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "PROVIDER")
	assert.Contains(t, output, "DISTRIBUTION")
	assert.Contains(t, output, "CLUSTER")
	assert.Contains(t, output, "TTL")
	assert.Contains(t, output, "cluster-1")
	assert.Contains(t, output, "dev-cluster")
	assert.Regexp(t, `\d+h`, output)
}

func TestDisplayListResults_WithExpiredTTL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionTalos,
			"expired-cluster",
			&state.TTLInfo{ExpiresAt: time.Now().Add(-1 * time.Hour)},
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "TTL")
	assert.Contains(t, output, "EXPIRED")
	assert.Contains(t, output, "expired-cluster")
}

func TestDisplayListResults_NoTTLColumn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionVanilla,
			"my-cluster",
			nil,
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "PROVIDER")
	assert.Contains(t, output, "CLUSTER")
	assert.NotContains(t, output, "TTL")
}

func TestStatusLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Running", cluster.ExportStatusLabel(clusterdiscovery.RunStateRunning))
	assert.Equal(t, "Stopped", cluster.ExportStatusLabel(clusterdiscovery.RunStateStopped))
	assert.Equal(t, "Unknown", cluster.ExportStatusLabel(clusterdiscovery.RunStateUnknown))
}

func TestDisplayListResults_StatusColumn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResultWithRunState(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionVanilla,
			"running-cluster",
			clusterdiscovery.RunStateRunning,
		),
		cluster.ExportNewListResultWithRunState(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionK3s,
			"stopped-cluster",
			clusterdiscovery.RunStateStopped,
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "Running")
	assert.Contains(t, output, "Stopped")
}

func TestComponentLabel_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(none)", cluster.ExportComponentLabel(""))
}

func TestComponentLabel_None(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(none)", cluster.ExportComponentLabel("None"))
}

func TestComponentLabel_Disabled(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(disabled)", cluster.ExportComponentLabel("Disabled"))
}

func TestComponentLabel_ActiveValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Cilium", cluster.ExportComponentLabel("Cilium"))
}

func TestDisplayClusterIdentity_AllFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	info := &clusterdetector.Info{
		ClusterName:    "my-cluster",
		Distribution:   v1alpha1.DistributionVanilla,
		Provider:       v1alpha1.ProviderDocker,
		Context:        "kind-my-cluster",
		ServerURL:      "https://127.0.0.1:6443",
		KubeconfigPath: "/home/user/.kube/config",
	}

	cluster.ExportDisplayClusterIdentity(&buf, info)

	out := buf.String()
	assert.Contains(t, out, "KSail Cluster Details:")
	assert.Contains(t, out, "my-cluster")
	assert.Contains(t, out, string(v1alpha1.DistributionVanilla))
	assert.Contains(t, out, string(v1alpha1.ProviderDocker))
	assert.Contains(t, out, "kind-my-cluster")
	assert.Contains(t, out, "https://127.0.0.1:6443")
	assert.Contains(t, out, "/home/user/.kube/config")
}

func TestDisplayClusterIdentity_OptionalFieldsOmitted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	info := &clusterdetector.Info{
		ClusterName:  "bare-cluster",
		Distribution: v1alpha1.DistributionVanilla,
		Provider:     v1alpha1.ProviderDocker,
	}

	cluster.ExportDisplayClusterIdentity(&buf, info)

	out := buf.String()
	assert.Contains(t, out, "bare-cluster")
	assert.NotContains(t, out, "Context:")
	assert.NotContains(t, out, "Server:")
	assert.NotContains(t, out, "Kubeconfig:")
}

func TestDisplayTTLInfo_NoTTL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Using a nonexistent cluster name: no state file → silent return.
	cluster.ExportDisplayTTLInfo(&buf, "nonexistent-cluster-ttl-test")
	assert.Empty(t, buf.String())
}

func TestDisplayTTLInfo_WithActiveTTL(t *testing.T) {
	t.Parallel()

	clusterName := "display-ttl-active-test"

	err := state.SaveClusterTTL(clusterName, 2*time.Hour)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	var buf bytes.Buffer

	cluster.ExportDisplayTTLInfo(&buf, clusterName)

	out := buf.String()
	assert.Contains(t, out, "cluster TTL")
	assert.Contains(t, out, "remaining")
}

func TestDisplayTTLInfo_WithExpiredTTL(t *testing.T) {
	t.Parallel()

	clusterName := "display-ttl-expired-test"

	// Save a TTL of 1ms so it is already expired by the time we read it.
	err := state.SaveClusterTTL(clusterName, time.Millisecond)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	// Wait long enough for the TTL to lapse.
	time.Sleep(10 * time.Millisecond)

	var buf bytes.Buffer

	cluster.ExportDisplayTTLInfo(&buf, clusterName)

	out := buf.String()
	assert.Contains(t, out, "EXPIRED")
}

func TestDisplayComponents_NoState(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// No saved spec → displayComponents returns silently.
	cluster.ExportDisplayComponents(&buf, "nonexistent-cluster-components-test")
	assert.Empty(t, buf.String())
}

func TestDisplayComponents_WithState(t *testing.T) {
	t.Parallel()

	clusterName := "display-components-test"

	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
		CNI:           v1alpha1.CNICilium,
		CSI:           v1alpha1.CSIDisabled,
		MetricsServer: v1alpha1.MetricsServerDisabled,
		LoadBalancer:  v1alpha1.LoadBalancerDisabled,
		CertManager:   v1alpha1.CertManagerDisabled,
		PolicyEngine:  v1alpha1.PolicyEngineNone,
	}

	err := state.SaveClusterSpec(clusterName, spec)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	var buf bytes.Buffer

	cluster.ExportDisplayComponents(&buf, clusterName)

	out := buf.String()
	assert.Contains(t, out, "Components:")
	assert.Contains(t, out, string(v1alpha1.GitOpsEngineFlux))
	assert.Contains(t, out, string(v1alpha1.CNICilium))
	assert.Contains(t, out, "(disabled)")
	assert.Contains(t, out, "(none)")
}

// jsonListRow mirrors the cluster.ListItemJSON contract for decoding in tests.
// Keeping a local copy guards against accidental field/tag changes: any rename
// of the exported shape breaks this decode.
type jsonListRow struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Distribution string  `json:"distribution"`
	Status       string  `json:"status"`
	TTL          *string `json:"ttl"`
}

// newListCmdWithJSONOutput builds a command wired like NewListCmd for the JSON
// path: it registers the --output flag and sets it to json.
func newListCmdWithJSONOutput(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()

	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("output", "text", "Output format")
	require.NoError(t, cmd.Flags().Set("output", "json"))

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(io.Discard)
	cmd.SetContext(context.Background())

	return cmd, &buf
}

//nolint:paralleltest // uses isolated HOME state via TestMain
func TestListCmd_OutputJSON_EmptyIsArray(t *testing.T) {
	cmd, buf := newListCmdWithJSONOutput(t)

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{}}
		},
	}

	require.NoError(t, cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps))

	var rows []jsonListRow

	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	assert.Empty(t, rows)
	assert.Equal(t, "[]\n", buf.String())
}

//nolint:paralleltest // uses isolated HOME state via TestMain
func TestListCmd_OutputJSON_Contract(t *testing.T) {
	cmd, buf := newListCmdWithJSONOutput(t)

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"json-contract-cluster"}}
		},
		DockerStatusFunc: runningDockerStatus,
	}

	require.NoError(t, cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps))

	var rows []jsonListRow

	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "json-contract-cluster", rows[0].Name)
	assert.Equal(t, "docker", rows[0].Provider)
	assert.Equal(t, string(v1alpha1.DistributionVanilla), rows[0].Distribution)
	// Run-state probe stubbed to Running → status field is "Running".
	assert.Equal(t, "Running", rows[0].Status)
	// No TTL set → null.
	assert.Nil(t, rows[0].TTL)
}

//nolint:paralleltest // uses isolated HOME state via TestMain
func TestListCmd_OutputJSON_TTLIsString(t *testing.T) {
	clusterName := "json-ttl-cluster"
	require.NoError(t, state.SaveClusterTTL(clusterName, time.Hour))

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	cmd, buf := newListCmdWithJSONOutput(t)

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{clusterName}}
		},
	}

	require.NoError(t, cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps))

	var rows []jsonListRow

	require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].TTL)
	assert.NotEmpty(t, *rows[0].TTL)
}
