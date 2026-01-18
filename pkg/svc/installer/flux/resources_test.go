// Package fluxinstaller_test provides unit tests for the flux installer package.
//
//nolint:err113,funlen // Tests use dynamic errors for mock behaviors and table-driven tests are naturally long
package fluxinstaller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestBuildDockerConfigJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		registry       string
		username       string
		password       string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:     "basic credentials",
			registry: "ghcr.io",
			username: "user",
			password: "pass",
			wantContains: []string{
				`"auths"`,
				`"ghcr.io"`,
				`"username":"user"`,
				`"password":"pass"`,
				`"auth"`,
			},
		},
		{
			name:     "custom registry",
			registry: "registry.example.com:5000",
			username: "admin",
			password: "secret123",
			wantContains: []string{
				`"registry.example.com:5000"`,
				`"username":"admin"`,
				`"password":"secret123"`,
			},
		},
		{
			name:     "special characters in password",
			registry: "docker.io",
			username: "user@example.com",
			password: "p@ss:w0rd!",
			wantContains: []string{
				`"username":"user@example.com"`,
				`"password":"p@ss:w0rd!"`,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			data, err := fluxinstaller.BuildDockerConfigJSON(
				testCase.registry,
				testCase.username,
				testCase.password,
			)
			require.NoError(t, err)
			require.NotEmpty(t, data)

			jsonStr := string(data)
			for _, want := range testCase.wantContains {
				assert.Contains(t, jsonStr, want)
			}

			for _, notWant := range testCase.wantNotContain {
				assert.NotContains(t, jsonStr, notWant)
			}
		})
	}
}

func TestBuildExternalRegistryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		localRegistry  v1alpha1.LocalRegistry
		wantURL        string
		wantSecret     string
		wantTagContain string
	}{
		{
			name: "external registry without credentials",
			localRegistry: v1alpha1.LocalRegistry{
				Registry: "ghcr.io/example/repo",
			},
			wantURL:    "oci://ghcr.io/example/repo",
			wantSecret: "",
		},
		{
			name: "external registry with credentials",
			localRegistry: v1alpha1.LocalRegistry{
				Registry: "user:pass@ghcr.io/example/repo",
			},
			wantURL:    "oci://ghcr.io/example/repo",
			wantSecret: fluxinstaller.ExternalRegistrySecretName,
		},
		{
			name: "external registry with tag",
			localRegistry: v1alpha1.LocalRegistry{
				Registry: "user:pass@ghcr.io/example/repo:v1.0.0",
			},
			wantURL:        "oci://ghcr.io/example/repo",
			wantSecret:     fluxinstaller.ExternalRegistrySecretName,
			wantTagContain: "v1.0.0",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			url, secret, tag := fluxinstaller.BuildExternalRegistryURL(testCase.localRegistry)
			assert.Equal(t, testCase.wantURL, url)
			assert.Equal(t, testCase.wantSecret, secret)

			if testCase.wantTagContain != "" {
				assert.Equal(t, testCase.wantTagContain, tag)
			}
		})
	}
}

func TestBuildLocalRegistryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		localRegistry v1alpha1.LocalRegistry
		clusterCfg    *v1alpha1.Cluster
		clusterName   string
		wantContains  []string
	}{
		{
			name:          "default local registry enabled",
			localRegistry: v1alpha1.LocalRegistry{},
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
			clusterName: "test-cluster",
			wantContains: []string{
				"oci://",
			},
		},
		{
			name:          "custom source directory",
			localRegistry: v1alpha1.LocalRegistry{},
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "manifests/kubernetes",
					},
				},
			},
			clusterName: "my-cluster",
			wantContains: []string{
				"oci://",
				"manifests-kubernetes",
			},
		},
		{
			name:          "empty source directory uses default",
			localRegistry: v1alpha1.LocalRegistry{},
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "",
					},
				},
			},
			clusterName:  "cluster",
			wantContains: []string{"oci://"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			url := fluxinstaller.BuildLocalRegistryURL(
				testCase.localRegistry,
				testCase.clusterCfg,
				testCase.clusterName,
			)
			for _, want := range testCase.wantContains {
				assert.Contains(t, url, want)
			}
		})
	}
}

func TestBuildFluxInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterCfg  *v1alpha1.Cluster
		clusterName string
		wantName    string
	}{
		{
			name: "local registry enabled",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
			clusterName: "test-cluster",
			wantName:    "flux",
		},
		{
			name: "external registry",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/example/repo",
						},
					},
					Workload: v1alpha1.WorkloadSpec{
						SourceDirectory: "k8s",
					},
				},
			},
			clusterName: "test-cluster",
			wantName:    "flux",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			instance, err := fluxinstaller.BuildFluxInstance(
				testCase.clusterCfg,
				testCase.clusterName,
			)
			require.NoError(t, err)
			require.NotNil(t, instance)

			assert.Equal(t, testCase.wantName, instance.GetName())
			assert.Equal(t, "flux-system", instance.GetNamespace())
			assert.NotNil(t, instance.Spec.Sync)
			assert.Equal(t, "OCIRepository", instance.Spec.Sync.Kind)
			assert.NotEmpty(t, instance.Spec.Sync.URL)
		})
	}
}

func TestBuildRegistrySecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
		wantName   string
	}{
		{
			name: "external registry with credentials",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "user:pass@ghcr.io/example/repo",
						},
					},
				},
			},
			wantName: fluxinstaller.ExternalRegistrySecretName,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			secret, err := fluxinstaller.BuildRegistrySecret(testCase.clusterCfg)
			require.NoError(t, err)
			require.NotNil(t, secret)

			assert.Equal(t, testCase.wantName, secret.Name)
			assert.Equal(t, "flux-system", secret.Namespace)
			assert.Contains(t, secret.Labels, "app.kubernetes.io/managed-by")
			assert.Equal(t, "ksail", secret.Labels["app.kubernetes.io/managed-by"])
			assert.NotEmpty(t, secret.Data[".dockerconfigjson"])
		})
	}
}

func TestIsTransientAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{
			name:      "nil error",
			err:       nil,
			wantRetry: false,
		},
		{
			name:      "generic error",
			err:       errors.New("some error"),
			wantRetry: false,
		},
		{
			name:      "service unavailable",
			err:       apierrors.NewServiceUnavailable("service unavailable"),
			wantRetry: true,
		},
		{
			name:      "timeout error",
			err:       apierrors.NewTimeoutError("timeout", 1),
			wantRetry: true,
		},
		{
			name:      "too many requests",
			err:       apierrors.NewTooManyRequestsError("too many requests"),
			wantRetry: true,
		},
		{
			name: "conflict error",
			err: apierrors.NewConflict(
				schema.GroupResource{Group: "", Resource: "pods"},
				"test",
				errors.New("conflict"),
			),
			wantRetry: true,
		},
		{
			name:      "server could not find resource",
			err:       errors.New("the server could not find the requested resource"),
			wantRetry: true,
		},
		{
			name:      "no matches for kind",
			err:       errors.New("no matches for kind \"FluxInstance\" in version"),
			wantRetry: true,
		},
		{
			name:      "connection refused",
			err:       errors.New("connection refused"),
			wantRetry: true,
		},
		{
			name:      "connection reset",
			err:       errors.New("connection reset by peer"),
			wantRetry: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := fluxinstaller.IsTransientAPIError(testCase.err)
			assert.Equal(t, testCase.wantRetry, result)
		})
	}
}

func TestNormalizeFluxPath(t *testing.T) {
	t.Parallel()

	path := fluxinstaller.NormalizeFluxPath()
	assert.Equal(t, "./", path)
}

func TestPollUntilReady_Success(t *testing.T) {
	t.Parallel()

	callCount := 0
	checkFn := func() (bool, error) {
		callCount++
		if callCount >= 2 {
			return true, nil
		}

		return false, nil
	}

	err := fluxinstaller.PollUntilReady(
		context.Background(),
		5*time.Second,
		10*time.Millisecond,
		"test resource",
		checkFn,
	)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 2)
}

func TestPollUntilReady_Timeout(t *testing.T) {
	t.Parallel()

	checkFn := func() (bool, error) {
		return false, errors.New("not ready")
	}

	err := fluxinstaller.PollUntilReady(
		context.Background(),
		50*time.Millisecond,
		10*time.Millisecond,
		"test resource",
		checkFn,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Contains(t, err.Error(), "test resource")
}

func TestPollUntilReady_ContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	checkFn := func() (bool, error) {
		return false, nil
	}

	err := fluxinstaller.PollUntilReady(
		ctx,
		5*time.Second,
		10*time.Millisecond,
		"test resource",
		checkFn,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestPollUntilReady_ImmediateSuccess(t *testing.T) {
	t.Parallel()

	callCount := 0
	checkFn := func() (bool, error) {
		callCount++

		return true, nil
	}

	err := fluxinstaller.PollUntilReady(
		context.Background(),
		5*time.Second,
		10*time.Millisecond,
		"test resource",
		checkFn,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestEnsureDefaultResources_NilConfig(t *testing.T) {
	t.Parallel()

	err := fluxinstaller.EnsureDefaultResources(
		context.Background(),
		"",
		nil,
		"test-cluster",
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster configuration is required")
}

func TestBuildLocalRegistryURL_CustomPort(t *testing.T) {
	t.Parallel()

	localRegistry := v1alpha1.LocalRegistry{
		Registry: "localhost:8080",
	}

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}

	url := fluxinstaller.BuildLocalRegistryURL(localRegistry, clusterCfg, "test")

	// Should use the resolved host:port from the local registry ref
	assert.Contains(t, url, "oci://")
}
