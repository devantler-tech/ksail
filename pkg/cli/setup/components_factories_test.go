package setup_test

import (
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTestHelmFactory = errors.New("test helm factory error")

const testKubeconfig = "kubeconfig"

// TestPolicyEngineFactory exercises the policy engine factory returned by
// DefaultInstallerFactories. It tests engine selection (Kyverno, Gatekeeper),
// the disabled path, unknown-engine fallback, and helm client errors.
//
//nolint:funlen // table-driven test with many cases
func TestPolicyEngineFactory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		engine        v1alpha1.PolicyEngine
		helmErr       error
		expectErr     bool
		expectErrIs   error
		expectNilInst bool
	}{
		{
			name:          "PolicyEngineNone returns ErrPolicyEngineDisabled",
			engine:        v1alpha1.PolicyEngineNone,
			expectErr:     true,
			expectErrIs:   setup.ErrPolicyEngineDisabled,
			expectNilInst: true,
		},
		{
			name:          "empty engine returns ErrPolicyEngineDisabled",
			engine:        "",
			expectErr:     true,
			expectErrIs:   setup.ErrPolicyEngineDisabled,
			expectNilInst: true,
		},
		{
			name:      "Kyverno creates installer",
			engine:    v1alpha1.PolicyEngineKyverno,
			expectErr: false,
		},
		{
			name:      "Gatekeeper creates installer",
			engine:    v1alpha1.PolicyEngineGatekeeper,
			expectErr: false,
		},
		{
			name:          "unknown engine returns error",
			engine:        v1alpha1.PolicyEngine("unknown"),
			expectErr:     true,
			expectNilInst: true,
		},
		{
			name:          "helm client error propagates",
			engine:        v1alpha1.PolicyEngineKyverno,
			helmErr:       errTestHelmFactory,
			expectErr:     true,
			expectNilInst: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factories := setup.DefaultInstallerFactories()
			if testCase.helmErr != nil {
				factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
					return nil, "", testCase.helmErr
				}
			} else {
				factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
					return nil, testKubeconfig, nil
				}
			}

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						PolicyEngine: testCase.engine,
					},
				},
			}

			inst, err := factories.PolicyEngine(clusterCfg)

			if testCase.expectErr {
				require.Error(t, err)

				if testCase.expectErrIs != nil {
					require.ErrorIs(t, err, testCase.expectErrIs)
				}
			} else {
				require.NoError(t, err)
			}

			if testCase.expectNilInst {
				assert.Nil(t, inst)
			} else {
				assert.NotNil(t, inst)
			}
		})
	}
}

// TestCSIFactory exercises the CSI factory returned by DefaultInstallerFactories.
// It tests the Talos×Hetzner special case and the default local-path-provisioner path.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestCSIFactory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		helmErr      error
		expectErr    bool
	}{
		{
			name:         "Talos+Hetzner creates Hetzner CSI",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderHetzner,
			expectErr:    false,
		},
		{
			name:         "Vanilla+Docker creates local-path-provisioner",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			expectErr:    false,
		},
		{
			name:         "Talos+Docker creates local-path-provisioner",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			expectErr:    false,
		},
		{
			name:         "helm client error propagates",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			helmErr:      errTestHelmFactory,
			expectErr:    true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factories := setup.DefaultInstallerFactories()
			if testCase.helmErr != nil {
				factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
					return nil, "", testCase.helmErr
				}
			} else {
				factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
					return nil, testKubeconfig, nil
				}
			}

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: testCase.distribution,
						Provider:     testCase.provider,
					},
				},
			}

			inst, err := factories.CSI(clusterCfg)

			if testCase.expectErr {
				require.Error(t, err)
				assert.Nil(t, inst)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, inst)
			}
		})
	}
}

// TestArgoCDInstallerFactory exercises the ArgoCD factory, including SOPS and helm errors.
func TestArgoCDInstallerFactory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		helmErr   error
		expectErr bool
	}{
		{
			name:      "creates installer with default SOPS",
			expectErr: false,
		},
		{
			name:      "helm client error propagates",
			helmErr:   errTestHelmFactory,
			expectErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factories := setup.DefaultInstallerFactories()
			if testCase.helmErr != nil {
				factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
					return nil, "", testCase.helmErr
				}
			} else {
				factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
					return nil, testKubeconfig, nil
				}
			}

			inst, err := factories.ArgoCD(&v1alpha1.Cluster{})

			if testCase.expectErr {
				require.Error(t, err)
				assert.Nil(t, inst)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, inst)
			}
		})
	}
}

// TestHelmInstallerFactory_CertManager exercises the generic helmInstallerFactory
// via the CertManager factory.
//
//nolint:dupl // Duplicated setup keeps the parallel test cases readable.
func TestHelmInstallerFactory_CertManager(t *testing.T) {
	t.Parallel()

	t.Run("creates installer on success", func(t *testing.T) {
		t.Parallel()

		factories := setup.DefaultInstallerFactories()
		factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, testKubeconfig, nil
		}

		inst, err := factories.CertManager(&v1alpha1.Cluster{})
		require.NoError(t, err)
		assert.NotNil(t, inst)
	})

	t.Run("helm error propagates", func(t *testing.T) {
		t.Parallel()

		factories := setup.DefaultInstallerFactories()
		factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, "", errTestHelmFactory
		}

		inst, err := factories.CertManager(&v1alpha1.Cluster{})
		require.Error(t, err)
		assert.Nil(t, inst)
	})
}

// TestHelmInstallerFactory_KubeletCSRApprover exercises the KubeletCSRApprover factory.
//
//nolint:dupl // Duplicated setup keeps the parallel test cases readable.
func TestHelmInstallerFactory_KubeletCSRApprover(t *testing.T) {
	t.Parallel()

	t.Run("creates installer on success", func(t *testing.T) {
		t.Parallel()

		factories := setup.DefaultInstallerFactories()
		factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, testKubeconfig, nil
		}

		inst, err := factories.KubeletCSRApprover(&v1alpha1.Cluster{})
		require.NoError(t, err)
		assert.NotNil(t, inst)
	})

	t.Run("helm error propagates", func(t *testing.T) {
		t.Parallel()

		factories := setup.DefaultInstallerFactories()
		factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
			return nil, "", errTestHelmFactory
		}

		inst, err := factories.KubeletCSRApprover(&v1alpha1.Cluster{})
		require.Error(t, err)
		assert.Nil(t, inst)
	})
}

// TestFluxFactory exercises the Flux factory function directly.
func TestFluxFactory(t *testing.T) {
	t.Parallel()

	factories := setup.DefaultInstallerFactories()
	inst := factories.Flux(nil, 5*time.Minute)

	assert.NotNil(t, inst)
}

// TestInstallerFactories_AllFieldsPopulated verifies every factory field is set.
func TestInstallerFactories_AllFieldsPopulated(t *testing.T) {
	t.Parallel()

	factories := setup.DefaultInstallerFactories()

	assert.NotNil(t, factories.Flux, "Flux")
	assert.NotNil(t, factories.CertManager, "CertManager")
	assert.NotNil(t, factories.CSI, "CSI")
	assert.NotNil(t, factories.PolicyEngine, "PolicyEngine")
	assert.NotNil(t, factories.ArgoCD, "ArgoCD")
	assert.NotNil(t, factories.KubeletCSRApprover, "KubeletCSRApprover")
	assert.NotNil(t, factories.EnsureArgoCDResources, "EnsureArgoCDResources")
	assert.NotNil(t, factories.EnsureFluxResources, "EnsureFluxResources")
	assert.NotNil(t, factories.SetupFluxInstance, "SetupFluxInstance")
	assert.NotNil(t, factories.WaitForFluxReady, "WaitForFluxReady")
	assert.NotNil(t, factories.HelmClientFactory, "HelmClientFactory")
}

// TestInstallPolicyEngineSilent_NilFactory verifies nil factory error.
func TestInstallPolicyEngineSilent_NilFactory(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		PolicyEngine: nil,
	}

	err := setup.InstallPolicyEngineSilent(
		t.Context(),
		&v1alpha1.Cluster{},
		factories,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrPolicyEngineInstallerFactoryNil)
}

// TestInstallPolicyEngineSilent_DisabledEngine verifies disabled engine propagates as error.
func TestInstallPolicyEngineSilent_DisabledEngine(t *testing.T) {
	t.Parallel()

	factories := setup.DefaultInstallerFactories()
	factories.HelmClientFactory = func(_ *v1alpha1.Cluster) (*helm.Client, string, error) {
		return nil, testKubeconfig, nil
	}

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				PolicyEngine: v1alpha1.PolicyEngineNone,
			},
		},
	}

	err := setup.InstallPolicyEngineSilent(t.Context(), clusterCfg, factories)
	// The factory returns ErrPolicyEngineDisabled which is wrapped
	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrPolicyEngineDisabled)
}

// TestInstallPolicyEngineSilent_InstallError verifies install error propagation.
func TestInstallPolicyEngineSilent_InstallError(t *testing.T) {
	t.Parallel()

	factories := &setup.InstallerFactories{
		PolicyEngine: func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return &mockInstaller{installErr: errInstallFailed}, nil
		},
	}

	err := setup.InstallPolicyEngineSilent(
		t.Context(),
		&v1alpha1.Cluster{},
		factories,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "install failed")
}
