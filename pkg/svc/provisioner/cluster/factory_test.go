package clusterprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	k3dTypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestCreateClusterProvisioner_WithDistributionConfig(t *testing.T) {
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
			distribution: v1alpha1.DistributionKind,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{
					Name: "test-kind",
				},
			},
			expectedType: &kindprovisioner.KindClusterProvisioner{},
			expectError:  false,
		},
		{
			name:         "k3d with pre-loaded config",
			distribution: v1alpha1.DistributionK3d,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				K3d: &k3dv1alpha5.SimpleConfig{
					ObjectMeta: k3dTypes.ObjectMeta{
						Name: "test-k3d",
					},
				},
			},
			expectedType: &k3dprovisioner.K3dClusterProvisioner{},
			expectError:  false,
		},
		{
			name:         "talosindocker with pre-loaded config",
			distribution: v1alpha1.DistributionTalosInDocker,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				TalosInDocker: &talosconfigmanager.Configs{
					Name: "test-talos",
				},
			},
			expectedType: &talosindockerprovisioner.TalosInDockerProvisioner{},
			expectError:  false,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := clusterprovisioner.DefaultFactory{
				DistributionConfig: tt.distributionConfig,
			}
			cluster := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: tt.distribution,
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

			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, provisioner)
				if tt.errorIs != nil {
					assert.ErrorIs(t, err, tt.errorIs)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, provisioner)
				assert.IsType(t, tt.expectedType, provisioner)
			}
		})
	}
}

func TestCreateClusterProvisioner_NilCluster(t *testing.T) {
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

func TestCreateClusterProvisioner_MissingDistributionConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{}
	cluster := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKind,
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

func TestCreateClusterProvisioner_WrongDistributionConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		distribution       v1alpha1.Distribution
		distributionConfig *clusterprovisioner.DistributionConfig
	}{
		{
			name:         "kind requested but k3d config provided",
			distribution: v1alpha1.DistributionKind,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				K3d: &k3dv1alpha5.SimpleConfig{},
			},
		},
		{
			name:         "k3d requested but kind config provided",
			distribution: v1alpha1.DistributionK3d,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{},
			},
		},
		{
			name:         "talosindocker requested but kind config provided",
			distribution: v1alpha1.DistributionTalosInDocker,
			distributionConfig: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := clusterprovisioner.DefaultFactory{
				DistributionConfig: tt.distributionConfig,
			}
			cluster := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: tt.distribution,
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
				Distribution: v1alpha1.DistributionKind,
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
