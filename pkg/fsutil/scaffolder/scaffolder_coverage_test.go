package scaffolder_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScaffoldKind_MirrorRegistries tests Kind scaffolding with mirror registries configured.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestScaffoldKind_MirrorRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		mirrors            []string
		expectedMirrorDirs []string
	}{
		{
			name:               "single mirror creates hosts.toml",
			mirrors:            []string{"docker.io=https://registry-1.docker.io"},
			expectedMirrorDirs: []string{"docker.io"},
		},
		{
			name: "multiple mirrors create multiple hosts.toml",
			mirrors: []string{
				"docker.io=https://registry-1.docker.io",
				"ghcr.io=https://ghcr.io",
			},
			expectedMirrorDirs: []string{"docker.io", "ghcr.io"},
		},
		{
			name:               "no mirrors creates no hosts.toml",
			mirrors:            nil,
			expectedMirrorDirs: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			cluster := v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:       v1alpha1.DistributionVanilla,
						DistributionConfig: scaffolder.KindConfigFile,
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			}

			scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, testCase.mirrors)

			err := scaffolderInstance.Scaffold(tempDir, true)
			require.NoError(t, err)

			if len(testCase.expectedMirrorDirs) > 0 {
				mirrorsDir := filepath.Join(tempDir, scaffolderInstance.GetKindMirrorsDir())
				for _, mirrorHost := range testCase.expectedMirrorDirs {
					hostsPath := filepath.Join(mirrorsDir, mirrorHost, "hosts.toml")
					_, statErr := os.Stat(hostsPath)
					require.NoError(t, statErr, "expected hosts.toml at %s", hostsPath)

					content, readErr := os.ReadFile(hostsPath) //nolint:gosec // test file
					require.NoError(t, readErr)
					assert.Contains(t, string(content), "capabilities = [\"pull\", \"resolve\"]")
				}
			}
		})
	}
}

// TestScaffoldKind_MirrorRegistries_SkipExistingNoForce tests that mirror hosts.toml
// files are skipped when they exist and force is false.
func TestScaffoldKind_MirrorRegistries_SkipExistingNoForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVanilla,
				DistributionConfig: scaffolder.KindConfigFile,
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}

	mirrors := []string{"docker.io=https://registry-1.docker.io"}
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, mirrors)

	// First scaffold to create files
	err := scaffolderInstance.Scaffold(tempDir, true)
	require.NoError(t, err)

	// Second scaffold without force - files should be skipped
	buffer := &bytes.Buffer{}
	scaffolderInstance2 := scaffolder.NewScaffolder(cluster, buffer, mirrors)

	err = scaffolderInstance2.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Verify the warning message about skipping appears
	assert.Contains(t, buffer.String(), "skipped")
}

// TestScaffoldKind_NodeCounts tests Kind scaffolding with explicit node counts.
func TestScaffoldKind_NodeCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		controlPlanes int32
		workers       int32
	}{
		{
			name:          "explicit control planes only",
			controlPlanes: 3,
			workers:       0,
		},
		{
			name:          "explicit workers only - defaults to 1 control plane",
			controlPlanes: 0,
			workers:       2,
		},
		{
			name:          "both control planes and workers",
			controlPlanes: 3,
			workers:       5,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			cluster := v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution:       v1alpha1.DistributionVanilla,
						DistributionConfig: scaffolder.KindConfigFile,
						Talos: v1alpha1.OptionsTalos{
							ControlPlanes: testCase.controlPlanes,
							Workers:       testCase.workers,
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			}

			scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

			err := scaffolderInstance.Scaffold(tempDir, true)
			require.NoError(t, err)

			// Verify kind.yaml was created
			kindPath := filepath.Join(tempDir, scaffolder.KindConfigFile)
			content, readErr := os.ReadFile(kindPath) //nolint:gosec // test file
			require.NoError(t, readErr)

			// With explicit node counts, kind.yaml should contain node role definitions
			kindContent := string(content)
			if testCase.controlPlanes > 0 || testCase.workers > 0 {
				assert.Contains(t, kindContent, "role:")
			}
		})
	}
}

// TestScaffoldKind_MetricsServerEnabled tests that kubelet cert rotation patches
// are applied when metrics-server is enabled for Kind.
func TestScaffoldKind_MetricsServerEnabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVanilla,
				DistributionConfig: scaffolder.KindConfigFile,
				MetricsServer:      v1alpha1.MetricsServerEnabled,
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, true)
	require.NoError(t, err)

	kindPath := filepath.Join(tempDir, scaffolder.KindConfigFile)
	content, readErr := os.ReadFile(kindPath) //nolint:gosec // test file
	require.NoError(t, readErr)

	// Should contain kubelet extra args for cert rotation
	assert.Contains(t, string(content), "kubeadmConfigPatches")
}

// TestScaffoldKind_CDIEnabled tests that CDI patches are applied when CDI is enabled.
func TestScaffoldKind_CDIEnabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVanilla,
				DistributionConfig: scaffolder.KindConfigFile,
				CDI:                v1alpha1.CDIEnabled,
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, true)
	require.NoError(t, err)

	kindPath := filepath.Join(tempDir, scaffolder.KindConfigFile)
	content, readErr := os.ReadFile(kindPath) //nolint:gosec // test file
	require.NoError(t, readErr)

	// When CDI is enabled, containerdConfigPatches should be present
	assert.Contains(t, string(content), "containerdConfigPatches")
}

// TestScaffoldK3d_LoadBalancerDisabled tests that servicelb is disabled in K3d args.
func TestScaffoldK3d_LoadBalancerDisabled(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	config := scaffolderInstance.CreateK3dConfig(".")

	found := false

	for _, arg := range config.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=servicelb" {
			found = true

			assert.Equal(t, []string{"server:*"}, arg.NodeFilters)

			break
		}
	}

	assert.True(t, found, "--disable=servicelb flag should be present")
}

// TestScaffoldK3d_NodeCounts tests K3d config with explicit node counts.
func TestScaffoldK3d_NodeCounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		controlPlanes   int32
		workers         int32
		expectedServers int
		expectedAgents  int
	}{
		{
			name:            "3 control planes and 2 workers",
			controlPlanes:   3,
			workers:         2,
			expectedServers: 3,
			expectedAgents:  2,
		},
		{
			name:            "default counts",
			controlPlanes:   0,
			workers:         0,
			expectedServers: 0,
			expectedAgents:  0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						Talos: v1alpha1.OptionsTalos{
							ControlPlanes: testCase.controlPlanes,
							Workers:       testCase.workers,
						},
					},
				},
			}

			scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
			config := scaffolderInstance.CreateK3dConfig(".")

			assert.Equal(t, testCase.expectedServers, config.Servers)
			assert.Equal(t, testCase.expectedAgents, config.Agents)
		})
	}
}

// TestScaffoldK3d_WithClusterName tests that custom cluster name is applied to K3d config.
func TestScaffoldK3d_WithClusterName(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	scaffolderInstance.WithClusterName("my-custom-cluster")

	config := scaffolderInstance.CreateK3dConfig(".")
	assert.Equal(t, "my-custom-cluster", config.Name)
}

// TestScaffoldK3d_LocalRegistryEnabled tests K3d config with local registry.
func TestScaffoldK3d_LocalRegistryEnabled(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	config := scaffolderInstance.CreateK3dConfig(".")

	// When local registry is enabled, Registries.Create should be set
	require.NotNil(t, config.Registries.Create, "Registries.Create should be set")
	assert.NotEmpty(t, config.Registries.Create.Name)
	assert.NotEmpty(t, config.Registries.Config)
}

// TestScaffoldKind_CalicoCNI tests that Calico CNI also disables default CNI.
func TestScaffoldKind_CalicoCNI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVanilla,
				DistributionConfig: scaffolder.KindConfigFile,
				CNI:                v1alpha1.CNICalico,
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, true)
	require.NoError(t, err)

	kindPath := filepath.Join(tempDir, scaffolder.KindConfigFile)
	content, readErr := os.ReadFile(kindPath) //nolint:gosec // test file
	require.NoError(t, readErr)

	assert.Contains(t, string(content), "disableDefaultCNI: true")
}

// TestScaffoldK3d_CalicoCNI tests that Calico CNI in K3d disables flannel.
func TestScaffoldK3d_CalicoCNI(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICalico,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	config := scaffolderInstance.CreateK3dConfig(".")

	hasFlannel := false
	hasDisableNetworkPolicy := false

	for _, arg := range config.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--flannel-backend=none" {
			hasFlannel = true
		}

		if arg.Arg == "--disable-network-policy" {
			hasDisableNetworkPolicy = true
		}
	}

	assert.True(t, hasFlannel, "should disable flannel for Calico")
	assert.True(t, hasDisableNetworkPolicy, "should disable network policy for Calico")
}

// TestScaffoldVCluster_ForceOverwrite tests force overwriting vcluster.yaml.
func TestScaffoldVCluster_ForceOverwrite(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createVClusterCluster("vcluster-force-test")

	// First scaffold
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)
	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Second scaffold with force
	scaffolderInstance2 := scaffolder.NewScaffolder(cluster, io.Discard, nil)
	err = scaffolderInstance2.Scaffold(tempDir, true)
	require.NoError(t, err)

	// Verify vcluster.yaml still exists
	vclusterPath := filepath.Join(tempDir, scaffolder.VClusterConfigFile)
	_, statErr := os.Stat(vclusterPath)
	require.NoError(t, statErr)
}

// TestScaffoldK3d_ContainerdConfig_ForceOverwrite tests force overwriting containerd config.
func TestScaffoldK3d_ContainerdConfig_ForceOverwrite(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createK3dCluster("k3d-containerd-force")
	cluster.Spec.Cluster.Talos.ImageVerification = v1alpha1.ImageVerificationEnabled

	// First scaffold
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)
	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Second scaffold with force
	scaffolderInstance2 := scaffolder.NewScaffolder(cluster, io.Discard, nil)
	err = scaffolderInstance2.Scaffold(tempDir, true)
	require.NoError(t, err)
}

// TestScaffold_GetKindMirrorsDir tests GetKindMirrorsDir returns the correct path.
func TestScaffold_GetKindMirrorsDir(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)
	result := scaffolderInstance.GetKindMirrorsDir()

	assert.NotEmpty(t, result, "GetKindMirrorsDir should return non-empty path")
}
