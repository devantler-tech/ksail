package setup_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Error variables for test cases.
var (
	errHelmClientFailed              = errors.New("helm client failed")
	errArgoCDInstallerCreationFailed = errors.New("argocd installer creation failed")
	errCSIInstallerCreationFailed    = errors.New("CSI installer creation failed")
	errCertManagerCreationFailed     = errors.New("cert-manager installer creation failed")
	errInstallFailed                 = errors.New("install failed")
	errCSIInstallFailed              = errors.New("CSI install failed")
	errCertManagerInstallFailed      = errors.New("cert-manager install failed")
	errFluxInstallFailed             = errors.New("flux install failed")
)

// mockInstaller implements installer.Installer for testing.
type mockInstaller struct {
	installErr   error
	uninstallErr error
}

func (m *mockInstaller) Install(_ context.Context) error {
	return m.installErr
}

func (m *mockInstaller) Uninstall(_ context.Context) error {
	return m.uninstallErr
}

func TestDefaultInstallerFactories(t *testing.T) {
	t.Parallel()

	factories := setup.DefaultInstallerFactories()

	assert.NotNil(t, factories, "DefaultInstallerFactories should return non-nil")
	assert.NotNil(t, factories.HelmClientFactory, "HelmClientFactory should not be nil")
	assert.NotNil(t, factories.Flux, "Flux factory should not be nil")
	assert.NotNil(t, factories.CertManager, "CertManager factory should not be nil")
	assert.NotNil(t, factories.CSI, "CSI factory should not be nil")
	assert.NotNil(t, factories.ArgoCD, "ArgoCD factory should not be nil")
}

func TestNeedsMetricsServerInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		metricsState v1alpha1.MetricsServer
		distribution v1alpha1.Distribution
		expected     bool
	}{
		{
			name:         "enabled with kind returns true",
			metricsState: v1alpha1.MetricsServerEnabled,
			distribution: v1alpha1.DistributionKind,
			expected:     true,
		},
		{
			name:         "enabled with k3d returns false (k3d provides by default)",
			metricsState: v1alpha1.MetricsServerEnabled,
			distribution: v1alpha1.DistributionK3d,
			expected:     false,
		},
		{
			name:         "disabled returns false",
			metricsState: v1alpha1.MetricsServerDisabled,
			distribution: v1alpha1.DistributionKind,
			expected:     false,
		},
		{
			name:         "default returns false",
			metricsState: v1alpha1.MetricsServerDefault,
			distribution: v1alpha1.DistributionKind,
			expected:     false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:  testCase.distribution,
						MetricsServer: testCase.metricsState,
					},
				},
			}

			result := setup.NeedsMetricsServerInstall(clusterCfg)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestInstallMetricsServerSilent_HelmClientError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		HelmClientFactory: func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, "", fmt.Errorf("helm error: %w", errHelmClientFailed)
		},
	}

	clusterCfg := &v1alpha1.Cluster{}

	err := setup.InstallMetricsServerSilent(context.Background(), clusterCfg, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client failed")
}

func TestInstallArgoCDSilent_NilFactory(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		ArgoCD: nil,
	}

	err := setup.InstallArgoCDSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrArgoCDInstallerFactoryNil)
}

func TestInstallArgoCDSilent_InstallerCreationError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		ArgoCD: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return nil, fmt.Errorf("argocd error: %w", errArgoCDInstallerCreationFailed)
		},
	}

	err := setup.InstallArgoCDSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "argocd installer creation failed")
}

func TestInstallFluxSilent_HelmClientError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		HelmClientFactory: func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, "", fmt.Errorf("helm error: %w", errHelmClientFailed)
		},
	}

	err := setup.InstallFluxSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client failed")
}

func TestInstallCSISilent_NilFactory(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		CSI: nil,
	}

	err := setup.InstallCSISilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

func TestInstallCSISilent_InstallerCreationError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		CSI: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return nil, fmt.Errorf("csi error: %w", errCSIInstallerCreationFailed)
		},
	}

	err := setup.InstallCSISilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CSI installer creation failed")
}

func TestInstallCertManagerSilent_NilFactory(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		CertManager: nil,
	}

	err := setup.InstallCertManagerSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCertManagerInstallerFactoryNil)
}

func TestInstallCertManagerSilent_InstallerCreationError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		CertManager: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return nil, fmt.Errorf("cert error: %w", errCertManagerCreationFailed)
		},
	}

	err := setup.InstallCertManagerSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cert-manager installer creation failed")
}

func TestEnsureArgoCDResources_NilConfig(t *testing.T) {
	t.Parallel()

	err := setup.EnsureArgoCDResources(context.Background(), "/kubeconfig", nil, "cluster")
	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrClusterConfigNil)
}

func TestErrorVariables(t *testing.T) {
	t.Parallel()

	t.Run("ErrCertManagerInstallerFactoryNil", func(t *testing.T) {
		t.Parallel()
		require.Error(t, setup.ErrCertManagerInstallerFactoryNil)
	})

	t.Run("ErrArgoCDInstallerFactoryNil", func(t *testing.T) {
		t.Parallel()
		require.Error(t, setup.ErrArgoCDInstallerFactoryNil)
	})

	t.Run("ErrClusterConfigNil", func(t *testing.T) {
		t.Parallel()
		require.Error(t, setup.ErrClusterConfigNil)
	})

	t.Run("ErrCSIInstallerFactoryNil", func(t *testing.T) {
		t.Parallel()
		require.Error(t, setup.ErrCSIInstallerFactoryNil)
	})
}

func TestInstallArgoCDSilent_InstallError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		ArgoCD: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return &mockInstaller{
				installErr: fmt.Errorf("argocd install: %w", errInstallFailed),
			}, nil
		},
	}

	err := setup.InstallArgoCDSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install failed")
}

func TestInstallCSISilent_InstallError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		CSI: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return &mockInstaller{
				installErr: fmt.Errorf("csi install: %w", errCSIInstallFailed),
			}, nil
		},
	}

	err := setup.InstallCSISilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CSI install failed")
}

func TestInstallCertManagerSilent_InstallError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		CertManager: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return &mockInstaller{
				installErr: fmt.Errorf("cert install: %w", errCertManagerInstallFailed),
			}, nil
		},
	}

	err := setup.InstallCertManagerSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cert-manager install failed")
}

func TestInstallFluxSilent_InstallError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		HelmClientFactory: func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, "", nil
		},
		Flux: func(_ helm.Interface, _ time.Duration) installer.Installer {
			return &mockInstaller{installErr: fmt.Errorf("flux install: %w", errFluxInstallFailed)}
		},
	}

	err := setup.InstallFluxSilent(context.Background(), &v1alpha1.Cluster{}, factories)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flux install failed")
}
