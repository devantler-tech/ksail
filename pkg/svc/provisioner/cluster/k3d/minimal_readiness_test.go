package k3dprovisioner_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/runner"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// These exercise the actual minimal factory and readiness clients, replacing
// only the container start command. TestMain already isolates the default home.
func TestMinimalK3sStartUsesPrivateKubeconfigAndTargetContext(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		context   string
		missing   bool
		forbidden bool
		wantReads bool
	}{
		{name: "private kubeconfig", context: "k3d-alpha", wantReads: true},
		{name: "wrong target context", context: "k3d-other"},
		{name: "missing explicit path", context: "k3d-alpha", missing: true},
		{name: "namespace forbidden", context: "k3d-alpha", forbidden: true, wantReads: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var readyCalls, namespaceCalls atomic.Int32

			server := newMinimalReadinessServer(t, testCase.forbidden, &readyCalls, &namespaceCalls)

			privatePath := writeMinimalReadinessKubeconfig(t, server.URL, testCase.context)
			alternative := writeMinimalReadinessKubeconfig(t, server.URL, "k3d-alpha")
			t.Setenv("KUBECONFIG", alternative)

			if testCase.missing {
				privatePath = filepath.Join(t.TempDir(), "absent-kubeconfig")
			}

			minimal, err := clusterprovisioner.CreateMinimalProvisioner(
				v1alpha1.DistributionK3s, "alpha", privatePath, v1alpha1.ProviderDocker,
			)
			require.NoError(t, err)

			provisioner, ok := minimal.(*k3dprovisioner.Provisioner)
			require.True(t, ok)

			mockRunner := runner.NewMockCommandRunner(t)
			mockRunner.EXPECT().Run(mock.Anything, mock.Anything, []string{"alpha"}).
				Return(runner.CommandResult{}, nil).Once()
			provisioner.WithRunnerForTest(mockRunner)

			timeout := 5 * time.Second
			if testCase.forbidden {
				timeout = 300 * time.Millisecond
			}

			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()

			err = provisioner.Start(ctx, "alpha")
			assertMinimalReadinessOutcome(t, testCase.wantReads, testCase.forbidden,
				err, readyCalls.Load(), namespaceCalls.Load())
		})
	}
}

func newMinimalReadinessServer(
	t *testing.T, forbidden bool, readyCalls, namespaceCalls *atomic.Int32,
) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")

			switch request.URL.Path {
			case "/readyz":
				readyCalls.Add(1)

				_, _ = writer.Write([]byte("ok"))
			case "/api/v1/namespaces":
				namespaceCalls.Add(1)

				if forbidden {
					writer.WriteHeader(http.StatusForbidden)
					_, _ = writer.Write(
						[]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure",` +
							`"reason":"Forbidden","message":"fixture denied namespaces","code":403}`),
					)

					return
				}

				_, _ = writer.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1","items":[]}`))
			default:
				writer.WriteHeader(http.StatusNotFound)
			}
		}),
	)
	t.Cleanup(server.Close)

	return server
}

func writeMinimalReadinessKubeconfig(t *testing.T, server, contextName string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster:
    server: %s
users:
- name: fixture
  user: {}
contexts:
- name: %s
  context:
    cluster: fixture
    user: fixture
current-context: %s
`, server, contextName, contextName)

	require.NoError(t, os.WriteFile(path, []byte(config), 0o600))

	return path
}

func assertMinimalReadinessOutcome(
	t *testing.T, wantReads, forbidden bool, err error, readyCalls, namespaceCalls int32,
) {
	t.Helper()

	if !wantReads {
		require.Error(t, err)
		require.Zero(t, readyCalls)
		require.Zero(t, namespaceCalls)

		return
	}

	if forbidden {
		require.ErrorIs(t, err, k8s.ErrClusterNotReady)
	} else {
		require.NoError(t, err)
	}

	require.Positive(t, readyCalls, "start must probe the explicit kubeconfig's API")
	require.Positive(t, namespaceCalls, "start must require an authorized namespace read")
}
