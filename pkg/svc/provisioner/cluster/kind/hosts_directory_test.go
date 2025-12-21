package kindprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestSetupRegistryHostsDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mirrorSpecs    []registry.MirrorSpec
		clusterName    string
		scaffoldFirst  bool // Whether to pre-create scaffolded files
		wantNil        bool
		wantErr        bool
	}{
		{
			name: "generates hosts.toml at runtime when no scaffolded config exists",
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName:   "test-cluster",
			scaffoldFirst: false,
			wantNil:       false,
			wantErr:       false,
		},
		{
			name: "preserves scaffolded config when it exists",
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			clusterName:   "test-cluster-scaffolded",
			scaffoldFirst: true,
			wantNil:       false,
			wantErr:       false,
		},
		{
			name:          "returns nil for empty mirror specs",
			mirrorSpecs:   []registry.MirrorSpec{},
			clusterName:   "test-cluster-empty",
			scaffoldFirst: false,
			wantNil:       true,
			wantErr:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hostsDir := "kind-mirrors"

			// Pre-create scaffolded directory if needed
			if tc.scaffoldFirst {
				for _, spec := range tc.mirrorSpecs {
					registryDir := filepath.Join(hostsDir, spec.Host)
					err := os.MkdirAll(registryDir, 0o755)
					require.NoError(t, err)

					// Write a scaffolded hosts.toml with custom upstream
					hostsPath := filepath.Join(registryDir, "hosts.toml")
					content := `server = "https://` + spec.Host + `"` + "\n\n" +
						`[host."` + spec.Remote + `"]` + "\n" +
						`  capabilities = ["pull", "resolve"]` + "\n"
					err = os.WriteFile(hostsPath, []byte(content), 0o644)
					require.NoError(t, err)
				}
			}

			mgr, err := kindprovisioner.SetupRegistryHostsDirectory(tc.mirrorSpecs, tc.clusterName)

			// Cleanup after test
			defer func() {
				_ = os.RemoveAll(hostsDir)
			}()

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.wantNil {
				assert.Nil(t, mgr)
				return
			}

			require.NotNil(t, mgr)

			// Verify base directory exists
			baseDir := mgr.GetBaseDir()
			assert.DirExists(t, baseDir)

			// Verify hosts.toml files exist
			for _, spec := range tc.mirrorSpecs {
				hostDir := filepath.Join(baseDir, spec.Host)
				assert.DirExists(t, hostDir)

				hostsFile := filepath.Join(hostDir, "hosts.toml")
				assert.FileExists(t, hostsFile)

				// Verify content
				content, readErr := os.ReadFile(hostsFile)
				require.NoError(t, readErr)
				assert.Contains(t, string(content), "server = ")
				assert.Contains(t, string(content), `capabilities = ["pull", "resolve"]`)

				// If scaffolded, verify the custom upstream was preserved
				if tc.scaffoldFirst {
					assert.Contains(t, string(content), spec.Remote)
				}
			}
		})
	}
}

func TestConfigureKindWithHostsDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kindConfig  *v1alpha4.Cluster
		hostsDir    string
		mirrorSpecs []registry.MirrorSpec
		wantErr     bool
		checkMounts func(*testing.T, *v1alpha4.Cluster)
	}{
		{
			name: "adds extraMounts for single mirror",
			kindConfig: &v1alpha4.Cluster{
				Nodes: []v1alpha4.Node{
					{Role: v1alpha4.ControlPlaneRole},
				},
			},
			hostsDir: "/tmp/test-hosts",
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			wantErr: false,
			checkMounts: func(t *testing.T, config *v1alpha4.Cluster) {
				t.Helper()
				require.Len(t, config.Nodes, 1)
				require.Len(t, config.Nodes[0].ExtraMounts, 1)

				mount := config.Nodes[0].ExtraMounts[0]
				assert.Equal(t, "/tmp/test-hosts/docker.io", mount.HostPath)
				assert.Equal(t, "/etc/containerd/certs.d/docker.io", mount.ContainerPath)
				assert.True(t, mount.Readonly)
			},
		},
		{
			name: "adds extraMounts for multiple mirrors",
			kindConfig: &v1alpha4.Cluster{
				Nodes: []v1alpha4.Node{
					{Role: v1alpha4.ControlPlaneRole},
				},
			},
			hostsDir: "/tmp/test-hosts-multi",
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			wantErr: false,
			checkMounts: func(t *testing.T, config *v1alpha4.Cluster) {
				t.Helper()
				require.Len(t, config.Nodes, 1)
				require.Len(t, config.Nodes[0].ExtraMounts, 2)

				// Check docker.io mount
				mount1 := config.Nodes[0].ExtraMounts[0]
				assert.Equal(t, "/tmp/test-hosts-multi/docker.io", mount1.HostPath)
				assert.Equal(t, "/etc/containerd/certs.d/docker.io", mount1.ContainerPath)

				// Check ghcr.io mount
				mount2 := config.Nodes[0].ExtraMounts[1]
				assert.Equal(t, "/tmp/test-hosts-multi/ghcr.io", mount2.HostPath)
				assert.Equal(t, "/etc/containerd/certs.d/ghcr.io", mount2.ContainerPath)
			},
		},
		{
			name: "creates default node if none exist",
			kindConfig: &v1alpha4.Cluster{
				Nodes: []v1alpha4.Node{},
			},
			hostsDir: "/tmp/test-hosts-no-nodes",
			mirrorSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			wantErr: false,
			checkMounts: func(t *testing.T, config *v1alpha4.Cluster) {
				t.Helper()
				require.Len(t, config.Nodes, 1)
				assert.Equal(t, v1alpha4.ControlPlaneRole, config.Nodes[0].Role)
				require.Len(t, config.Nodes[0].ExtraMounts, 1)
			},
		},
		{
			name:        "handles nil config",
			kindConfig:  nil,
			hostsDir:    "/tmp/test-hosts-nil",
			mirrorSpecs: []registry.MirrorSpec{{Host: "docker.io"}},
			wantErr:     false,
		},
		{
			name:        "handles empty hostsDir",
			kindConfig:  &v1alpha4.Cluster{},
			hostsDir:    "",
			mirrorSpecs: []registry.MirrorSpec{{Host: "docker.io"}},
			wantErr:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := kindprovisioner.ConfigureKindWithHostsDirectory(
				tc.kindConfig,
				tc.hostsDir,
				tc.mirrorSpecs,
			)

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.checkMounts != nil {
				tc.checkMounts(t, tc.kindConfig)
			}
		})
	}
}

func TestCleanupHostsDirectory(t *testing.T) {
	t.Parallel()

	// Create a test directory
	clusterName := "test-cleanup-cluster"
	hostsDir := "kind-mirrors"

	err := os.MkdirAll(hostsDir, 0o755)
	require.NoError(t, err)

	// Create a test file
	testFile := filepath.Join(hostsDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test"), 0o644)
	require.NoError(t, err)

	// Verify directory exists
	assert.DirExists(t, hostsDir)

	// Cleanup
	kindprovisioner.CleanupHostsDirectory(clusterName)

	// Verify directory was removed
	assert.NoDirExists(t, hostsDir)
}
