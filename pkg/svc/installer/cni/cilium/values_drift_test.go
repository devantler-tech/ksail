package ciliuminstaller_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errReleaseUnreadable = errors.New("release storage is unreadable")

// driftCase names the inputs Cilium's rendered values depend on.
type driftCase struct {
	distribution v1alpha1.Distribution
	loadBalancer v1alpha1.LoadBalancer
	haEnabled    bool
}

func newDriftInstaller(client helm.Interface, input driftCase) *ciliuminstaller.Installer {
	return ciliuminstaller.NewInstaller(
		client, "/path/to/kubeconfig", "test-context", 5*time.Minute,
		input.distribution, v1alpha1.ProviderDocker, input.loadBalancer, input.haEnabled,
	)
}

// storedRenderedValues returns the values an installer renders as Helm stores
// and returns them: persisted as JSON, so numbers read back as float64.
func storedRenderedValues(t *testing.T, installer *ciliuminstaller.Installer) map[string]any {
	t.Helper()

	rendered, err := installer.RenderedValues()
	require.NoError(t, err)

	raw, err := json.Marshal(rendered)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))

	return stored
}

func expectDeployedRelease(client *helm.MockInterface, values map[string]any) {
	client.EXPECT().
		ReleaseExists(mock.Anything, "cilium", "kube-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "cilium", "kube-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "cilium", "kube-system").
		Return(values, nil)
}

func vanillaWithoutLoadBalancer() driftCase {
	return driftCase{
		distribution: v1alpha1.DistributionVanilla,
		loadBalancer: v1alpha1.LoadBalancerDisabled,
	}
}

// TestValuesDrifted_DetectsAReleaseWithOtherRenderedValues replays ksail#7722:
// a release installed with other values than the ones this KSail renders must
// report drift, so `cluster update` upgrades it even though spec.cluster.cni
// did not change. Each case differs from the installed release in one input
// the values are rendered from.
func TestValuesDrifted_DetectsAReleaseWithOtherRenderedValues(t *testing.T) {
	t.Parallel()

	for name, deployed := range map[string]driftCase{
		"operator replicas": {
			distribution: v1alpha1.DistributionVanilla,
			loadBalancer: v1alpha1.LoadBalancerDisabled,
			haEnabled:    true,
		},
		"host network for the gateway": {
			distribution: v1alpha1.DistributionVanilla,
			loadBalancer: v1alpha1.LoadBalancerEnabled,
		},
		"distribution settings": {
			distribution: v1alpha1.DistributionTalos,
			loadBalancer: v1alpha1.LoadBalancerDisabled,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			installer := newDriftInstaller(client, vanillaWithoutLoadBalancer())

			expectDeployedRelease(client, storedRenderedValues(
				t, newDriftInstaller(helm.NewMockInterface(t), deployed),
			))

			drifted, err := installer.ValuesDrifted(context.Background())
			require.NoError(t, err)
			assert.True(t, drifted, "a release with other values must report drift")
		})
	}
}

// TestValuesDrifted_StaysSilentWhenTheReleaseMatches is the control: a release
// that carries exactly the rendered values, including the numbers and lists
// Helm stores with another type, is not drift.
func TestValuesDrifted_StaysSilentWhenTheReleaseMatches(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]driftCase{
		"vanilla": vanillaWithoutLoadBalancer(),
		"vanilla with a load balancer and high availability": {
			distribution: v1alpha1.DistributionVanilla,
			loadBalancer: v1alpha1.LoadBalancerEnabled,
			haEnabled:    true,
		},
		"talos": {
			distribution: v1alpha1.DistributionTalos,
			loadBalancer: v1alpha1.LoadBalancerDisabled,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			installer := newDriftInstaller(client, input)

			expectDeployedRelease(client, storedRenderedValues(t, installer))

			drifted, err := installer.ValuesDrifted(context.Background())
			require.NoError(t, err)
			assert.False(t, drifted, "matching values must not report drift")
		})
	}
}

// TestValuesDrifted_LeavesAGitOpsOwnedReleaseAlone never reports drift for a
// release a GitOps controller owns: Install skips it, so the reported upgrade
// would never happen.
func TestValuesDrifted_LeavesAGitOpsOwnedReleaseAlone(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, vanillaWithoutLoadBalancer())

	client.EXPECT().
		ReleaseExists(mock.Anything, "cilium", "kube-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "cilium", "kube-system").
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": "cilium"}, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_LeavesAMissingReleaseAlone leaves installing a release that
// does not exist to the cluster.cni field.
func TestValuesDrifted_LeavesAMissingReleaseAlone(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, vanillaWithoutLoadBalancer())

	client.EXPECT().
		ReleaseExists(mock.Anything, "cilium", "kube-system").
		Return(false, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_ReportsAnUnreadableRelease never answers "no drift" for a
// release it could not read.
func TestValuesDrifted_ReportsAnUnreadableRelease(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, vanillaWithoutLoadBalancer())

	client.EXPECT().
		ReleaseExists(mock.Anything, "cilium", "kube-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "cilium", "kube-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "cilium", "kube-system").
		Return(nil, errReleaseUnreadable)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.ErrorIs(t, err, errReleaseUnreadable)
	assert.False(t, drifted)
}

// TestValuesDrifted_ReportsAMissingHelmClient surfaces an installer that has
// no client instead of reading it as a clean release.
func TestValuesDrifted_ReportsAMissingHelmClient(t *testing.T) {
	t.Parallel()

	installer := newDriftInstaller(nil, vanillaWithoutLoadBalancer())

	drifted, err := installer.ValuesDrifted(context.Background())
	require.Error(t, err)
	assert.False(t, drifted)

	_, err = installer.RenderedValues()
	require.Error(t, err)
}
