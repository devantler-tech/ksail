package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	errTestListClusters = errors.New("failed to list clusters")
	errTestFactoryError = errors.New("factory error")
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

// fakeProvisionerWithClusters returns a list of clusters for testing.
type fakeProvisionerWithClusters struct {
	clusters []string
	listErr  error
}

func (f *fakeProvisionerWithClusters) Create(context.Context, string) error { return nil }
func (f *fakeProvisionerWithClusters) Delete(context.Context, string) error { return nil }
func (f *fakeProvisionerWithClusters) Start(context.Context, string) error  { return nil }
func (f *fakeProvisionerWithClusters) Stop(context.Context, string) error   { return nil }
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
) (clusterprovisioner.ClusterProvisioner, any, error) {
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

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{}}
		},
	}

	// Filter to Docker provider - no output for empty list
	err := clusterpkg.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
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

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
	}

	// Filter to Docker provider
	err := clusterpkg.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
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

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				clusters: []string{"cluster-1", "cluster-2", "cluster-3"},
			}
		},
	}

	// Filter to Docker provider
	err := clusterpkg.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_AllProviders(t *testing.T) {
	// Clear HCLOUD_TOKEN to ensure Hetzner provider is skipped
	t.Setenv("HCLOUD_TOKEN", "")

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	// Create a mock factory that returns test-cluster for all distributions
	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
	}

	// No filter = list all providers (default behavior)
	// Hetzner will be skipped since HCLOUD_TOKEN is cleared
	err := clusterpkg.HandleListRunE(cmd, "", deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_ListError(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				listErr: fmt.Errorf("test error: %w", errTestListClusters),
			}
		},
	}

	// List errors per distribution are silently skipped - command succeeds with no clusters found
	err := clusterpkg.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
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

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test"}}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_FactoryError(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithErrors{}
		},
	}

	// Factory errors per distribution are silently skipped - command succeeds with no clusters found
	err := clusterpkg.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
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

	deps := clusterpkg.ListDeps{}

	// Invalid provider filter is logged as warning, command still succeeds
	err := clusterpkg.HandleListRunE(cmd, "InvalidProvider", deps)
	require.NoError(t, err)

	// Output should show no clusters found since invalid provider returns nothing
	require.Contains(t, buf.String(), "No clusters found")
}

// fakeFactoryWithErrors creates a provisioner that returns an error.
type fakeFactoryWithErrors struct{}

func (fakeFactoryWithErrors) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	return nil, nil, fmt.Errorf("test error: %w", errTestFactoryError)
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.ClusterProvisioner = (*fakeProvisionerWithClusters)(nil)
	_ clusterprovisioner.Factory            = (*fakeFactoryWithClusters)(nil)
	_ clusterprovisioner.Factory            = (*fakeFactoryWithErrors)(nil)
)
