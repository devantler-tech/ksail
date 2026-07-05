package gkeprovisioner_test

import (
	"encoding/base64"
	"testing"
	"time"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	gkeprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/gke"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"k8s.io/client-go/tools/clientcmd"
)

// failingTokenSource pins token-mint error propagation.
type failingTokenSource struct{}

func (failingTokenSource) Token() (*oauth2.Token, error) { return nil, errBoom }

// staticTokenSource is the happy-path token source tests inject.
func staticTokenSource() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})
}

// caPEM is the raw CA material tests round-trip through the base64 field the
// GKE API serves.
func caPEM() []byte { return []byte("test-ca-pem") }

func runningCluster(endpoint, caBase64 string) *containerpb.Cluster {
	return &containerpb.Cluster{
		Name:     "gke-default",
		Location: "europe-west1",
		Status:   containerpb.Cluster_RUNNING,
		Endpoint: endpoint,
		MasterAuth: &containerpb.MasterAuth{
			ClusterCaCertificate: caBase64,
		},
	}
}

// newConnectorProvisioner wires a Provisioner around the fake manager and
// token source, mirroring newProvisioner but exercising the Option seam.
func newConnectorProvisioner(
	t *testing.T,
	fake *fakeClusterManager,
	location string,
	source oauth2.TokenSource,
) *gkeprovisioner.Provisioner {
	t.Helper()

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(fake),
		gke.WithPollInterval(time.Millisecond),
	)
	require.NoError(t, err)

	provisioner, err := gkeprovisioner.NewProvisioner(
		"gke-default", "test-project", location, nil, client, nil,
		gkeprovisioner.WithTokenSource(source),
	)
	require.NoError(t, err)

	return provisioner
}

func TestKubeconfigBuildsOperatorUsableConfig(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		getFunc: func(_ *containerpb.GetClusterRequest) (*containerpb.Cluster, error) {
			return runningCluster(
				"203.0.113.10", base64.StdEncoding.EncodeToString(caPEM()),
			), nil
		},
	}
	provisioner := newConnectorProvisioner(t, fake, "europe-west1", staticTokenSource())

	raw, err := provisioner.Kubeconfig(t.Context(), "")
	require.NoError(t, err)

	config, err := clientcmd.Load(raw)
	require.NoError(t, err)

	contextName := "gke_test-project_europe-west1_gke-default"
	assert.Equal(t, contextName, config.CurrentContext)
	require.Contains(t, config.Clusters, contextName)
	assert.Equal(t, "https://203.0.113.10", config.Clusters[contextName].Server)
	assert.Equal(t, caPEM(), config.Clusters[contextName].CertificateAuthorityData)
	require.Contains(t, config.AuthInfos, contextName)
	assert.Equal(t, "test-token", config.AuthInfos[contextName].Token)
}

func TestKubeconfigNotReadyWhileProvisioning(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		cluster *containerpb.Cluster
	}{
		{
			name: "provisioning status",
			cluster: &containerpb.Cluster{
				Name:     "gke-default",
				Status:   containerpb.Cluster_PROVISIONING,
				Endpoint: "203.0.113.10",
				MasterAuth: &containerpb.MasterAuth{
					ClusterCaCertificate: base64.StdEncoding.EncodeToString(caPEM()),
				},
			},
		},
		{
			name:    "running without endpoint",
			cluster: runningCluster("", base64.StdEncoding.EncodeToString(caPEM())),
		},
		{
			name:    "running without cluster CA",
			cluster: runningCluster("203.0.113.10", ""),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeClusterManager{
				getFunc: func(_ *containerpb.GetClusterRequest) (*containerpb.Cluster, error) {
					return testCase.cluster, nil
				},
			}
			provisioner := newConnectorProvisioner(t, fake, "europe-west1", staticTokenSource())

			_, err := provisioner.Kubeconfig(t.Context(), "")
			require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
		})
	}
}

func TestKubeconfigClusterNotFoundAcrossLocations(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		listFunc: func(_ *containerpb.ListClustersRequest) (*containerpb.ListClustersResponse, error) {
			return listResponse(), nil
		},
	}
	provisioner := newConnectorProvisioner(t, fake, "", staticTokenSource())

	_, err := provisioner.Kubeconfig(t.Context(), "gke-default")
	require.ErrorIs(t, err, gkeprovisioner.ErrClusterNotFound)
}

func TestKubeconfigPropagatesGetClusterError(t *testing.T) {
	t.Parallel()

	// getFunc left nil: the fake's GetCluster fails with errBoom.
	provisioner := newConnectorProvisioner(
		t,
		&fakeClusterManager{},
		"europe-west1",
		staticTokenSource(),
	)

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorIs(t, err, errBoom)
}

func TestKubeconfigPropagatesTokenSourceError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		getFunc: func(_ *containerpb.GetClusterRequest) (*containerpb.Cluster, error) {
			return runningCluster(
				"203.0.113.10", base64.StdEncoding.EncodeToString(caPEM()),
			), nil
		},
	}
	provisioner := newConnectorProvisioner(t, fake, "europe-west1", failingTokenSource{})

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorIs(t, err, errBoom)
}

func TestKubeconfigRejectsMalformedClusterCA(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		getFunc: func(_ *containerpb.GetClusterRequest) (*containerpb.Cluster, error) {
			return runningCluster("203.0.113.10", "not-base64!"), nil
		},
	}
	provisioner := newConnectorProvisioner(t, fake, "europe-west1", staticTokenSource())

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorContains(t, err, "decoding gke cluster CA certificate")
}
