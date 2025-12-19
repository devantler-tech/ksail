package cluster_test

import (
	"bytes"
	"context"
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/cmd/cluster"
	"github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListCmd_NoAllFlag tests listing clusters without the --all flag.
func TestListCmd_NoAllFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusters       []string
		expectedOutput string
	}{
		{
			name:           "no clusters found",
			clusters:       []string{},
			expectedOutput: "► no clusters found",
		},
		{
			name:           "single cluster",
			clusters:       []string{"test-cluster"},
			expectedOutput: "test-cluster",
		},
		{
			name:           "multiple clusters",
			clusters:       []string{"cluster1", "cluster2"},
			expectedOutput: "cluster1, cluster2",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			cmd := setupTestCommand(t, &buf)

			// Create fake factory and config manager
			factory := &fakeListFactory{clusters: testCase.clusters}
			cfgManager := setupTestConfig(t, cmd, false)
			deps := clusterpkg.ListDeps{
				Factory:             factory,
				DistributionFactory: factory,
			}

			err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
			require.NoError(t, err)

			output := buf.String()
			assert.Contains(t, output, testCase.expectedOutput)
		})
	}
}

// TestListCmd_WithAllFlag tests listing clusters with the --all flag.
func TestListCmd_WithAllFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		kindClusters        []string
		k3dClusters         []string
		expectedKindSection string
		expectedK3dSection  string
	}{
		{
			name:                "no clusters in either distribution",
			kindClusters:        []string{},
			k3dClusters:         []string{},
			expectedKindSection: "---|kind|---\nNo kind clusters found.",
			expectedK3dSection:  "---|k3d|---\nNo k3d clusters found.",
		},
		{
			name:                "kind clusters only",
			kindClusters:        []string{"kind-cluster"},
			k3dClusters:         []string{},
			expectedKindSection: "---|kind|---\nkind: kind-cluster",
			expectedK3dSection:  "---|k3d|---\nNo k3d clusters found.",
		},
		{
			name:                "k3d clusters only",
			kindClusters:        []string{},
			k3dClusters:         []string{"k3d-cluster"},
			expectedKindSection: "---|kind|---\nNo kind clusters found.",
			expectedK3dSection:  "---|k3d|---\nk3d: k3d-cluster",
		},
		{
			name:                "both distributions have clusters",
			kindClusters:        []string{"kind1", "kind2"},
			k3dClusters:         []string{"k3d1"},
			expectedKindSection: "---|kind|---\nkind: kind1, kind2",
			expectedK3dSection:  "---|k3d|---\nk3d: k3d1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testWithAllFlag(
				t,
				testCase.name,
				testCase.kindClusters,
				testCase.k3dClusters,
				testCase.expectedKindSection,
				testCase.expectedK3dSection,
			)
		})
	}
}

func testWithAllFlag(
	t *testing.T,
	_ string,
	kindClusters,
	k3dClusters []string,
	expectedKindSection,
	expectedK3dSection string,
) {
	t.Helper()

	var buf bytes.Buffer

	cmd := setupTestCommand(t, &buf)

	// Create fake factory and config manager
	factory := &fakeListFactoryForAll{
		kindClusters: kindClusters,
		k3dClusters:  k3dClusters,
	}
	cfgManager := setupTestConfig(t, cmd, true)
	deps := clusterpkg.ListDeps{
		Factory:             factory,
		DistributionFactory: factory,
	}

	err := clusterpkg.HandleListRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, expectedKindSection)
	assert.Contains(t, output, expectedK3dSection)
}

// setupTestCommand creates a test command with output buffer.
func setupTestCommand(t *testing.T, buf *bytes.Buffer) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "list"}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetContext(context.Background())

	return cmd
}

// setupTestConfig creates a test config manager with appropriate settings.
func setupTestConfig(
	t *testing.T,
	cmd *cobra.Command,
	allFlag bool,
) *ksailconfigmanager.ConfigManager {
	t.Helper()

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Create flags
	cmd.Flags().Bool("all", false, "List all clusters")

	// Bind the all flag
	_ = cfgManager.Viper.BindPFlag("all", cmd.Flags().Lookup("all"))

	// Set the all flag value if needed
	if allFlag {
		_ = cmd.Flags().Set("all", "true")
	}

	// Set minimal cluster config in viper
	cfgManager.Viper.Set("cluster.name", "test-cluster")
	cfgManager.Viper.Set("cluster.spec.distribution", "kind")

	return cfgManager
}

// fakeListFactory is a test double for the provisioner factory.
type fakeListFactory struct {
	clusters []string
}

func (f *fakeListFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	return &fakeListProvisioner{clusters: f.clusters}, nil, nil
}

// fakeListFactoryForAll is a test double that handles both distributions.
type fakeListFactoryForAll struct {
	kindClusters []string
	k3dClusters  []string
}

func (f *fakeListFactoryForAll) Create(
	_ context.Context,
	cluster *v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	if cluster.Spec.Distribution == v1alpha1.DistributionKind {
		return &fakeListProvisioner{clusters: f.kindClusters}, nil, nil
	}

	return &fakeListProvisioner{clusters: f.k3dClusters}, nil, nil
}

// fakeListProvisioner implements the ClusterProvisioner interface for testing.
type fakeListProvisioner struct {
	clusters []string
}

func (f *fakeListProvisioner) Create(context.Context, string) error { return nil }
func (f *fakeListProvisioner) Delete(context.Context, string) error { return nil }
func (f *fakeListProvisioner) Start(context.Context, string) error  { return nil }
func (f *fakeListProvisioner) Stop(context.Context, string) error   { return nil }

func (f *fakeListProvisioner) List(context.Context) ([]string, error) {
	return f.clusters, nil
}

func (f *fakeListProvisioner) Exists(context.Context, string) (bool, error) {
	return len(f.clusters) > 0, nil
}
