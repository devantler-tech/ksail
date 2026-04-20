package installer_test

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFactory_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	factory := installer.NewFactory(
		helmMock,
		nil,
		"/some/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	require.NotNil(t, factory)
}

func TestNewFactory_NilClients(t *testing.T) {
	t.Parallel()

	factory := installer.NewFactory(nil, nil, "", "", 0, "")

	require.NotNil(t, factory, "factory should be created even with all nil/zero params")
}

func TestNewFactory_WithDockerClient(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	dockerMock := docker.NewMockAPIClient(t)

	factory := installer.NewFactory(
		helmMock,
		dockerMock,
		"/some/kubeconfig",
		"test-context",
		10*time.Minute,
		v1alpha1.DistributionTalos,
	)

	require.NotNil(t, factory)
}

func TestGetImagesForCluster_EmptyConfig(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	factory := installer.NewFactory(
		helmMock,
		nil,
		"/some/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{},
		},
	}

	images, err := factory.GetImagesForCluster(context.Background(), cfg)

	require.NoError(t, err)
	assert.Empty(t, images, "empty config should produce no images")
}

func TestGetImagesFromInstallers_NilMap(t *testing.T) {
	t.Parallel()

	images, err := installer.GetImagesFromInstallers(context.Background(), nil)

	require.NoError(t, err)
	assert.Empty(t, images)
}

func TestGetImagesFromInstallers_SingleInstaller(t *testing.T) {
	t.Parallel()

	mockInst := installer.NewMockInstaller(t)
	mockInst.On("Images", context.Background()).
		Return([]string{"image1:v1", "image2:v1"}, nil)

	installers := map[string]installer.Installer{
		"comp1": mockInst,
	}

	images, err := installer.GetImagesFromInstallers(context.Background(), installers)

	require.NoError(t, err)
	assert.Len(t, images, 2)
	assert.Contains(t, images, "image1:v1")
	assert.Contains(t, images, "image2:v1")
}

func TestGetImagesFromInstallers_ErrorFromInstaller(t *testing.T) {
	t.Parallel()

	mockInst := installer.NewMockInstaller(t)
	mockInst.On("Images", context.Background()).
		Return(nil, assert.AnError)

	installers := map[string]installer.Installer{
		"failing": mockInst,
	}

	_, err := installer.GetImagesFromInstallers(context.Background(), installers)

	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestFactory_LoadBalancer_VanillaDocker_NilDockerClient(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	factory := installer.NewFactory(
		helmMock,
		nil, // nil docker client
		"/some/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
	)

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
				Distribution: v1alpha1.DistributionVanilla,
				Provider:     v1alpha1.ProviderDocker,
			},
		},
	}

	installers := factory.CreateInstallersForConfig(cfg)

	assert.NotContains(t, installers, "cloud-provider-kind",
		"cloud-provider-kind requires a non-nil docker client")
}

func TestFactory_LoadBalancer_Default_TalosHetzner(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	factory := installer.NewFactory(
		helmMock,
		nil,
		"/some/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionTalos,
	)

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				LoadBalancer: v1alpha1.LoadBalancerDefault,
				Distribution: v1alpha1.DistributionTalos,
				Provider:     v1alpha1.ProviderHetzner,
			},
		},
	}

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Contains(t, installers, "hcloud-ccm",
		"Talos on Hetzner with Default LoadBalancer should install hcloud-ccm")
}

func TestFactory_CSI_TalosDocker(t *testing.T) {
	t.Parallel()

	helmMock := helm.NewMockInterface(t)
	factory := installer.NewFactory(
		helmMock,
		nil,
		"/some/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionTalos,
	)

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				CSI:          v1alpha1.CSIEnabled,
				Distribution: v1alpha1.DistributionTalos,
				Provider:     v1alpha1.ProviderDocker,
			},
		},
	}

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Contains(t, installers, "local-path-storage",
		"Talos on Docker with CSI enabled should use local-path-storage")
	assert.NotContains(t, installers, "hetzner-csi")
}

func TestInstallTimeoutConstants_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("flux_install_timeout", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 12*time.Minute, installer.FluxInstallTimeout)
	})

	t.Run("gatekeeper_install_timeout", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 7*time.Minute, installer.GatekeeperInstallTimeout)
	})

	t.Run("cert_manager_install_timeout", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 10*time.Minute, installer.CertManagerInstallTimeout)
	})
}
