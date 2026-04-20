package clusterprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	k3dTypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

//nolint:funlen // table-driven test with multiple test cases
func TestCreateProvisioner_WithDistributionConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		distribution       v1alpha1.Distribution
		distributionConfig *clusterprovisioner.DistributionConfig
		expectedType       any
		expectError        bool
		errorIs            error
	}{
		{
			name:         "kind with pre-loaded config",
			distribution: v1alpha1.DistributionVanilla,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{
					Name: "test-kind",
				},
			},
			expectedType: &kindprovisioner.Provisioner{},
			expectError:  false,
		},
		{
			name:         "k3d with pre-loaded config",
			distribution: v1alpha1.DistributionK3s,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				K3d: &k3dv1alpha5.SimpleConfig{
					ObjectMeta: k3dTypes.ObjectMeta{
						Name: "test-k3d",
					},
				},
			},
			expectedType: &k3dprovisioner.Provisioner{},
			expectError:  false,
		},
		{
			name:         "talos with pre-loaded config",
			distribution: v1alpha1.DistributionTalos,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Talos: &talosconfigmanager.Configs{
					Name: "test-talos",
				},
			},
			expectedType: &talosprovisioner.Provisioner{},
			expectError:  false,
		},
		{
			name:         "eks with pre-loaded config",
			distribution: v1alpha1.DistributionEKS,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				EKS: &clusterprovisioner.EKSConfig{
					Name:       "test-eks",
					Region:     "us-east-1",
					ConfigPath: "/tmp/eksctl.yaml",
				},
			},
			expectedType: &eksprovisioner.Provisioner{},
			expectError:  false,
		},
		{
			name:               "eks without config returns error",
			distribution:       v1alpha1.DistributionEKS,
			distributionConfig: &clusterprovisioner.DistributionConfig{},
			expectError:        true,
			errorIs:            clusterprovisioner.ErrMissingDistributionConfig,
		},
		{
			name:         "unsupported distribution returns error",
			distribution: v1alpha1.Distribution("unknown"),
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{},
			},
			expectError: true,
			errorIs:     clusterprovisioner.ErrUnsupportedDistribution,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factory := clusterprovisioner.DefaultFactory{
				DistributionConfig: testCase.distributionConfig,
			}
			cluster := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: testCase.distribution,
						Connection: v1alpha1.Connection{
							Kubeconfig: "",
						},
					},
				},
			}

			provisioner, _, err := factory.Create(
				context.Background(),
				cluster,
			)

			if testCase.expectError {
				require.Error(t, err)
				require.Nil(t, provisioner)

				if testCase.errorIs != nil {
					require.ErrorIs(t, err, testCase.errorIs)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, provisioner)
				assert.IsType(t, testCase.expectedType, provisioner)
			}
		})
	}
}

func TestCreateProvisioner_NilCluster(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		},
	}
	provisioner, distributionConfig, err := factory.Create(
		context.Background(),
		nil,
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	require.Nil(t, distributionConfig)
	assert.ErrorIs(t, err, clusterprovisioner.ErrUnsupportedDistribution)
}

func TestCreateProvisioner_MissingDistributionConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{}
	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				Connection: v1alpha1.Connection{
					Kubeconfig: "",
				},
			},
		},
	}

	provisioner, _, err := factory.Create(
		context.Background(),
		cluster,
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	assert.ErrorIs(t, err, clusterprovisioner.ErrMissingDistributionConfig)
}

func TestCreateProvisioner_WrongDistributionConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		distribution       v1alpha1.Distribution
		distributionConfig *clusterprovisioner.DistributionConfig
	}{
		{
			name:         "kind requested but k3d config provided",
			distribution: v1alpha1.DistributionVanilla,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				K3d: &k3dv1alpha5.SimpleConfig{},
			},
		},
		{
			name:         "k3d requested but kind config provided",
			distribution: v1alpha1.DistributionK3s,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{},
			},
		},
		{
			name:         "talos requested but kind config provided",
			distribution: v1alpha1.DistributionTalos,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factory := clusterprovisioner.DefaultFactory{
				DistributionConfig: testCase.distributionConfig,
			}
			cluster := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: testCase.distribution,
						Connection: v1alpha1.Connection{
							Kubeconfig: "",
						},
					},
				},
			}

			provisioner, _, err := factory.Create(
				context.Background(),
				cluster,
			)

			require.Error(t, err)
			require.Nil(t, provisioner)
			assert.ErrorIs(t, err, clusterprovisioner.ErrMissingDistributionConfig)
		})
	}
}

func TestCreateKindProvisioner_DockerClientError(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{
				Name: "test-kind",
			},
		},
	}
	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				Connection: v1alpha1.Connection{
					Kubeconfig: "",
				},
			},
		},
	}

	provisioner, _, err := factory.Create(
		context.Background(),
		cluster,
	)

	require.Error(t, err)
	assert.Nil(t, provisioner)
	assert.Contains(t, err.Error(), "failed to create Docker client")
}

func TestCreateKindProvisioner_ImageVerificationPatchApplied(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://invalid")

	kindConfig := &v1alpha4.Cluster{
		Name: "test-kind",
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind: kindConfig,
		},
	}

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				Talos: v1alpha1.OptionsTalos{
					ImageVerification: v1alpha1.ImageVerificationEnabled,
				},
			},
		},
	}

	// factory.Create will fail on Docker client creation,
	// but image verification patches are applied before that.
	//nolint:dogsled // only testing side effects on kindConfig, return values irrelevant
	_, _, _ = factory.Create(context.Background(), cluster)

	assert.NotEmpty(t, kindConfig.ContainerdConfigPatches,
		"image verification containerd config patch should be applied")
	assert.Contains(t, kindConfig.ContainerdConfigPatches[0],
		`io.containerd.image-verifier.v1.bindir`)
}

func TestCreateKindProvisioner_ImageVerificationDisabledNoPatch(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://invalid")

	kindConfig := &v1alpha4.Cluster{
		Name: "test-kind",
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind: kindConfig,
		},
	}

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				Talos: v1alpha1.OptionsTalos{
					ImageVerification: v1alpha1.ImageVerificationDisabled,
				},
			},
		},
	}

	//nolint:dogsled // only testing side effects on kindConfig, return values irrelevant
	_, _, _ = factory.Create(context.Background(), cluster)

	assert.Empty(t, kindConfig.ContainerdConfigPatches,
		"no containerd config patch should be applied when image verification is disabled")
}

func TestCreateK3dProvisioner_ImageVerificationVolumeMountApplied(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://invalid")

	// Create the template file so the factory finds it
	templateDir := filepath.Join(t.TempDir(), k3dconfigmanager.DefaultImageVerifierDir)
	require.NoError(t, os.MkdirAll(templateDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(templateDir, "config.toml.tmpl"), []byte("test"), 0o600),
	)
	t.Chdir(t.TempDir())

	// Re-create structure in the test's working directory
	wdTemplateDir := filepath.Join(".", k3dconfigmanager.DefaultImageVerifierDir)
	require.NoError(t, os.MkdirAll(wdTemplateDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(wdTemplateDir, "config.toml.tmpl"), []byte("test"), 0o600),
	)

	k3dConfig := &k3dv1alpha5.SimpleConfig{
		ObjectMeta: k3dTypes.ObjectMeta{Name: "test-k3d"},
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			K3d: k3dConfig,
		},
	}

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				Talos: v1alpha1.OptionsTalos{
					ImageVerification: v1alpha1.ImageVerificationEnabled,
				},
			},
		},
	}

	// factory.Create may fail on k3d internals, but the volume mount is applied before that.
	//nolint:dogsled // only testing side effects on k3dConfig, return values irrelevant
	_, _, _ = factory.Create(context.Background(), cluster)

	found := false

	for _, vol := range k3dConfig.Volumes {
		if strings.Contains(vol.Volume, k3dconfigmanager.ContainerdConfigTemplatePath) {
			found = true

			assert.Equal(t, []string{"all"}, vol.NodeFilters)

			break
		}
	}

	assert.True(t, found,
		"image verification volume mount should be applied to K3d config")
}

func TestCreateK3dProvisioner_ImageVerificationMissingTemplate(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://invalid")
	t.Chdir(t.TempDir())

	k3dConfig := &k3dv1alpha5.SimpleConfig{
		ObjectMeta: k3dTypes.ObjectMeta{Name: "test-k3d"},
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			K3d: k3dConfig,
		},
	}

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				Talos: v1alpha1.OptionsTalos{
					ImageVerification: v1alpha1.ImageVerificationEnabled,
				},
			},
		},
	}

	_, _, err := factory.Create(context.Background(), cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image verification template not found")
}

func TestCreateK3dProvisioner_ImageVerificationDisabledNoVolumeMount(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://invalid")

	k3dConfig := &k3dv1alpha5.SimpleConfig{
		ObjectMeta: k3dTypes.ObjectMeta{Name: "test-k3d"},
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			K3d: k3dConfig,
		},
	}

	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				Talos: v1alpha1.OptionsTalos{
					ImageVerification: v1alpha1.ImageVerificationDisabled,
				},
			},
		},
	}

	//nolint:dogsled // only testing side effects on k3dConfig, return values irrelevant
	_, _, _ = factory.Create(context.Background(), cluster)

	for _, vol := range k3dConfig.Volumes {
		assert.NotContains(
			t,
			vol.Volume,
			k3dconfigmanager.ContainerdConfigTemplatePath,
			"no volume mount for containerd config template should be present when image verification is disabled",
		)
	}
}
