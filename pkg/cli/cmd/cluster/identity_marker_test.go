package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/buildmeta"
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterpkg "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusteridentity"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

var errConnectionRefused = errors.New("connection refused")

func identityMarkerCluster(t *testing.T) *v1alpha1.Cluster {
	t.Helper()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = filepath.Join(t.TempDir(), "kubeconfig")

	return clusterCfg
}

func identityMarkerCmd() (*cobra.Command, *bytes.Buffer) {
	var stderr bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetErr(&stderr)

	return cmd, &stderr
}

// TestWriteClusterIdentityMarkerRecordsTheProvisionedCluster checks the create/update hook: it
// connects to the cluster's own context, records the provisioned identity with the default
// provider filled in, and binds it to the live kube-system UID.
func TestWriteClusterIdentityMarkerRecordsTheProvisionedCluster(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(&corev1.Namespace{
		Name: "kube-system", UID: "live-uid",
	})
	clusterCfg := identityMarkerCluster(t)
	cmd, stderr := identityMarkerCmd()

	var gotContext string

	clusterpkg.ExportWriteClusterIdentityMarker(cmd, "dev", clusterCfg,
		func(_, kubeContext string) (kubernetes.Interface, error) {
			gotContext = kubeContext

			return client, nil
		})

	require.Empty(t, stderr.String())
	require.Equal(
		t,
		"kind-dev",
		gotContext,
		"the marker is written into the cluster just provisioned",
	)

	configMap, err := client.CoreV1().ConfigMaps("kube-system").
		Get(context.Background(), clusteridentity.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		clusteridentity.KeyClusterName:   "dev",
		clusteridentity.KeyDistribution:  "Vanilla",
		clusteridentity.KeyProvider:      "Docker",
		clusteridentity.KeyKSailVersion:  buildmeta.Version,
		clusteridentity.KeyKubeSystemUID: "live-uid",
	}, configMap.Data)
}

// TestWriteClusterIdentityMarkerOnlyWarns keeps the marker metadata: a cluster that cannot be
// reached, or one without a kube-system UID, produces a warning and nothing else.
func TestWriteClusterIdentityMarkerOnlyWarns(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		newClient func(string, string) (kubernetes.Interface, error)
		want      string
	}{
		{
			name: "unreachable",
			newClient: func(string, string) (kubernetes.Interface, error) {
				return nil, errConnectionRefused
			},
			want: "connection refused",
		},
		{
			name: "no-kube-system-uid",
			newClient: func(string, string) (kubernetes.Interface, error) {
				return fake.NewClientset(&corev1.Namespace{
					Name: "kube-system",
				}), nil
			},
			want: "no UID",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, stderr := identityMarkerCmd()

			require.NotPanics(t, func() {
				clusterpkg.ExportWriteClusterIdentityMarker(
					cmd, "dev", identityMarkerCluster(t), testCase.newClient,
				)
			})
			require.Contains(t, stderr.String(), "failed to write the cluster identity marker")
			require.Contains(t, stderr.String(), testCase.want)
		})
	}
}

// useFakeIdentityMarker serves the identity marker from an in-memory cluster API for command
// tests whose fake provisioner creates no real cluster.
func useFakeIdentityMarker(t *testing.T) *fake.Clientset {
	t.Helper()

	client := fake.NewClientset(&corev1.Namespace{
		Name: "kube-system", UID: "fake-kube-system-uid",
	})

	t.Cleanup(clusterpkg.SetIdentityMarkerClientForTests(
		func(string, string) (kubernetes.Interface, error) { return client, nil },
	))

	return client
}

// TestCreateWritesTheIdentityMarker drives `cluster create` end to end with a fake provisioner
// and checks that the created cluster carries the marker for the spec KSail provisioned.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreateWritesTheIdentityMarker(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)
	t.Cleanup(clusterpkg.SetProvisionerFactoryForTests(fakeFactory{}))

	client := useFakeIdentityMarker(t)

	cmd := clusterpkg.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute(), out.String())
	require.NotContains(t, out.String(), "identity marker")

	configMap, err := client.CoreV1().ConfigMaps("kube-system").
		Get(context.Background(), clusteridentity.Name, metav1.GetOptions{})
	require.NoError(t, err, "create must leave the identity marker")
	require.Equal(t, "fake-kube-system-uid", configMap.Data[clusteridentity.KeyKubeSystemUID])
	require.Equal(t, "Vanilla", configMap.Data[clusteridentity.KeyDistribution])
	require.Equal(t, "Docker", configMap.Data[clusteridentity.KeyProvider])
	require.NotEmpty(t, configMap.Data[clusteridentity.KeyClusterName])
}

var errUpdateFailed = errors.New("update failed")

// TestUpdateRefreshesTheIdentityMarker covers the update path: an applied update rewrites a
// stale marker, while a dry run, a declined prompt or a failed update leaves it untouched.
//
//nolint:paralleltest // mutates the shared identity-marker client seam
func TestUpdateRefreshesTheIdentityMarker(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		applied   bool
		updateErr error
		refreshed bool
	}{
		{name: "applied", applied: true, refreshed: true},
		{name: "dry-run-or-declined"},
		{name: "failed", applied: true, updateErr: errUpdateFailed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := useFakeIdentityMarker(t)
			_, err := client.CoreV1().ConfigMaps("kube-system").Create(context.Background(),
				&corev1.ConfigMap{
					Name: clusteridentity.Name, Namespace: "kube-system",
					Data: map[string]string{clusteridentity.KeyKSailVersion: "v0.0.1-stale"},
				}, metav1.CreateOptions{})
			require.NoError(t, err)

			cmd, stderr := identityMarkerCmd()
			err = clusterpkg.ExportRecordIdentityAfterUpdate(cmd, "dev", identityMarkerCluster(t),
				func() (bool, error) { return testCase.applied, testCase.updateErr })
			require.ErrorIs(t, err, testCase.updateErr)
			require.Empty(t, stderr.String())

			configMap, err := client.CoreV1().ConfigMaps("kube-system").
				Get(context.Background(), clusteridentity.Name, metav1.GetOptions{})
			require.NoError(t, err)

			if testCase.refreshed {
				require.Equal(t, buildmeta.Version, configMap.Data[clusteridentity.KeyKSailVersion])
				require.Equal(
					t,
					"fake-kube-system-uid",
					configMap.Data[clusteridentity.KeyKubeSystemUID],
				)

				return
			}

			require.Equal(t, "v0.0.1-stale", configMap.Data[clusteridentity.KeyKSailVersion])
		})
	}
}

// TestWriteClusterIdentityMarkerUsesTheNestedProvisionerContext pins the context the marker
// connects to when none is configured: the one the provisioner writes. Real CI on ksail#8194
// showed a nested vCluster written as "vcluster-nested-vcluster" while the marker looked for the
// Docker driver's "vcluster-docker_nested-vcluster".
func TestWriteClusterIdentityMarkerUsesTheNestedProvisionerContext(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		configured   string
		want         string
	}{
		{
			name:         "nested-vcluster",
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderKubernetes,
			want:         "vcluster-nested",
		},
		{
			name:         "nested-k3s",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderKubernetes,
			want:         "k3k-nested",
		},
		{
			name:         "docker-vcluster",
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderDocker,
			want:         "vcluster-docker_nested",
		},
		{
			name:         "configured-context-wins",
			distribution: v1alpha1.DistributionVCluster,
			provider:     v1alpha1.ProviderKubernetes,
			configured:   "my-context",
			want:         "my-context",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := identityMarkerCluster(t)
			clusterCfg.Spec.Cluster.Distribution = testCase.distribution
			clusterCfg.Spec.Cluster.Provider = testCase.provider
			clusterCfg.Spec.Cluster.Connection.Context = testCase.configured
			cmd, _ := identityMarkerCmd()

			var gotContext string

			clusterpkg.ExportWriteClusterIdentityMarker(cmd, "nested", clusterCfg,
				func(_, kubeContext string) (kubernetes.Interface, error) {
					gotContext = kubeContext

					return nil, errConnectionRefused
				})

			require.Equal(t, testCase.want, gotContext)
		})
	}
}
