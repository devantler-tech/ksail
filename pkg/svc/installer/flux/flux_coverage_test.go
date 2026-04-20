//nolint:err113,funlen // Tests use dynamic errors and table-driven tests are naturally long
package fluxinstaller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// ---------------------------------------------------------------------------
// ResolveDesiredTag
// ---------------------------------------------------------------------------

func TestResolveDesiredTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
		want       string
	}{
		{
			name:       "nil cluster config returns default tag",
			clusterCfg: nil,
			want:       "dev",
		},
		{
			name:       "empty tag with local registry returns default",
			clusterCfg: &v1alpha1.Cluster{},
			want:       "dev",
		},
		{
			name: "workload tag set",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{Tag: "v1.2.3"},
				},
			},
			want: "v1.2.3",
		},
		{
			name: "external registry tag used when workload tag empty",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/org/repo:latest",
						},
					},
				},
			},
			want: "latest",
		},
		{
			name: "workload tag takes priority over registry tag",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Workload: v1alpha1.WorkloadSpec{Tag: "my-tag"},
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/org/repo:latest",
						},
					},
				},
			},
			want: "my-tag",
		},
		{
			name: "external registry without tag falls back to default",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "ghcr.io/org/repo",
						},
					},
				},
			},
			want: "dev",
		},
		{
			name: "local registry tag is not used",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "localhost:5000/repo:shouldignore",
						},
					},
				},
			},
			want: "dev",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := fluxinstaller.ResolveDesiredTag(tc.clusterCfg)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// EnsureDefaultResources
// ---------------------------------------------------------------------------

func TestEnsureDefaultResources_NilClusterConfig(t *testing.T) {
	t.Parallel()

	err := fluxinstaller.EnsureDefaultResources(
		context.Background(), "", nil, "test", "", false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster configuration is required")
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestEnsureDefaultResources_NilContext(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return &rest.Config{Host: "https://127.0.0.1:6443"}, nil
	})
	defer restoreREST()

	restoreSetup := fluxinstaller.SetSetupFluxCoreToNoop()
	defer restoreSetup()

	clusterCfg := &v1alpha1.Cluster{}

	//nolint:staticcheck // intentionally passing nil context to test nil-guard
	err := fluxinstaller.EnsureDefaultResources(
		nil, "", clusterCfg, "test", "", false,
	)
	require.NoError(t, err)
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestEnsureDefaultResources_ArtifactNotPushed(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return &rest.Config{Host: "https://127.0.0.1:6443"}, nil
	})
	defer restoreREST()

	restoreSetup := fluxinstaller.SetSetupFluxCoreToNoop()
	defer restoreSetup()

	clusterCfg := &v1alpha1.Cluster{}
	err := fluxinstaller.EnsureDefaultResources(
		context.Background(), "", clusterCfg, "test", "", false,
	)
	require.NoError(t, err)
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestEnsureDefaultResources_LoadRESTConfigError(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return nil, errors.New("config error")
	})
	defer restoreREST()

	clusterCfg := &v1alpha1.Cluster{}
	err := fluxinstaller.EnsureDefaultResources(
		context.Background(), "", clusterCfg, "test", "", false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config error")
}

// ---------------------------------------------------------------------------
// SetupInstance
// ---------------------------------------------------------------------------

func TestSetupInstance_NilClusterConfig(t *testing.T) {
	t.Parallel()

	err := fluxinstaller.SetupInstance(
		context.Background(), "", nil, "test", "",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster configuration is required")
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestSetupInstance_NilContext(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return &rest.Config{Host: "https://127.0.0.1:6443"}, nil
	})
	defer restoreREST()

	restoreSetup := fluxinstaller.SetSetupFluxCoreToNoop()
	defer restoreSetup()

	clusterCfg := &v1alpha1.Cluster{}

	//nolint:staticcheck // intentionally passing nil context to test nil-guard
	err := fluxinstaller.SetupInstance(nil, "", clusterCfg, "test", "")
	require.NoError(t, err)
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestSetupInstance_LoadRESTConfigError(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return nil, errors.New("config error")
	})
	defer restoreREST()

	clusterCfg := &v1alpha1.Cluster{}
	err := fluxinstaller.SetupInstance(
		context.Background(), "", clusterCfg, "test", "",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config error")
}

// ---------------------------------------------------------------------------
// WaitForFluxReady
// ---------------------------------------------------------------------------

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForFluxReady_LoadRESTConfigError(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return nil, errors.New("no config")
	})
	defer restoreREST()

	err := fluxinstaller.WaitForFluxReady(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config")
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForFluxReady_NilContext(t *testing.T) {
	restoreREST := fluxinstaller.SetLoadRESTConfig(func(_ string) (*rest.Config, error) {
		return nil, errors.New("no config")
	})
	defer restoreREST()

	//nolint:staticcheck // intentionally passing nil context to test nil-guard
	err := fluxinstaller.WaitForFluxReady(nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no config")
}

// ---------------------------------------------------------------------------
// NormalizeFluxPath — edge cases
// ---------------------------------------------------------------------------

func TestNormalizeFluxPath_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string returns root",
			input: "",
			want:  "./",
		},
		{
			name:  "dot path returns root",
			input: ".",
			want:  "./",
		},
		{
			name:  "single component gets dot-slash prefix",
			input: "overlays",
			want:  "./overlays",
		},
		{
			name:  "trailing slash cleaned",
			input: "overlays/prod/",
			want:  "./overlays/prod",
		},
		{
			name:  "backslash normalized to forward slash",
			input: `overlays\prod`,
			want:  "./overlays/prod",
		},
		{
			name:  "parent traversal returns root",
			input: "../escape",
			want:  "./",
		},
		{
			name:  "absolute path returns root",
			input: "/absolute/path",
			want:  "./",
		},
		{
			name:  "whitespace trimmed",
			input: "  overlays/prod  ",
			want:  "./overlays/prod",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := fluxinstaller.NormalizeFluxPath(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// IsTransientAPIError
// ---------------------------------------------------------------------------

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestIsTransientAPIError_Extended(t *testing.T) {
	t.Parallel()

	gr := schema.GroupResource{Group: "test", Resource: "things"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "non-transient error",
			err:  errors.New("something else went wrong"),
			want: false,
		},
		{
			name: "service unavailable",
			err:  apierrors.NewServiceUnavailable("try later"),
			want: true,
		},
		{
			name: "timeout",
			err:  apierrors.NewTimeoutError("took too long", 30),
			want: true,
		},
		{
			name: "too many requests",
			err:  apierrors.NewTooManyRequestsError("slow down"),
			want: true,
		},
		{
			name: "conflict",
			err:  apierrors.NewConflict(gr, "obj", errors.New("conflict")),
			want: true,
		},
		{
			name: "server could not find resource pattern",
			err:  errors.New("the server could not find the requested resource"),
			want: true,
		},
		{
			name: "no matches for kind pattern",
			err:  errors.New("no matches for kind \"FluxInstance\" in version \"v1\""),
			want: true,
		},
		{
			name: "connection refused pattern",
			err:  errors.New("dial tcp 127.0.0.1:6443: connection refused"),
			want: true,
		},
		{
			name: "connection reset pattern",
			err:  errors.New("read tcp: connection reset by peer"),
			want: true,
		},
		{
			name: "not found is NOT transient",
			err:  apierrors.NewNotFound(gr, "missing"),
			want: false,
		},
		{
			name: "forbidden is NOT transient",
			err:  apierrors.NewForbidden(gr, "secret", errors.New("no")),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := fluxinstaller.IsTransientAPIError(tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// PollUntilReady
// ---------------------------------------------------------------------------

func TestPollUntilReady_ImmediatelyReady(t *testing.T) {
	t.Parallel()

	err := fluxinstaller.PollUntilReady(
		context.Background(),
		5*time.Second,
		10*time.Millisecond,
		"test-resource",
		func() (bool, error) { return true, nil },
	)
	require.NoError(t, err)
}

func TestPollUntilReady_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := fluxinstaller.PollUntilReady(
		ctx,
		5*time.Second,
		10*time.Millisecond,
		"test-resource",
		func() (bool, error) { return false, nil },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestPollUntilReady_ErrorPropagation(t *testing.T) {
	t.Parallel()

	checkErr := errors.New("transient failure")

	err := fluxinstaller.PollUntilReady(
		context.Background(),
		200*time.Millisecond,
		10*time.Millisecond,
		"test-resource",
		func() (bool, error) { return false, checkErr },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Contains(t, err.Error(), "transient failure")
}

func TestPollUntilReady_BecomesReadyAfterRetries(t *testing.T) {
	t.Parallel()

	calls := 0

	err := fluxinstaller.PollUntilReady(
		context.Background(),
		5*time.Second,
		10*time.Millisecond,
		"test-resource",
		func() (bool, error) {
			calls++

			return calls >= 3, nil
		},
	)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 3)
}
