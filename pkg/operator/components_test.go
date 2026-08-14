package operator_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// errBoom is a static sentinel for tests that exercise installer failure handling.
var errBoom = errors.New("boom")

// Component keys reused across the installer-ordering tests.
const (
	componentCilium        = "cilium"
	componentMetricsServer = "metrics-server"
	componentFlux          = "flux"
	componentAWSLB         = "aws-load-balancer-controller"
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

// recordingInstaller records the order in which installers run and optionally fails. Install
// failures use err; Uninstall failures use uninstallErr, and the uninstall order is recorded into
// uninstallOrder when set (nil disables uninstall recording).
type recordingInstaller struct {
	name           string
	err            error
	uninstallErr   error
	order          *[]string
	uninstallOrder *[]string
}

type ownershipReportingInstaller struct {
	gitOpsManaged bool
	identity      string
	identityOwned bool
}

func (*ownershipReportingInstaller) Install(context.Context) error   { return nil }
func (*ownershipReportingInstaller) Uninstall(context.Context) error { return nil }
func (*ownershipReportingInstaller) Images(context.Context) ([]string, error) {
	return nil, nil
}

func (i *ownershipReportingInstaller) IsGitOpsManaged(context.Context) (bool, error) {
	return i.gitOpsManaged, nil
}

func (i *ownershipReportingInstaller) ReleaseIdentity(context.Context) (string, error) {
	return i.identity, nil
}

func (i *ownershipReportingInstaller) OwnsReleaseIdentity(
	context.Context,
	string,
) (bool, error) {
	return i.identityOwned, nil
}

func (r *recordingInstaller) Install(_ context.Context) error {
	*r.order = append(*r.order, r.name)

	return r.err
}

func (r *recordingInstaller) Uninstall(_ context.Context) error {
	if r.uninstallOrder != nil {
		*r.uninstallOrder = append(*r.uninstallOrder, r.name)
	}

	return r.uninstallErr
}

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

// componentKyverno is reused across the uninstall/removal tests.
const componentKyverno = "kyverno"

func TestRunUninstallers_OrdersGitOpsFirstAndCNILast(t *testing.T) {
	t.Parallel()

	var order []string

	// Uninstall is the inverse of install: GitOps must come down first, CNI last.
	installers := map[string]installer.Installer{
		componentCilium: &recordingInstaller{name: componentCilium, uninstallOrder: &order},
		componentFlux:   &recordingInstaller{name: componentFlux, uninstallOrder: &order},
		componentKyverno: &recordingInstaller{
			name:           componentKyverno,
			uninstallOrder: &order,
		},
	}

	err := operator.RunUninstallers(context.Background(), installers)
	require.NoError(t, err)
	assert.Equal(t, []string{componentFlux, componentKyverno, componentCilium}, order)
}

func TestRunUninstallers_AggregatesErrorsAndContinues(t *testing.T) {
	t.Parallel()

	var order []string

	installers := map[string]installer.Installer{
		componentFlux: &recordingInstaller{
			name:           componentFlux,
			uninstallErr:   errBoom,
			uninstallOrder: &order,
		},
		componentCilium: &recordingInstaller{name: componentCilium, uninstallOrder: &order},
	}

	err := operator.RunUninstallers(context.Background(), installers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), componentFlux)
	// cilium still uninstalled despite flux failing, in reverse order.
	assert.Equal(t, []string{componentFlux, componentCilium}, order)
}

// newComponentFactory returns a factory builder backed by a bare helm mock — installer
// construction does not call helm, so no expectations are needed (mirrors the factory tests).
// It records each distribution it is invoked with into gotDistributions when non-nil, so tests
// can assert which distribution the baseline factory was built for.
func newComponentFactory(
	t *testing.T,
	gotDistributions *[]v1alpha1.Distribution,
) func(v1alpha1.Distribution) *installer.Factory {
	t.Helper()

	return func(distribution v1alpha1.Distribution) *installer.Factory {
		if gotDistributions != nil {
			*gotDistributions = append(*gotDistributions, distribution)
		}

		return installer.NewFactory(
			helm.NewMockInterface(t),
			nil,
			"/tmp/kubeconfig",
			"",
			5*time.Minute,
			distribution,
		)
	}
}

func TestRemovedComponentInstallers_ReturnsComponentsDroppedFromSpec(t *testing.T) {
	t.Parallel()

	newFactory := newComponentFactory(t, nil)

	// Baseline had Kyverno + Flux; the desired spec keeps only Flux (policyEngine flipped to None), so
	// Kyverno must be reported as removed and Flux must not.
	previous := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionVanilla,
		PolicyEngine: v1alpha1.PolicyEngineKyverno,
		GitOpsEngine: v1alpha1.GitOpsEngineFlux,
	}}}
	baseline, err := json.Marshal(previous.Spec)
	require.NoError(t, err)

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.LastAppliedComponentsAnnotation: string(baseline),
	}

	desired := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionVanilla,
		PolicyEngine: v1alpha1.PolicyEngineNone,
		GitOpsEngine: v1alpha1.GitOpsEngineFlux,
	}}}
	desiredInstallers, err := newFactory(v1alpha1.DistributionVanilla).
		CreateInstallersForConfig(desired)
	require.NoError(t, err)

	removed, err := operator.RemovedComponentInstallers(newFactory, cluster, desiredInstallers)
	require.NoError(t, err)
	assert.Contains(t, removed, componentKyverno, "a component dropped from the spec is removed")
	assert.NotContains(t, removed, componentFlux, "a still-desired component is not removed")
}

func TestRemovedComponentInstallers_OperatorOwnedAWSControllerUninstalls(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageMetadata(mock.Anything, componentAWSLB, "kube-system").
		Return(&helm.ReleaseStorageMetadata{
			Identity: "operator-release-uid",
			Labels: map[string]string{
				awslbcontrollerinstaller.ReleaseOwnershipLabel: "ksail",
			},
		}, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, componentAWSLB, "kube-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		UninstallRelease(mock.Anything, componentAWSLB, "kube-system").
		Return(nil)

	previous := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
		LoadBalancer: v1alpha1.LoadBalancerEnabled,
		EKS: v1alpha1.OptionsEKS{
			ExperimentalAWSLoadBalancerController: true,
		},
	}}}
	baseline, err := json.Marshal(previous.Spec)
	require.NoError(t, err)

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.LastAppliedComponentsAnnotation:                    string(baseline),
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation: "operator-release-uid",
	}
	newFactory := func(distribution v1alpha1.Distribution) *installer.Factory {
		return operator.NewInstallerFactory(
			client,
			"/tmp/kubeconfig",
			"operator-owned-eks",
			distribution,
		)
	}

	removed, err := operator.RemovedComponentInstallers(
		newFactory,
		cluster,
		map[string]installer.Installer{},
	)
	require.NoError(t, err)
	require.Contains(t, removed, componentAWSLB)
	require.NoError(t, operator.RunUninstallers(t.Context(), removed))
}

func TestRemovedComponentInstallers_PartialAWSControllerInstallUninstalls(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageMetadata(mock.Anything, componentAWSLB, "kube-system").
		Return(&helm.ReleaseStorageMetadata{
			Identity: "partial-install-release-uid",
			Labels: map[string]string{
				awslbcontrollerinstaller.ReleaseOwnershipLabel: "ksail",
			},
		}, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, componentAWSLB, "kube-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		UninstallRelease(mock.Anything, componentAWSLB, "kube-system").
		Return(nil)

	previous := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
		LoadBalancer: v1alpha1.LoadBalancerDisabled,
	}}}
	baseline, err := json.Marshal(previous.Spec)
	require.NoError(t, err)

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.LastAppliedComponentsAnnotation:                    string(baseline),
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation: "partial-install-release-uid",
	}
	newFactory := func(distribution v1alpha1.Distribution) *installer.Factory {
		return operator.NewInstallerFactory(
			client,
			"/tmp/kubeconfig",
			"partial-install-eks",
			distribution,
		)
	}

	removed, err := operator.RemovedComponentInstallers(
		newFactory,
		cluster,
		map[string]installer.Installer{},
	)
	require.NoError(t, err)
	require.Contains(t, removed, componentAWSLB)
	require.NoError(t, operator.RunUninstallers(t.Context(), removed))
}

func TestRecordAWSLoadBalancerControllerOwnershipUsesActualInstallOutcome(t *testing.T) {
	t.Parallel()

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation: "stale-identity",
	}

	err := operator.RecordAWSLoadBalancerControllerOwnership(
		t.Context(),
		cluster,
		map[string]installer.Installer{
			componentAWSLB: &ownershipReportingInstaller{gitOpsManaged: true},
		},
	)
	require.NoError(t, err)
	assert.NotContains(
		t,
		cluster.Annotations,
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation,
		"a GitOps-preserved release must not become operator-owned",
	)

	err = operator.RecordAWSLoadBalancerControllerOwnership(
		t.Context(),
		cluster,
		map[string]installer.Installer{
			componentAWSLB: &ownershipReportingInstaller{
				identity: "operator-release-uid", identityOwned: true,
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		"operator-release-uid",
		cluster.Annotations[v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation],
	)
}

func TestRecordAWSLoadBalancerControllerOwnershipRejectsManualRelease(t *testing.T) {
	t.Parallel()

	cluster := &v1alpha1.Cluster{}
	err := operator.RecordAWSLoadBalancerControllerOwnership(
		t.Context(),
		cluster,
		map[string]installer.Installer{
			componentAWSLB: &ownershipReportingInstaller{identity: "manual-release-uid"},
		},
	)

	require.Error(t, err)
	assert.NotContains(
		t,
		cluster.Annotations,
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation,
	)
}

func TestRecordAWSLoadBalancerControllerOwnershipSurvivesSiblingFailure(t *testing.T) {
	t.Parallel()

	cluster := &v1alpha1.Cluster{}
	err := operator.RecordAWSLoadBalancerControllerOwnershipAfterApply(
		t.Context(),
		cluster,
		map[string]installer.Installer{
			componentAWSLB: &ownershipReportingInstaller{
				identity: "partial-apply-release-uid", identityOwned: true,
			},
		},
		errBoom,
		errBoom,
	)

	require.NoError(t, err)
	assert.Equal(
		t,
		"partial-apply-release-uid",
		cluster.Annotations[v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation],
	)
}

func TestSanitizeForWriteDropsClientSuppliedControllerOwnership(t *testing.T) {
	t.Parallel()

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation: "forged-release-uid",
		"example.com/retained": "value",
	}

	sanitized := operator.SanitizeForWrite(cluster)

	assert.NotContains(
		t,
		sanitized.Annotations,
		v1alpha1.AWSLoadBalancerControllerReleaseIdentityAnnotation,
	)
	assert.Equal(t, "value", sanitized.Annotations["example.com/retained"])
}

func TestRemovedComponentInstallers_DoesNotInferAWSOwnershipFromDesiredBaseline(t *testing.T) {
	t.Parallel()

	previous := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
		LoadBalancer: v1alpha1.LoadBalancerEnabled,
		EKS: v1alpha1.OptionsEKS{
			ExperimentalAWSLoadBalancerController: true,
		},
	}}}
	baseline, err := json.Marshal(previous.Spec)
	require.NoError(t, err)

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.LastAppliedComponentsAnnotation: string(baseline),
	}
	client := helm.NewMockInterface(t)
	newFactory := func(distribution v1alpha1.Distribution) *installer.Factory {
		return operator.NewInstallerFactory(
			client,
			"/tmp/kubeconfig",
			"externally-owned-eks",
			distribution,
		)
	}

	removed, err := operator.RemovedComponentInstallers(
		newFactory,
		cluster,
		map[string]installer.Installer{},
	)
	require.NoError(t, err)
	require.Contains(t, removed, componentAWSLB)
	err = operator.RunUninstallers(t.Context(), removed)
	require.ErrorContains(t, err, "operator ownership evidence is required")
}

func TestRemovedComponentInstallers_BaselineFactoryUsesPreviousDistribution(t *testing.T) {
	t.Parallel()

	var gotDistributions []v1alpha1.Distribution

	newFactory := newComponentFactory(t, &gotDistributions)

	// The baseline was applied while the cluster ran K3s; the cluster has since been
	// switched to Vanilla. The previous installer set must be rebuilt with a factory
	// for K3s — several installers are distribution-dependent, so reusing the current
	// (Vanilla) factory would compute the wrong uninstall set.
	previous := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionK3s,
		GitOpsEngine: v1alpha1.GitOpsEngineFlux,
	}}}
	baseline, err := json.Marshal(previous.Spec)
	require.NoError(t, err)

	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.LastAppliedComponentsAnnotation: string(baseline),
	}

	_, err = operator.RemovedComponentInstallers(
		newFactory,
		cluster,
		map[string]installer.Installer{},
	)
	require.NoError(t, err)
	assert.Equal(t, []v1alpha1.Distribution{v1alpha1.DistributionK3s}, gotDistributions,
		"the baseline installer set must be built for the previously-applied distribution")
}

func TestRemovedComponentInstallers_NoBaselineReturnsNil(t *testing.T) {
	t.Parallel()

	newFactory := newComponentFactory(t, nil)

	// No baseline annotation (first reconcile): nothing is considered removed.
	removed, err := operator.RemovedComponentInstallers(
		newFactory,
		&v1alpha1.Cluster{},
		map[string]installer.Installer{},
	)
	require.NoError(t, err)
	assert.Empty(t, removed)
}

func TestRemovedComponentInstallers_UnparseableBaselineReturnsNil(t *testing.T) {
	t.Parallel()

	newFactory := newComponentFactory(t, nil)

	// A corrupt baseline must never block a fresh apply: it is treated as "no baseline".
	cluster := &v1alpha1.Cluster{}
	cluster.Annotations = map[string]string{
		v1alpha1.LastAppliedComponentsAnnotation: "{not valid json",
	}

	removed, err := operator.RemovedComponentInstallers(
		newFactory,
		cluster,
		map[string]installer.Installer{},
	)
	require.NoError(t, err)
	assert.Empty(t, removed)
}
