package docker_test

import (
	"errors"
	"testing"

	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

//nolint:err113,funlen // Tests use controlled mock errors. Table-driven coverage stays easier to read as one block.
func TestIsContainerRunning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		containers  []container.Summary
		listErr     error
		expected    bool
		expectErr   bool
		errContains string
	}{
		{
			name: "returns true when container is running",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/k3d-registry"},
					State: "running",
				},
			},
			expected: true,
		},
		{
			name: "returns false when container is exited",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/k3d-registry"},
					State: "exited",
				},
			},
			expected: false,
		},
		{
			name:       "returns false when no containers found",
			containers: []container.Summary{},
			expected:   false,
		},
		{
			name:        "returns error when list fails",
			listErr:     errors.New("daemon not running"),
			expectErr:   true,
			errContains: "failed to list containers",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient, manager, ctx := setupTestRegistryManager(t)

			mockClient.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(testCase.containers, testCase.listErr).
				Once()

			result, err := manager.IsContainerRunning(ctx, "k3d-registry")

			if testCase.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

//nolint:err113,funlen // Tests use controlled mock errors. Table-driven coverage stays easier to read as one block.
func TestGetContainerPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		containers   []container.Summary
		privatePort  uint16
		listErr      error
		expectedPort int
		expectedErr  error
		errContains  string
	}{
		{
			name: "returns port when matching private port found",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/k3d-registry"},
					Ports: []container.Port{
						{PrivatePort: 5000, PublicPort: 5050},
					},
				},
			},
			privatePort:  5000,
			expectedPort: 5050,
		},
		{
			name:        "returns ErrRegistryNotFound when no containers",
			containers:  []container.Summary{},
			privatePort: 5000,
			expectedErr: docker.ErrRegistryNotFound,
		},
		{
			name: "returns ErrRegistryPortNotFound when no matching port",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/k3d-registry"},
					Ports: []container.Port{
						{PrivatePort: 8080, PublicPort: 9090},
					},
				},
			},
			privatePort: 5000,
			expectedErr: docker.ErrRegistryPortNotFound,
		},
		{
			name:        "returns error when list fails",
			listErr:     errors.New("connection refused"),
			privatePort: 5000,
			errContains: "failed to list containers",
		},
		{
			name: "returns first matching port from multiple ports",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/k3d-registry"},
					Ports: []container.Port{
						{PrivatePort: 8080, PublicPort: 9090},
						{PrivatePort: 5000, PublicPort: 5001},
					},
				},
			},
			privatePort:  5000,
			expectedPort: 5001,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient, manager, ctx := setupTestRegistryManager(t)

			mockClient.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(testCase.containers, testCase.listErr).
				Once()

			port, err := manager.GetContainerPort(ctx, "k3d-registry", testCase.privatePort)

			if testCase.expectedErr != nil {
				require.ErrorIs(t, err, testCase.expectedErr)
				assert.Zero(t, port)

				return
			}

			if testCase.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedPort, port)
		})
	}
}

//nolint:err113,funlen // Tests use controlled mock errors. Table-driven coverage stays easier to read as one block.
func TestGetUsedHostPorts(t *testing.T) {
	t.Parallel()

	t.Run("returns all public ports from running containers", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID: "c1",
					Ports: []container.Port{
						{PublicPort: 5000},
						{PublicPort: 5001},
					},
				},
				{
					ID: "c2",
					Ports: []container.Port{
						{PublicPort: 8080},
						{PublicPort: 0}, // No public port bound
					},
				},
			}, nil).
			Once()

		ports, err := manager.GetUsedHostPorts(ctx)

		require.NoError(t, err)
		assert.Len(t, ports, 3)

		_, has5000 := ports[5000]
		assert.True(t, has5000)

		_, has5001 := ports[5001]
		assert.True(t, has5001)

		_, has8080 := ports[8080]
		assert.True(t, has8080)

		_, has0 := ports[0]
		assert.False(t, has0, "port 0 should not be included")
	})

	t.Run("returns empty map when no containers", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{}, nil).
			Once()

		ports, err := manager.GetUsedHostPorts(ctx)

		require.NoError(t, err)
		assert.Empty(t, ports)
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return(nil, errors.New("docker not running")).
			Once()

		ports, err := manager.GetUsedHostPorts(ctx)

		require.Error(t, err)
		assert.Nil(t, ports)
		assert.Contains(t, err.Error(), "failed to list containers")
	})

	t.Run("handles containers with no ports", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:    "c1",
					Ports: []container.Port{},
				},
			}, nil).
			Once()

		ports, err := manager.GetUsedHostPorts(ctx)

		require.NoError(t, err)
		assert.Empty(t, ports)
	})
}

//nolint:err113,funlen // Tests use controlled mock errors. Table-driven coverage stays easier to read as one block.
func TestFindContainerBySuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		suffix       string
		containers   []container.Summary
		listErr      error
		expectedName string
		expectErr    bool
		errContains  string
	}{
		{
			name:   "finds container matching suffix",
			suffix: "-local-registry",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/test-cluster-local-registry"},
				},
			},
			expectedName: "test-cluster-local-registry",
		},
		{
			name:   "returns empty when no match",
			suffix: "-local-registry",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/some-other-container"},
				},
			},
			expectedName: "",
		},
		{
			name:         "returns empty when no containers",
			suffix:       "-local-registry",
			containers:   []container.Summary{},
			expectedName: "",
		},
		{
			name:        "returns error when list fails",
			suffix:      "-local-registry",
			listErr:     errors.New("daemon error"),
			expectErr:   true,
			errContains: "failed to list containers",
		},
		{
			name:   "finds first matching container from multiple",
			suffix: "-registry",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/alpha-registry"},
				},
				{
					ID:    "c2",
					Names: []string{"/beta-registry"},
				},
			},
			expectedName: "alpha-registry",
		},
		{
			name:   "handles container with multiple names",
			suffix: "-mirror",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/alias1", "/actual-mirror"},
				},
			},
			expectedName: "actual-mirror",
		},
		{
			name:   "strips leading slash from container name",
			suffix: "-reg",
			containers: []container.Summary{
				{
					ID:    "c1",
					Names: []string{"/cluster-reg"},
				},
			},
			expectedName: "cluster-reg",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient, manager, ctx := setupTestRegistryManager(t)

			mockClient.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(testCase.containers, testCase.listErr).
				Once()

			name, err := manager.FindContainerBySuffix(ctx, testCase.suffix)

			if testCase.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedName, name)
		})
	}
}

//nolint:funlen // Table-driven coverage stays easier to read as one block.
func TestBuildContainerConfig(t *testing.T) {
	t.Parallel()

	t.Run("basic config without upstream", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name: "my-registry",
			Port: 5000,
		}

		cfg, err := manager.ExportBuildContainerConfig(config)

		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, docker.RegistryImageName, cfg.Image)
		assert.Equal(t, "my-registry", cfg.Labels[docker.RegistryLabelKey])
		assert.Empty(t, cfg.Env, "no env vars for non-proxy registry")
		assert.NotNil(t, cfg.Healthcheck)
	})

	t.Run("config with upstream URL sets proxy env", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name:        "docker.io",
			Port:        5000,
			UpstreamURL: "https://registry-1.docker.io",
		}

		cfg, err := manager.ExportBuildContainerConfig(config)

		require.NoError(t, err)
		assert.Contains(t, cfg.Env, "REGISTRY_PROXY_REMOTEURL=https://registry-1.docker.io")
	})

	t.Run("config with empty name has no label", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Port: 5000,
		}

		cfg, err := manager.ExportBuildContainerConfig(config)

		require.NoError(t, err)

		_, hasLabel := cfg.Labels[docker.RegistryLabelKey]
		assert.False(t, hasLabel)
	})

	t.Run("exposed ports always include registry port", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name: "test",
		}

		cfg, err := manager.ExportBuildContainerConfig(config)

		require.NoError(t, err)

		_, hasPort := cfg.ExposedPorts[docker.RegistryContainerPort]
		assert.True(t, hasPort)
	})
}

//nolint:funlen // Table-driven coverage stays easier to read as one block.
func TestBuildHostConfig(t *testing.T) {
	t.Parallel()

	t.Run("local registry gets port binding", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name: "local-registry",
			Port: 5001,
			// No UpstreamURL → local registry
		}

		hostCfg := manager.ExportBuildHostConfig(config, "my-volume")

		require.NotNil(t, hostCfg)
		bindings := hostCfg.PortBindings[docker.RegistryContainerPort]
		require.Len(t, bindings, 1)
		assert.Equal(t, docker.RegistryHostIP, bindings[0].HostIP)
		assert.Equal(t, "5001", bindings[0].HostPort)
	})

	t.Run("mirror registry gets no port binding", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name:        "docker.io",
			Port:        5000,
			UpstreamURL: "https://registry-1.docker.io",
		}

		hostCfg := manager.ExportBuildHostConfig(config, "vol")

		require.NotNil(t, hostCfg)
		bindings := hostCfg.PortBindings[docker.RegistryContainerPort]
		assert.Empty(t, bindings)
	})

	t.Run("zero port gets no binding", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name: "test",
			Port: 0,
		}

		hostCfg := manager.ExportBuildHostConfig(config, "vol")

		bindings := hostCfg.PortBindings[docker.RegistryContainerPort]
		assert.Empty(t, bindings)
	})

	t.Run("mounts volume to registry data path", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{Name: "test"}

		hostCfg := manager.ExportBuildHostConfig(config, "data-volume")

		require.Len(t, hostCfg.Mounts, 1)
		assert.Equal(t, "data-volume", hostCfg.Mounts[0].Source)
		assert.Equal(t, docker.RegistryDataPath, hostCfg.Mounts[0].Target)
	})

	t.Run("restart policy is unless-stopped", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{Name: "test"}

		hostCfg := manager.ExportBuildHostConfig(config, "vol")

		assert.Equal(t, docker.RegistryRestartPolicy, string(hostCfg.RestartPolicy.Name))
	})
}

func TestBuildNetworkConfig(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when no network specified", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name: "test",
		}

		netCfg := manager.ExportBuildNetworkConfig(config)

		assert.Nil(t, netCfg)
	})

	t.Run("returns config with network endpoint", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		config := docker.RegistryConfig{
			Name:        "test",
			NetworkName: "kind-test-cluster",
		}

		netCfg := manager.ExportBuildNetworkConfig(config)

		require.NotNil(t, netCfg)
		require.NotNil(t, netCfg.EndpointsConfig)
		_, hasEndpoint := netCfg.EndpointsConfig["kind-test-cluster"]
		assert.True(t, hasEndpoint)
	})
}

func TestResolveVolumeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   docker.RegistryConfig
		expected string
	}{
		{
			name: "uses explicit VolumeName when set",
			config: docker.RegistryConfig{
				Name:       "docker.io",
				VolumeName: "custom-volume",
			},
			expected: "custom-volume",
		},
		{
			name: "normalizes registry name when no explicit volume",
			config: docker.RegistryConfig{
				Name: "kind-docker.io",
			},
			expected: "docker.io",
		},
		{
			name: "uses name as-is for non-prefixed registry",
			config: docker.RegistryConfig{
				Name: "my-registry",
			},
			expected: "my-registry",
		},
		{
			name: "strips k3d prefix",
			config: docker.RegistryConfig{
				Name: "k3d-registry",
			},
			expected: "registry",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, manager, _ := setupTestRegistryManager(t)

			result := manager.ExportResolveVolumeName(testCase.config)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestBuildProxyCredentialsEnv(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty credentials", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		env, err := manager.ExportBuildProxyCredentialsEnv("", "")

		require.NoError(t, err)
		assert.Nil(t, env)
	})

	t.Run("returns error for partial credentials - username only", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		_, err := manager.ExportBuildProxyCredentialsEnv("myuser", "")

		require.ErrorIs(t, err, docker.ErrRegistryPartialCredentials)
	})

	t.Run("returns error for partial credentials - password only", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		_, err := manager.ExportBuildProxyCredentialsEnv("", "mypass")

		require.ErrorIs(t, err, docker.ErrRegistryPartialCredentials)
	})

	t.Run("returns env vars for valid credentials", func(t *testing.T) {
		t.Parallel()

		_, manager, _ := setupTestRegistryManager(t)

		env, err := manager.ExportBuildProxyCredentialsEnv("myuser", "mypass")

		require.NoError(t, err)
		require.Len(t, env, 2)
		assert.Equal(t, "REGISTRY_PROXY_USERNAME=myuser", env[0])
		assert.Equal(t, "REGISTRY_PROXY_PASSWORD=mypass", env[1])
	})
}

func TestBuildProxyCredentialsEnv_ExpandsEnvVars(t *testing.T) {
	t.Setenv("TEST_USER", "expanded-user")
	t.Setenv("TEST_PASS", "expanded-pass")

	_, manager, _ := setupTestRegistryManager(t)

	env, err := manager.ExportBuildProxyCredentialsEnv("${TEST_USER}", "${TEST_PASS}")

	require.NoError(t, err)
	require.Len(t, env, 2)
	assert.Equal(t, "REGISTRY_PROXY_USERNAME=expanded-user", env[0])
	assert.Equal(t, "REGISTRY_PROXY_PASSWORD=expanded-pass", env[1])
}

//nolint:gosec // Test-only fixtures use controlled temp paths and permissions.
func TestBuildContainerConfig_WithCredentials(t *testing.T) {
	t.Setenv("PROXY_USER", "testuser")
	t.Setenv("PROXY_PASS", "testpass")

	_, manager, _ := setupTestRegistryManager(t)

	config := docker.RegistryConfig{
		Name:        "ghcr.io",
		Port:        5000,
		UpstreamURL: "https://ghcr.io",
		Username:    "${PROXY_USER}",
		Password:    "${PROXY_PASS}",
	}

	cfg, err := manager.ExportBuildContainerConfig(config)

	require.NoError(t, err)
	assert.Contains(t, cfg.Env, "REGISTRY_PROXY_REMOTEURL=https://ghcr.io")
	assert.Contains(t, cfg.Env, "REGISTRY_PROXY_USERNAME=testuser")
	assert.Contains(t, cfg.Env, "REGISTRY_PROXY_PASSWORD=testpass")
}

func TestBuildContainerConfig_PartialCredentialsError(t *testing.T) {
	t.Parallel()

	_, manager, _ := setupTestRegistryManager(t)

	config := docker.RegistryConfig{
		Name:        "ghcr.io",
		Port:        5000,
		UpstreamURL: "https://ghcr.io",
		Username:    "user",
		// Password intentionally empty
	}

	_, err := manager.ExportBuildContainerConfig(config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy credentials")
}
