package operator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvisioner satisfies clusterprovisioner.Provisioner without implementing Connector.
type stubProvisioner struct{}

func (stubProvisioner) Create(context.Context, string) error         { return nil }
func (stubProvisioner) Delete(context.Context, string) error         { return nil }
func (stubProvisioner) Start(context.Context, string) error          { return nil }
func (stubProvisioner) Stop(context.Context, string) error           { return nil }
func (stubProvisioner) List(context.Context) ([]string, error)       { return nil, nil }
func (stubProvisioner) Exists(context.Context, string) (bool, error) { return true, nil }

// connectorProvisioner adds the Connector capability to stubProvisioner.
type connectorProvisioner struct {
	stubProvisioner

	kubeconfig []byte
	err        error
}

func (c connectorProvisioner) Kubeconfig(context.Context, string) ([]byte, error) {
	return c.kubeconfig, c.err
}

func TestInstallComponents_NoConnectorIsNoOp(t *testing.T) {
	t.Parallel()

	// A provisioner without the Connector capability cannot expose the child cluster, so component
	// install is skipped without error (e.g. the Docker provider, which is unreachable from a hub).
	err := operator.InstallComponents(
		context.Background(),
		stubProvisioner{},
		clusterWithDistribution("c1", v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)
}

func TestInstallComponents_KubeconfigNotReadyPropagates(t *testing.T) {
	t.Parallel()

	// When the child kubeconfig is not published yet the error propagates so the reconcile requeues;
	// the install never reaches Helm.
	err := operator.InstallComponents(
		context.Background(),
		connectorProvisioner{err: clustererr.ErrKubeconfigNotReady},
		clusterWithDistribution("c1", v1alpha1.DistributionVCluster),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
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
