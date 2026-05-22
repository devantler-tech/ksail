package operator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clusterWithProvider(distribution v1alpha1.Distribution, provider v1alpha1.Provider) *v1alpha1.Cluster {
	cluster := clusterWithDistribution("c1", distribution)
	cluster.Spec.Cluster.Provider = provider

	return cluster
}

func TestComponentsSupported(t *testing.T) {
	t.Parallel()

	assert.True(
		t,
		operator.ComponentsSupported(
			clusterWithProvider(v1alpha1.DistributionVCluster, v1alpha1.ProviderKubernetes),
		),
		"VCluster on the Kubernetes provider is supported",
	)
	assert.False(
		t,
		operator.ComponentsSupported(
			clusterWithProvider(v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker),
		),
		"VCluster on Docker (Vind) has no hub-published kubeconfig",
	)
	assert.False(
		t,
		operator.ComponentsSupported(
			clusterWithProvider(v1alpha1.DistributionVanilla, v1alpha1.ProviderKubernetes),
		),
		"non-VCluster distributions are not yet supported",
	)
}

// recordingInstaller records the order in which installers run and optionally fails.
type recordingInstaller struct {
	name  string
	err   error
	order *[]string
}

func (r *recordingInstaller) Install(_ context.Context) error {
	*r.order = append(*r.order, r.name)

	return r.err
}

func (r *recordingInstaller) Uninstall(_ context.Context) error { return nil }

func (r *recordingInstaller) Images(_ context.Context) ([]string, error) { return nil, nil }

func TestRunInstallers_OrdersCNIFirstAndGitOpsLast(t *testing.T) {
	t.Parallel()

	var order []string

	installers := map[string]installer.Installer{
		"flux":           &recordingInstaller{name: "flux", order: &order},
		"metrics-server": &recordingInstaller{name: "metrics-server", order: &order},
		"cilium":         &recordingInstaller{name: "cilium", order: &order},
	}

	err := operator.RunInstallers(context.Background(), installers)
	require.NoError(t, err)
	assert.Equal(t, []string{"cilium", "metrics-server", "flux"}, order)
}

func TestRunInstallers_AggregatesErrorsAndContinues(t *testing.T) {
	t.Parallel()

	var order []string

	boom := errors.New("boom")
	installers := map[string]installer.Installer{
		"cilium": &recordingInstaller{name: "cilium", err: boom, order: &order},
		"flux":   &recordingInstaller{name: "flux", order: &order},
	}

	err := operator.RunInstallers(context.Background(), installers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cilium")
	// flux still ran despite cilium failing.
	assert.Equal(t, []string{"cilium", "flux"}, order)
}
