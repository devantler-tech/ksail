package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
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

func setupListTest(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.dev/v1alpha1
kind: Cluster
spec:
  distribution: Kind
  distributionConfig: kind.yaml
  metricsServer: Disabled
  connection:
    kubeconfig: ./kubeconfig
`

	require.NoError(
		t,
		os.WriteFile(filepath.Join(workingDir, "ksail.yaml"), []byte(ksailYAML), 0o600),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(workingDir, "kind.yaml"),
		[]byte("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n"),
		0o600,
	))
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_NoClusterFound(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", false, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{}}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_SingleClusterFound(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", false, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_MultipleClustersFound(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", false, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				clusters: []string{"cluster-1", "cluster-2", "cluster-3"},
			}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_WithAllFlag(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", true, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))
	_ = cmd.Flags().Set("all", "true")

	// Create a mock factory that returns test-cluster for primary distribution
	// and empty clusters for other distributions.
	primaryFactory := fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
	emptyFactory := fakeFactoryWithClusters{clusters: []string{}}

	deps := clusterpkg.ListDeps{
		// Use mock factory for all distributions to avoid hitting real Docker.
		DistributionFactoryCreator: func(dist v1alpha1.Distribution) clusterprovisioner.Factory {
			// Primary distribution (Kind) returns test-cluster, others return empty
			if dist == v1alpha1.DistributionKind {
				return primaryFactory
			}

			return emptyFactory
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_ListError(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", false, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				listErr: fmt.Errorf("test error: %w", errTestListClusters),
			}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to list clusters")
}

//nolint:paralleltest // uses t.Chdir
func TestHandleListRunE_Success(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", false, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test"}}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.NoError(t, err)
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_FactoryError(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupListTest(t, workingDir)

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.Flags().BoolP("all", "a", false, "List all clusters")
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	deps := clusterpkg.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithErrors{}
		},
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to resolve cluster provisioner")
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
