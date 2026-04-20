package fluxinstaller_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildExternalRegistryURL_Formats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registry   v1alpha1.LocalRegistry
		wantURL    string
		wantSecret string
		wantTag    string
	}{
		{
			name: "registry with tagged version",
			registry: v1alpha1.LocalRegistry{
				Registry: "user:pass@ghcr.io/myorg/myrepo:v2.0.0",
			},
			wantURL:    "oci://ghcr.io/myorg/myrepo",
			wantSecret: fluxinstaller.ExternalRegistrySecretName,
			wantTag:    "v2.0.0",
		},
		{
			name: "docker.io registry",
			registry: v1alpha1.LocalRegistry{
				Registry: "docker.io/library/nginx",
			},
			wantURL:    "oci://docker.io/library/nginx",
			wantSecret: "",
		},
		{
			name: "bare host with path",
			registry: v1alpha1.LocalRegistry{
				Registry: "gcr.io/my-project/images",
			},
			wantURL:    "oci://gcr.io/my-project/images",
			wantSecret: "",
		},
	}

	//nolint:varnamelen // Short names keep table-driven tests readable.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			url, secret, tag := fluxinstaller.BuildExternalRegistryURL(tt.registry)

			assert.Equal(t, tt.wantURL, url)
			assert.Equal(t, tt.wantSecret, secret)

			if tt.wantTag != "" {
				assert.Equal(t, tt.wantTag, tag)
			}
		})
	}
}

func TestBuildInstance_MinimalConfig(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
			},
		},
	}

	instance, err := fluxinstaller.BuildInstance(cfg, "test-cluster", "")
	require.NoError(t, err)
	require.NotNil(t, instance)

	// Instance should have correct TypeMeta
	assert.Equal(t, "fluxcd.controlplane.io/v1", instance.APIVersion)
	assert.Equal(t, "FluxInstance", instance.Kind)
	assert.Equal(t, "flux", instance.Name)
	assert.Equal(t, "flux-system", instance.Namespace)
}

func TestBuildInstance_WithLocalRegistryAndWorkload(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}

	instance, err := fluxinstaller.BuildInstance(cfg, "my-cluster", "")
	require.NoError(t, err)
	require.NotNil(t, instance)

	// Should have sync configuration when local registry is configured
	assert.NotEmpty(t, instance.Spec.Distribution.Version)
}

func TestBuildInstance_RegistryHostOverride(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	instance, err := fluxinstaller.BuildInstance(cfg, "cluster", "custom-host:5001")
	require.NoError(t, err)
	require.NotNil(t, instance)
}

//nolint:varnamelen // Short names keep this table-driven test readable.
func TestBuildLocalRegistryURL_Variations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		registry             v1alpha1.LocalRegistry
		clusterCfg           *v1alpha1.Cluster
		clusterName          string
		registryHostOverride string
	}{
		{
			name: "no host override uses default",
			registry: v1alpha1.LocalRegistry{
				Registry: "localhost:5000",
			},
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{},
				},
			},
			clusterName:          "test-cluster",
			registryHostOverride: "",
		},
		{
			name: "with host override",
			registry: v1alpha1.LocalRegistry{
				Registry: "localhost:5000",
			},
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{},
				},
			},
			clusterName:          "test-cluster",
			registryHostOverride: "my-registry:9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := fluxinstaller.BuildLocalRegistryURL(
				tt.registry,
				tt.clusterCfg,
				tt.clusterName,
				tt.registryHostOverride,
			)

			assert.NotEmpty(t, result, "URL should not be empty")
		})
	}
}

func TestNormalizeFluxPath_Coverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", "./"},
		{"dot", ".", "./"},
		{"dot-slash", "./", "./"},
		{"simple path", "k8s", "./k8s"},
		{"path with dot-slash prefix", "./k8s", "./k8s"},
		{"nested path", "deploy/k8s/overlays", "./deploy/k8s/overlays"},
		{"path with dot-slash nested", "./deploy/k8s", "./deploy/k8s"},
		{"trailing slash", "k8s/", "./k8s"},
		{"multiple slashes", "a/b/c/d", "./a/b/c/d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := fluxinstaller.NormalizeFluxPath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

//nolint:varnamelen // Short names keep this table-driven test readable.
func TestBuildDockerConfigJSON_Formats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "valid credentials",
			registry: "ghcr.io",
			username: "myuser",
			password: "mytoken",
			wantErr:  false,
		},
		{
			name:     "localhost registry",
			registry: "localhost:5000",
			username: "admin",
			password: "secret",
			wantErr:  false,
		},
		{
			name:     "empty credentials",
			registry: "ghcr.io",
			username: "",
			password: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := fluxinstaller.BuildDockerConfigJSON(tt.registry, tt.username, tt.password)

			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, data)
			// JSON should be valid and contain the registry key
			assert.Contains(t, string(data), tt.registry)
		})
	}
}

//nolint:varnamelen // Short names keep this table-driven test readable.
func TestBuildRegistrySecret_Types(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *v1alpha1.Cluster
		wantErr bool
	}{
		{
			name: "with external registry credentials",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "myuser:mypass@ghcr.io/org/repo",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "local registry no credentials",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "localhost:5000",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			secret, err := fluxinstaller.BuildRegistrySecret(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, secret)
			assert.Equal(t, fluxinstaller.ExternalRegistrySecretName, secret.Name)
			assert.Equal(t, "flux-system", secret.Namespace)
		})
	}
}

func TestIsTransientAPIError_NilError(t *testing.T) {
	t.Parallel()

	result := fluxinstaller.IsTransientAPIError(nil)
	assert.False(t, result)
}
