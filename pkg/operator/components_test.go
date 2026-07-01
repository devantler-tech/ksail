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

// errBoom is a static sentinel for tests that exercise installer failure handling.
var errBoom = errors.New("boom")

// Component keys reused across the installer-ordering tests.
const (
	componentCilium        = "cilium"
	componentMetricsServer = "metrics-server"
	componentFlux          = "flux"
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
	// install is skipped (applied=false) without error (e.g. the Docker provider).
	applied, components, err := operator.InstallComponents(
		context.Background(),
		stubProvisioner{},
		clusterWithDistribution("c1", v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)
	assert.False(t, applied, "skipped install must report applied=false")
	assert.Empty(t, components, "a skipped install reports no per-component status")
}

func TestInstallComponents_KubeconfigNotReadyPropagates(t *testing.T) {
	t.Parallel()

	// When the child kubeconfig is not published yet the error propagates so the reconcile requeues;
	// the install never reaches Helm. A Connector exists, so applied is true.
	applied, components, err := operator.InstallComponents(
		context.Background(),
		connectorProvisioner{err: clustererr.ErrKubeconfigNotReady},
		clusterWithDistribution("c1", v1alpha1.DistributionVCluster),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
	assert.True(t, applied, "a Connector exists, so the attempt is reported as applied")
	assert.Empty(
		t,
		components,
		"install never reached the installer set, so no per-component status",
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
		componentFlux:          &recordingInstaller{name: componentFlux, order: &order},
		componentMetricsServer: &recordingInstaller{name: componentMetricsServer, order: &order},
		componentCilium:        &recordingInstaller{name: componentCilium, order: &order},
	}

	statuses, err := operator.RunInstallers(context.Background(), installers)
	require.NoError(t, err)
	assert.Equal(t, []string{componentCilium, componentMetricsServer, componentFlux}, order)
	// Per-component status mirrors the install order and reports every component Ready.
	assert.Equal(t, []v1alpha1.ComponentStatus{
		{Name: componentCilium, State: v1alpha1.ComponentStateReady},
		{Name: componentMetricsServer, State: v1alpha1.ComponentStateReady},
		{Name: componentFlux, State: v1alpha1.ComponentStateReady},
	}, statuses)
}

func TestRunInstallers_AggregatesErrorsAndContinues(t *testing.T) {
	t.Parallel()

	var order []string

	installers := map[string]installer.Installer{
		componentCilium: &recordingInstaller{name: componentCilium, err: errBoom, order: &order},
		componentFlux:   &recordingInstaller{name: componentFlux, order: &order},
	}

	statuses, err := operator.RunInstallers(context.Background(), installers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), componentCilium)
	// flux still ran despite cilium failing.
	assert.Equal(t, []string{componentCilium, componentFlux}, order)
	// The failed component is reported Failed with its error message; the survivor is Ready.
	assert.Equal(t, []v1alpha1.ComponentStatus{
		{Name: componentCilium, State: v1alpha1.ComponentStateFailed, Message: errBoom.Error()},
		{Name: componentFlux, State: v1alpha1.ComponentStateReady},
	}, statuses)
}
