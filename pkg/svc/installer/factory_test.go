package installer_test

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFactory(
	t *testing.T,
	distribution v1alpha1.Distribution,
) *installer.Factory {
	t.Helper()

	helmMock := helm.NewMockInterface(t)

	return installer.NewFactory(
		helmMock,
		nil, // dockerClient — nil is fine for factory creation logic tests
		"/tmp/kubeconfig",
		"test-context",
		5*time.Minute,
		distribution,
	)
}

func newTestFactoryWithDockerClient(
	t *testing.T,
	distribution v1alpha1.Distribution,
) *installer.Factory {
	t.Helper()

	helmMock := helm.NewMockInterface(t)
	dockerMock := docker.NewMockAPIClient(t)

	return installer.NewFactory(
		helmMock,
		dockerMock,
		"/tmp/kubeconfig",
		"test-context",
		5*time.Minute,
		distribution,
	)
}

func newTestCluster(
	opts ...func(*v1alpha1.ClusterSpec),
) *v1alpha1.Cluster {
	spec := v1alpha1.ClusterSpec{}
	for _, opt := range opts {
		opt(&spec)
	}

	return &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: spec,
		},
	}
}

// Tests for CreateInstallersForConfig.

func TestFactory_CreateInstallersForConfig_EmptyConfig(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionVanilla)
	cfg := newTestCluster()

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Empty(t, installers, "empty config should produce no installers")
}

func TestFactory_CreateInstallersForConfig_GitOpsEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		gitOpsEngine v1alpha1.GitOpsEngine
		expectedKey  string
	}{
		{
			name:         "flux_engine_creates_flux_installer",
			gitOpsEngine: v1alpha1.GitOpsEngineFlux,
			expectedKey:  "flux",
		},
		{
			name:         "argocd_engine_creates_argocd_installer",
			gitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
			expectedKey:  "argocd",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factory := newTestFactory(t, v1alpha1.DistributionVanilla)
			cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
				clusterSpec.GitOpsEngine = testCase.gitOpsEngine
			})

			installers := factory.CreateInstallersForConfig(cfg)

			assert.Contains(t, installers, testCase.expectedKey)
		})
	}
}

func TestFactory_CreateInstallersForConfig_NoGitOpsEngine(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionVanilla)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.GitOpsEngine = v1alpha1.GitOpsEngineNone
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.NotContains(t, installers, "flux")
	assert.NotContains(t, installers, "argocd")
}

func TestFactory_CreateInstallersForConfig_CNI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cni         v1alpha1.CNI
		expectedKey string
	}{
		{
			name:        "cilium_cni",
			cni:         v1alpha1.CNICilium,
			expectedKey: "cilium",
		},
		{
			name:        "calico_cni",
			cni:         v1alpha1.CNICalico,
			expectedKey: "calico",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factory := newTestFactory(t, v1alpha1.DistributionVanilla)
			cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
				clusterSpec.CNI = testCase.cni
			})

			installers := factory.CreateInstallersForConfig(cfg)

			assert.Contains(t, installers, testCase.expectedKey)
		})
	}
}

func TestFactory_CreateInstallersForConfig_DefaultCNI(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionVanilla)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.CNI = v1alpha1.CNIDefault
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.NotContains(t, installers, "cilium")
	assert.NotContains(t, installers, "calico")
}

func TestFactory_CreateInstallersForConfig_PolicyEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		engine      v1alpha1.PolicyEngine
		expectedKey string
	}{
		{
			name:        "kyverno_policy_engine",
			engine:      v1alpha1.PolicyEngineKyverno,
			expectedKey: "kyverno",
		},
		{
			name:        "gatekeeper_policy_engine",
			engine:      v1alpha1.PolicyEngineGatekeeper,
			expectedKey: "gatekeeper",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factory := newTestFactory(t, v1alpha1.DistributionVanilla)
			cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
				clusterSpec.PolicyEngine = testCase.engine
			})

			installers := factory.CreateInstallersForConfig(cfg)

			assert.Contains(t, installers, testCase.expectedKey)
		})
	}
}

func TestFactory_CreateInstallersForConfig_NoPolicyEngine(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionVanilla)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.PolicyEngine = v1alpha1.PolicyEngineNone
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.NotContains(t, installers, "kyverno")
	assert.NotContains(t, installers, "gatekeeper")
}

func TestFactory_CreateInstallersForConfig_CertManager(t *testing.T) {
	t.Parallel()

	t.Run("enabled_creates_installer", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionVanilla)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.CertManager = v1alpha1.CertManagerEnabled
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.Contains(t, installers, "cert-manager")
	})

	t.Run("disabled_no_installer", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionVanilla)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.CertManager = v1alpha1.CertManagerDisabled
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.NotContains(t, installers, "cert-manager")
	})
}

func TestFactory_CreateInstallersForConfig_MetricsServer(t *testing.T) {
	t.Parallel()

	t.Run("enabled_creates_installer", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionVanilla)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.MetricsServer = v1alpha1.MetricsServerEnabled
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.Contains(t, installers, "metrics-server")
	})

	t.Run("disabled_no_installer", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionVanilla)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.MetricsServer = v1alpha1.MetricsServerDisabled
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.NotContains(t, installers, "metrics-server")
	})

	t.Run("default_for_vanilla_creates_installer", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionVanilla)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.MetricsServer = v1alpha1.MetricsServerDefault
			clusterSpec.Distribution = v1alpha1.DistributionVanilla
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.Contains(t, installers, "metrics-server",
			"Vanilla does not provide metrics-server by default, so Default should install it")
	})

	t.Run("default_for_k3s_no_installer", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionK3s)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.MetricsServer = v1alpha1.MetricsServerDefault
			clusterSpec.Distribution = v1alpha1.DistributionK3s
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.NotContains(t, installers, "metrics-server",
			"K3s provides metrics-server by default, so Default should not install it")
	})
}

//nolint:funlen // Table-driven test with comprehensive test cases.
func TestFactory_CreateInstallersForConfig_CSI(t *testing.T) {
	t.Parallel()

	t.Run("csi_enabled_for_vanilla_creates_local_path_storage", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionVanilla)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.CSI = v1alpha1.CSIEnabled
			clusterSpec.Distribution = v1alpha1.DistributionVanilla
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.Contains(t, installers, "local-path-storage")
	})

	t.Run("csi_enabled_for_k3s_no_local_path_storage", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionK3s)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.CSI = v1alpha1.CSIEnabled
			clusterSpec.Distribution = v1alpha1.DistributionK3s
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.NotContains(t, installers, "local-path-storage",
			"K3s has built-in storage")
	})

	t.Run("talos_hetzner_creates_hetzner_csi_and_kubelet_csr_approver", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionTalos)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.CSI = v1alpha1.CSIEnabled
			clusterSpec.Distribution = v1alpha1.DistributionTalos
			clusterSpec.Provider = v1alpha1.ProviderHetzner
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.Contains(t, installers, "hetzner-csi")
		assert.Contains(t, installers, "kubelet-csr-approver")
		assert.NotContains(t, installers, "local-path-storage",
			"Talos on Hetzner uses Hetzner CSI, not local-path-storage")
	})

	t.Run("talos_hetzner_csi_disabled_no_csi_installers", func(t *testing.T) {
		t.Parallel()

		factory := newTestFactory(t, v1alpha1.DistributionTalos)
		cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
			clusterSpec.CSI = v1alpha1.CSIDisabled
			clusterSpec.Distribution = v1alpha1.DistributionTalos
			clusterSpec.Provider = v1alpha1.ProviderHetzner
		})

		installers := factory.CreateInstallersForConfig(cfg)

		assert.NotContains(t, installers, "hetzner-csi")
		assert.NotContains(t, installers, "local-path-storage")
	})
}

func TestFactory_CreateInstallersForConfig_LoadBalancer_VanillaDocker(t *testing.T) {
	t.Parallel()

	factory := newTestFactoryWithDockerClient(t, v1alpha1.DistributionVanilla)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.LoadBalancer = v1alpha1.LoadBalancerEnabled
		clusterSpec.Distribution = v1alpha1.DistributionVanilla
		clusterSpec.Provider = v1alpha1.ProviderDocker
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Contains(t, installers, "cloud-provider-kind")
	assert.NotContains(t, installers, "metallb",
		"MetalLB is for Talos, not Vanilla")
}

func TestFactory_CreateInstallersForConfig_LoadBalancer_TalosDocker(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionTalos)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.LoadBalancer = v1alpha1.LoadBalancerEnabled
		clusterSpec.Distribution = v1alpha1.DistributionTalos
		clusterSpec.Provider = v1alpha1.ProviderDocker
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Contains(t, installers, "metallb")
	assert.NotContains(t, installers, "cloud-provider-kind",
		"Cloud Provider KIND is for Vanilla, not Talos")
}

func TestFactory_CreateInstallersForConfig_LoadBalancer_TalosHetzner(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionTalos)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.LoadBalancer = v1alpha1.LoadBalancerEnabled
		clusterSpec.Distribution = v1alpha1.DistributionTalos
		clusterSpec.Provider = v1alpha1.ProviderHetzner
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Contains(t, installers, "hcloud-ccm",
		"Talos on Hetzner requires hcloud-ccm for LoadBalancer support")
	assert.NotContains(t, installers, "metallb",
		"Talos on Hetzner uses hcloud-ccm, not MetalLB")
	assert.NotContains(t, installers, "cloud-provider-kind")
}

func TestFactory_CreateInstallersForConfig_LoadBalancer_K3s(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionK3s)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.LoadBalancer = v1alpha1.LoadBalancerEnabled
		clusterSpec.Distribution = v1alpha1.DistributionK3s
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.NotContains(t, installers, "metallb",
		"K3s has built-in ServiceLB")
	assert.NotContains(t, installers, "cloud-provider-kind")
}

func TestFactory_CreateInstallersForConfig_LoadBalancer_Disabled(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionTalos)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.LoadBalancer = v1alpha1.LoadBalancerDisabled
		clusterSpec.Distribution = v1alpha1.DistributionTalos
		clusterSpec.Provider = v1alpha1.ProviderDocker
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.NotContains(t, installers, "metallb")
	assert.NotContains(t, installers, "cloud-provider-kind")
}

func TestFactory_CreateInstallersForConfig_MultipleComponents(t *testing.T) {
	t.Parallel()

	factory := newTestFactory(t, v1alpha1.DistributionVanilla)
	cfg := newTestCluster(func(clusterSpec *v1alpha1.ClusterSpec) {
		clusterSpec.GitOpsEngine = v1alpha1.GitOpsEngineFlux
		clusterSpec.CNI = v1alpha1.CNICilium
		clusterSpec.PolicyEngine = v1alpha1.PolicyEngineKyverno
		clusterSpec.CertManager = v1alpha1.CertManagerEnabled
		clusterSpec.MetricsServer = v1alpha1.MetricsServerEnabled
		clusterSpec.CSI = v1alpha1.CSIEnabled
		clusterSpec.Distribution = v1alpha1.DistributionVanilla
	})

	installers := factory.CreateInstallersForConfig(cfg)

	assert.Contains(t, installers, "flux")
	assert.Contains(t, installers, "cilium")
	assert.Contains(t, installers, "kyverno")
	assert.Contains(t, installers, "cert-manager")
	assert.Contains(t, installers, "metrics-server")
	assert.Contains(t, installers, "local-path-storage")
}

// Tests for GetImagesFromInstallers.

func TestGetImagesFromInstallers_EmptyMap(t *testing.T) {
	t.Parallel()

	images, err := installer.GetImagesFromInstallers(
		context.Background(),
		map[string]installer.Installer{},
	)

	require.NoError(t, err)
	assert.Empty(t, images)
}

func TestGetImagesFromInstallers_DeduplicatesImages(t *testing.T) {
	t.Parallel()

	mock1 := installer.NewMockInstaller(t)
	mock1.On("Images", context.Background()).
		Return([]string{"image-a:v1", "image-b:v1"}, nil)

	mock2 := installer.NewMockInstaller(t)
	mock2.On("Images", context.Background()).
		Return([]string{"image-b:v1", "image-c:v1"}, nil)

	installers := map[string]installer.Installer{
		"comp1": mock1,
		"comp2": mock2,
	}

	images, err := installer.GetImagesFromInstallers(context.Background(), installers)

	require.NoError(t, err)
	assert.Len(t, images, 3, "should deduplicate image-b:v1")
	assert.Contains(t, images, "image-a:v1")
	assert.Contains(t, images, "image-b:v1")
	assert.Contains(t, images, "image-c:v1")
}

func TestGetImagesFromInstallers_PropagatesError(t *testing.T) {
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
