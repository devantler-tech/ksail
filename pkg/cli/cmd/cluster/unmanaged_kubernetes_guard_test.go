package cluster_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

//nolint:paralleltest // isolates default-provider credentials and host option aliases.
func TestUnmanagedClusterGuard_KubernetesOwnership(t *testing.T) {
	isolateNestedGuardProviders(t)

	tests := []struct {
		name      string
		namespace string
		labels    map[string]string
		wantOwned bool
	}{
		{"kind", "ksail-nested", nestedGuardOwnedLabels(), true},
		{"k3s", "k3k-nested", nestedGuardOwnedLabels(), true},
		{"vcluster", "vcluster-nested", nestedGuardOwnedLabels(), true},
		{"missing labels", "ksail-nested", nil, false},
		{"wrong manager", "ksail-nested", map[string]string{
			"ksail.io/managed-by": "another-tool", "ksail.io/cluster": "nested",
		}, false},
		{"missing manager", "ksail-nested", map[string]string{"ksail.io/cluster": "nested"}, false},
		{"missing name", "ksail-nested", map[string]string{"ksail.io/managed-by": "ksail"}, false},
		{"wrong name", "ksail-nested", map[string]string{
			"ksail.io/managed-by": "ksail", "ksail.io/cluster": "other",
		}, false},
		{"different namespace", "ksail-other", nestedGuardOwnedLabels(), false},
		{"absent namespace", "", nil, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			resolved, hostReads := nestedGuardHost(
				t,
				testCase.namespace,
				testCase.labels,
				http.StatusOK,
			)
			err := cluster.ExportUnmanagedClusterGuard(t.Context(), resolved)
			if testCase.wantOwned {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
			}
			assert.Positive(t, hostReads.Load(), "ownership must be verified on the selected host")
		})
	}
}

//nolint:paralleltest // changes environment-variable aliases for the host connection.
func TestUnmanagedClusterGuard_KubernetesHostAliases(t *testing.T) {
	isolateNestedGuardProviders(t)
	resolved, hostReads := nestedGuardHost(
		t,
		"ksail-nested",
		nestedGuardOwnedLabels(),
		http.StatusOK,
	)
	t.Setenv("KSAIL_TEST_NESTED_HOST_FILE", resolved.KubernetesOpts.Kubeconfig)
	t.Setenv("KSAIL_TEST_NESTED_HOST_CONTEXT", "host")
	resolved.KubernetesOpts = v1alpha1.OptionsKubernetes{
		Kubeconfig:       "/invalid/direct/fallback",
		KubeconfigEnvVar: "KSAIL_TEST_NESTED_HOST_FILE",
		Context:          "invalid-direct-context",
		ContextEnvVar:    "KSAIL_TEST_NESTED_HOST_CONTEXT",
	}

	require.NoError(t, cluster.ExportUnmanagedClusterGuard(t.Context(), resolved))
	assert.Positive(t, hostReads.Load())
}

//nolint:paralleltest // keeps the production guard isolated from real provider credentials.
func TestUnmanagedClusterGuard_KubernetesHostFailures(t *testing.T) {
	isolateNestedGuardProviders(t)

	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			resolved, hostReads := nestedGuardHost(
				t,
				"ksail-nested",
				nestedGuardOwnedLabels(),
				status,
			)
			err := cluster.ExportUnmanagedClusterGuard(t.Context(), resolved)
			require.Error(t, err)
			require.NotErrorIs(t, err, cluster.ErrUnmanagedCluster)
			assert.Positive(t, hostReads.Load())
		})
	}

	t.Run("missing host kubeconfig", func(t *testing.T) {
		resolved, hostReads := nestedGuardHost(
			t,
			"ksail-nested",
			nestedGuardOwnedLabels(),
			http.StatusOK,
		)
		resolved.KubernetesOpts.Kubeconfig = filepath.Join(t.TempDir(), "missing")
		err := cluster.ExportUnmanagedClusterGuard(t.Context(), resolved)
		require.Error(t, err)
		require.NotErrorIs(t, err, cluster.ErrUnmanagedCluster)
		assert.Zero(t, hostReads.Load())
	})

	t.Run("unknown host context", func(t *testing.T) {
		resolved, hostReads := nestedGuardHost(
			t,
			"ksail-nested",
			nestedGuardOwnedLabels(),
			http.StatusOK,
		)
		resolved.KubernetesOpts.Context = "unknown"
		err := cluster.ExportUnmanagedClusterGuard(t.Context(), resolved)
		require.Error(t, err)
		require.NotErrorIs(t, err, cluster.ErrUnmanagedCluster)
		assert.Zero(t, hostReads.Load())
	})
}

//nolint:paralleltest // isolates credentials used by the unchanged default-provider guard.
func TestUnmanagedClusterGuard_KubernetesAbsentTargetIsIdempotent(t *testing.T) {
	isolateNestedGuardProviders(t)
	resolved, hostReads := nestedGuardHost(t, "", nil, http.StatusOK)
	resolved.KubeconfigPath = filepath.Join(t.TempDir(), "missing-child-config")

	require.NoError(t, cluster.ExportUnmanagedClusterGuard(t.Context(), resolved))
	assert.EqualValues(t, 3, hostReads.Load())
}

func TestKubernetesCleanup_NamespaceReadFailureStopsDeletion(t *testing.T) {
	t.Parallel()
	resolved, hostReads := nestedGuardHost(
		t,
		"ksail-nested",
		nestedGuardOwnedLabels(),
		http.StatusForbidden,
	)
	provisioner, err := lifecycle.CreateMinimalProvisionerForProvider(
		t.Context(),
		&clusterdetector.Info{
			ClusterName: resolved.ClusterName, Provider: v1alpha1.ProviderKubernetes,
			KubeconfigPath: resolved.KubeconfigPath,
		},
		lifecycle.MinimalProvisionerOptions{KubernetesOpts: resolved.KubernetesOpts},
	)
	require.NoError(t, err)

	require.Error(t, provisioner.Delete(t.Context(), resolved.ClusterName))
	assert.EqualValues(
		t,
		1,
		hostReads.Load(),
		"a failed ownership read must stop cleanup immediately",
	)
	kubeconfig, err := clientcmd.LoadFromFile(resolved.KubeconfigPath)
	require.NoError(t, err)
	assert.Contains(
		t,
		kubeconfig.Contexts,
		"kind-nested",
		"failed cleanup must preserve the child context",
	)
}

func TestKubernetesCleanup_DeletesOnlyVerifiedNamespace(t *testing.T) {
	t.Parallel()
	var deletes atomic.Int32
	host := nestedCleanupHost(t, &deletes)
	kubeconfig := filepath.Join(t.TempDir(), "host-config")
	require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"host": {Server: host.URL}},
		Contexts:       map[string]*clientcmdapi.Context{"host": {Cluster: "host"}},
		CurrentContext: "host",
	}, kubeconfig))
	nestedKubeconfig := writeKubeconfigWithContext(t, t.TempDir(), "kind-nested")
	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName: "nested", Provider: v1alpha1.ProviderKubernetes,
		KubeconfigPath: nestedKubeconfig,
		KubernetesOpts: v1alpha1.OptionsKubernetes{Kubeconfig: kubeconfig, Context: "host"},
	}
	require.NoError(t, cluster.ExportUnmanagedClusterGuard(t.Context(), resolved))
	provisioner, err := lifecycle.CreateMinimalProvisionerForProvider(
		t.Context(),
		&clusterdetector.Info{
			ClusterName: resolved.ClusterName, Provider: resolved.Provider,
			KubeconfigPath: nestedKubeconfig,
		},
		lifecycle.MinimalProvisionerOptions{KubernetesOpts: resolved.KubernetesOpts},
	)
	require.NoError(t, err)
	require.NoError(t, provisioner.Delete(t.Context(), resolved.ClusterName))
	assert.EqualValues(t, 1, deletes.Load())
	config, err := clientcmd.LoadFromFile(nestedKubeconfig)
	require.NoError(t, err)
	assert.NotContains(t, config.Contexts, "kind-nested")
}

func nestedCleanupHost(t *testing.T, deletes *atomic.Int32) *httptest.Server {
	t.Helper()
	host := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			if request.URL.Path != "/api/v1/namespaces/ksail-nested" {
				writer.WriteHeader(http.StatusNotFound)
				assert.NoError(
					t,
					json.NewEncoder(writer).Encode(metav1.Status{Code: http.StatusNotFound}),
				)
				return
			}
			if request.Method == http.MethodDelete {
				deletes.Add(1)
				var options metav1.DeleteOptions
				body, err := io.ReadAll(request.Body)
				assert.NoError(t, err)
				assert.NoError(
					t,
					runtime.DecodeInto(scheme.Codecs.UniversalDeserializer(), body, &options),
				)
				assert.Equal(t, &metav1.Preconditions{
					UID: new(types.UID("owned-uid")), ResourceVersion: new("42"),
				}, options.Preconditions, "deletion must pin the namespace version whose ownership was checked")
				assert.NoError(t, json.NewEncoder(writer).Encode(metav1.Status{Status: "Success"}))
				return
			}
			assert.Equal(t, http.MethodGet, request.Method)
			assert.NoError(t, json.NewEncoder(writer).Encode(corev1.Namespace{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
				ObjectMeta: metav1.ObjectMeta{
					Name: "ksail-nested", Labels: nestedGuardOwnedLabels(),
					UID: "owned-uid", ResourceVersion: "42",
				},
			}))
		}),
	)
	t.Cleanup(host.Close)

	return host
}

func isolateNestedGuardProviders(t *testing.T) {
	t.Helper()
	docker := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}),
	)
	t.Cleanup(docker.Close)
	t.Setenv("HCLOUD_TOKEN", "")
	t.Setenv("OMNI_ENDPOINT", "")
	t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", "")
	t.Setenv("DOCKER_HOST", strings.Replace(docker.URL, "http://", "tcp://", 1))
	t.Setenv("KSAIL_HOST_KUBECONFIG", "/invalid/default/host")
	t.Setenv("KSAIL_HOST_CONTEXT", "invalid-default-host")
}

func nestedGuardOwnedLabels() map[string]string {
	return map[string]string{"ksail.io/managed-by": "ksail", "ksail.io/cluster": "nested"}
}

func nestedGuardHost(
	t *testing.T,
	ownedNamespace string,
	labels map[string]string,
	status int,
) (*lifecycle.ResolvedClusterInfo, *atomic.Int32) {
	t.Helper()
	reads := &atomic.Int32{}
	host := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			reads.Add(1)
			assert.Equal(t, http.MethodGet, request.Method, "the guard must never mutate the host")
			writer.Header().Set("Content-Type", "application/json")
			name := strings.TrimPrefix(request.URL.Path, "/api/v1/namespaces/")
			if status != http.StatusOK || name != ownedNamespace {
				responseStatus := status
				if responseStatus == http.StatusOK {
					responseStatus = http.StatusNotFound
				}
				writer.WriteHeader(responseStatus)
				assert.NoError(t, json.NewEncoder(writer).Encode(metav1.Status{
					TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
					Status:   "Failure", Code: int32(responseStatus),
					Reason: metav1.StatusReason(http.StatusText(responseStatus)),
				}))
				return
			}
			assert.NoError(t, json.NewEncoder(writer).Encode(corev1.Namespace{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
				ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
			}))
		}),
	)
	t.Cleanup(host.Close)

	child := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Error("ownership must not query the current child context")
		writer.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(child.Close)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"host": {Server: host.URL}, "child": {Server: child.URL},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"host": {Cluster: "host"}, "kind-nested": {Cluster: "child"},
		},
		CurrentContext: "kind-nested",
	}, kubeconfig))

	return &lifecycle.ResolvedClusterInfo{
		ClusterName: "nested", Provider: v1alpha1.ProviderKubernetes,
		KubeconfigPath: kubeconfig,
		KubernetesOpts: v1alpha1.OptionsKubernetes{Kubeconfig: kubeconfig, Context: "host"},
	}, reads
}
