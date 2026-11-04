package clusteridentity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/clusteridentity"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const liveUID = types.UID("5f0c7c2e-live-kube-system")

var errAPIUnavailable = errors.New("api unavailable")

func kubeSystem(uid types.UID) *corev1.Namespace {
	return &corev1.Namespace{Name: "kube-system", UID: uid}
}

func marker() clusteridentity.Marker {
	return clusteridentity.Marker{
		ClusterName:  "dev",
		Distribution: "Vanilla",
		Provider:     "Docker",
		KSailVersion: "v7.1.0",
	}
}

func readMarker(t *testing.T, client *fake.Clientset) *corev1.ConfigMap {
	t.Helper()

	configMap, err := client.CoreV1().ConfigMaps("kube-system").
		Get(context.Background(), clusteridentity.Name, metav1.GetOptions{})
	require.NoError(t, err)

	return configMap
}

func TestWriteCreatesMarkerBoundToLiveKubeSystemUID(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(kubeSystem(liveUID))

	require.NoError(t, clusteridentity.Write(context.Background(), client, marker()))

	configMap := readMarker(t, client)
	require.Equal(t, map[string]string{
		clusteridentity.KeyClusterName:   "dev",
		clusteridentity.KeyDistribution:  "Vanilla",
		clusteridentity.KeyProvider:      "Docker",
		clusteridentity.KeyKSailVersion:  "v7.1.0",
		clusteridentity.KeyKubeSystemUID: string(liveUID),
	}, configMap.Data)
	require.Equal(t, "ksail", configMap.Labels["app.kubernetes.io/managed-by"])
}

// TestWriteReplacesStaleOrCopiedMarker refreshes a marker restored from another cluster: every
// field, the UID included, is rewritten from the live cluster, and unknown keys are dropped.
func TestWriteReplacesStaleOrCopiedMarker(t *testing.T) {
	t.Parallel()

	stale := &corev1.ConfigMap{
		Name: clusteridentity.Name, Namespace: "kube-system",
		Labels: map[string]string{"team": "platform"},
		Data: map[string]string{
			clusteridentity.KeyClusterName:   "old",
			clusteridentity.KeyKSailVersion:  "v6.0.0",
			clusteridentity.KeyKubeSystemUID: "uid-of-another-cluster",
			"leftover":                       "x",
		},
	}
	client := fake.NewClientset(kubeSystem(liveUID), stale)

	require.NoError(t, clusteridentity.Write(context.Background(), client, marker()))

	configMap := readMarker(t, client)
	require.Equal(t, string(liveUID), configMap.Data[clusteridentity.KeyKubeSystemUID])
	require.Equal(t, "dev", configMap.Data[clusteridentity.KeyClusterName])
	require.Equal(t, "v7.1.0", configMap.Data[clusteridentity.KeyKSailVersion])
	require.NotContains(t, configMap.Data, "leftover")
	require.Equal(t, "platform", configMap.Labels["team"], "existing labels are kept")
	require.Equal(t, "ksail", configMap.Labels["app.kubernetes.io/managed-by"])
}

func TestWriteRefusesWithoutABindableUID(t *testing.T) {
	t.Parallel()

	t.Run("namespace-unreadable", func(t *testing.T) {
		t.Parallel()

		client := fake.NewClientset()
		err := clusteridentity.Write(context.Background(), client, marker())
		require.ErrorContains(t, err, "read the kube-system namespace")
		requireNoMarker(t, client)
	})

	t.Run("namespace-without-uid", func(t *testing.T) {
		t.Parallel()

		client := fake.NewClientset(kubeSystem(""))
		err := clusteridentity.Write(context.Background(), client, marker())
		require.ErrorIs(t, err, clusteridentity.ErrNoKubeSystemUID)
		requireNoMarker(t, client)
	})
}

func TestWriteReportsAPIFailures(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"get", "create"} {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			client := fake.NewClientset(kubeSystem(liveUID))
			client.PrependReactor(verb, "configmaps",
				func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errAPIUnavailable
				})

			err := clusteridentity.Write(context.Background(), client, marker())
			require.ErrorContains(t, err, "api unavailable")
		})
	}
}

func requireNoMarker(t *testing.T, client *fake.Clientset) {
	t.Helper()

	_, err := client.CoreV1().ConfigMaps("kube-system").
		Get(context.Background(), clusteridentity.Name, metav1.GetOptions{})
	require.Error(t, err, "no marker may be written without a bindable UID")
}
