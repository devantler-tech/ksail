package installer_test

import (
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEKSFactory(t *testing.T, opts ...installer.Option) *installer.Factory {
	t.Helper()

	return installer.NewFactory(
		helm.NewMockInterface(t),
		nil,
		"/tmp/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionEKS,
		opts...,
	)
}

func newEKSLBCluster(optIn bool, loadBalancer v1alpha1.LoadBalancer) *v1alpha1.Cluster {
	return newTestCluster(func(spec *v1alpha1.ClusterSpec) {
		spec.Distribution = v1alpha1.DistributionEKS
		spec.Provider = v1alpha1.ProviderAWS
		spec.LoadBalancer = loadBalancer
		spec.EKS.ExperimentalAWSLoadBalancerController = optIn
	})
}

func TestFactory_CreateInstallersForConfig_AWSLBControllerOptIn(t *testing.T) {
	t.Parallel()

	factory := newEKSFactory(t, installer.WithEKSClusterName("prod-eks"))
	cfg := newEKSLBCluster(true, v1alpha1.LoadBalancerEnabled)

	installers, err := factory.CreateInstallersForConfig(cfg)
	require.NoError(t, err)

	assert.Contains(t, installers, "aws-load-balancer-controller",
		"the experimental opt-in with LoadBalancer Enabled must produce the installer")
}

func TestFactory_CreateInstallersForConfig_AWSLBControllerFlagOff(t *testing.T) {
	t.Parallel()

	factory := newEKSFactory(t, installer.WithEKSClusterName("prod-eks"))
	cfg := newEKSLBCluster(false, v1alpha1.LoadBalancerEnabled)

	installers, err := factory.CreateInstallersForConfig(cfg)
	require.NoError(t, err)

	assert.NotContains(t, installers, "aws-load-balancer-controller",
		"without the experimental opt-in EKS keeps its default in-tree path")
}

func TestFactory_CreateInstallersForConfig_AWSLBControllerLBDefault(t *testing.T) {
	t.Parallel()

	factory := newEKSFactory(t, installer.WithEKSClusterName("prod-eks"))
	cfg := newEKSLBCluster(true, v1alpha1.LoadBalancerDefault)

	installers, err := factory.CreateInstallersForConfig(cfg)
	require.NoError(t, err)

	assert.NotContains(t, installers, "aws-load-balancer-controller",
		"the opt-in requires LoadBalancer to be explicitly Enabled")
}

func TestFactory_CreateInstallersForConfig_AWSLBControllerMissingName(t *testing.T) {
	t.Parallel()

	factory := newEKSFactory(t) // no WithEKSClusterName
	cfg := newEKSLBCluster(true, v1alpha1.LoadBalancerEnabled)

	installers, err := factory.CreateInstallersForConfig(cfg)

	require.ErrorIs(t, err, awslbcontrollerinstaller.ErrClusterNameRequired,
		"an explicit opt-in with no resolvable cluster name must fail loud, not silently skip")
	assert.Nil(t, installers)
}
