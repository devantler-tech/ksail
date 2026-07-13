package eksprovisioner_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

var errConnectorBoom = errors.New("connector boom")

// fakeAWSClusterAPI scripts the connector's AWS seam per test. A nil
// describeFunc fails with errConnectorBoom; a nil mintFunc returns a static
// happy-path token.
type fakeAWSClusterAPI struct {
	describeFunc func(name string) (*ekstypes.Cluster, error)
	mintFunc     func(clusterName string) (string, error)
}

func (f *fakeAWSClusterAPI) DescribeCluster(
	_ context.Context,
	name string,
) (*ekstypes.Cluster, error) {
	if f.describeFunc == nil {
		return nil, errConnectorBoom
	}

	return f.describeFunc(name)
}

func (f *fakeAWSClusterAPI) MintToken(_ context.Context, clusterName string) (string, error) {
	if f.mintFunc == nil {
		return "test-token", nil
	}

	return f.mintFunc(clusterName)
}

// connectorCAPEM is the raw CA material tests round-trip through the base64
// field the EKS API serves.
func connectorCAPEM() []byte { return []byte("test-ca-pem") }

func activeCluster(endpoint, caBase64 string) *ekstypes.Cluster {
	return &ekstypes.Cluster{
		Name:     aws.String("eks-default"),
		Status:   ekstypes.ClusterStatusActive,
		Endpoint: aws.String(endpoint),
		CertificateAuthority: &ekstypes.Certificate{
			Data: aws.String(caBase64),
		},
	}
}

// newConnectorProvisioner wires a Provisioner around the fake AWS seam. The
// eksctl client is a default one — the connector path never shells out.
func newConnectorProvisioner(
	t *testing.T,
	name string,
	fake *fakeAWSClusterAPI,
) *eksprovisioner.Provisioner {
	t.Helper()

	provisioner, err := eksprovisioner.NewProvisioner(
		name, "eu-central-1", "",
		eksctlclient.NewClient(),
		nil,
		eksprovisioner.WithAWSClusterAPI(fake),
	)
	require.NoError(t, err)

	return provisioner
}

func TestKubeconfigBuildsOperatorUsableConfig(t *testing.T) {
	t.Parallel()

	fake := &fakeAWSClusterAPI{
		describeFunc: func(_ string) (*ekstypes.Cluster, error) {
			return activeCluster(
				"https://203.0.113.10",
				base64.StdEncoding.EncodeToString(connectorCAPEM()),
			), nil
		},
		mintFunc: nil,
	}
	provisioner := newConnectorProvisioner(t, "eks-default", fake)

	raw, err := provisioner.Kubeconfig(t.Context(), "")
	require.NoError(t, err)

	config, err := clientcmd.Load(raw)
	require.NoError(t, err)

	contextName := "eks_eu-central-1_eks-default"
	assert.Equal(t, contextName, config.CurrentContext)
	require.Contains(t, config.Clusters, contextName)
	assert.Equal(t, "https://203.0.113.10", config.Clusters[contextName].Server)
	assert.Equal(t, connectorCAPEM(), config.Clusters[contextName].CertificateAuthorityData)
	require.Contains(t, config.AuthInfos, contextName)
	assert.Equal(t, "test-token", config.AuthInfos[contextName].Token)
}

func TestKubeconfigNotReadyWhileProvisioning(t *testing.T) {
	t.Parallel()

	caBase64 := base64.StdEncoding.EncodeToString(connectorCAPEM())

	testCases := []struct {
		name    string
		cluster *ekstypes.Cluster
	}{
		{
			name: "creating status",
			cluster: &ekstypes.Cluster{
				Name:     aws.String("eks-default"),
				Status:   ekstypes.ClusterStatusCreating,
				Endpoint: aws.String("https://203.0.113.10"),
				CertificateAuthority: &ekstypes.Certificate{
					Data: aws.String(caBase64),
				},
			},
		},
		{
			name:    "nil cluster payload",
			cluster: nil,
		},
		{
			name:    "active without endpoint",
			cluster: activeCluster("", caBase64),
		},
		{
			name:    "active without cluster CA",
			cluster: activeCluster("https://203.0.113.10", ""),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeAWSClusterAPI{
				describeFunc: func(_ string) (*ekstypes.Cluster, error) {
					return testCase.cluster, nil
				},
				mintFunc: nil,
			}
			provisioner := newConnectorProvisioner(t, "eks-default", fake)

			_, err := provisioner.Kubeconfig(t.Context(), "")
			require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
		})
	}
}

func TestKubeconfigRequiresClusterName(t *testing.T) {
	t.Parallel()

	provisioner := newConnectorProvisioner(t, "", &fakeAWSClusterAPI{
		describeFunc: nil,
		mintFunc:     nil,
	})

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorIs(t, err, eksprovisioner.ErrClusterNameRequired)
}

func TestKubeconfigFailsClosedOnIncompleteMappedCredentials(t *testing.T) {
	t.Parallel()

	provisioner, err := eksprovisioner.NewProvisioner(
		"eks-default",
		"eu-central-1",
		"",
		eksctlclient.NewClient(),
		nil,
		eksprovisioner.WithCredentialValues("", "access-without-secret", "", ""),
	)
	require.NoError(t, err)

	_, err = provisioner.Kubeconfig(t.Context(), "")
	require.ErrorIs(t, err, eksclient.ErrIncompleteStaticCredentials)
}

func TestKubeconfigPropagatesDescribeClusterError(t *testing.T) {
	t.Parallel()

	// describeFunc left nil: the fake's DescribeCluster fails with
	// errConnectorBoom.
	provisioner := newConnectorProvisioner(t, "eks-default", &fakeAWSClusterAPI{
		describeFunc: nil,
		mintFunc:     nil,
	})

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorIs(t, err, errConnectorBoom)
}

func TestKubeconfigPropagatesTokenMintError(t *testing.T) {
	t.Parallel()

	fake := &fakeAWSClusterAPI{
		describeFunc: func(_ string) (*ekstypes.Cluster, error) {
			return activeCluster(
				"https://203.0.113.10",
				base64.StdEncoding.EncodeToString(connectorCAPEM()),
			), nil
		},
		mintFunc: func(_ string) (string, error) {
			return "", errConnectorBoom
		},
	}
	provisioner := newConnectorProvisioner(t, "eks-default", fake)

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorIs(t, err, errConnectorBoom)
}

func TestKubeconfigRejectsMalformedClusterCA(t *testing.T) {
	t.Parallel()

	fake := &fakeAWSClusterAPI{
		describeFunc: func(_ string) (*ekstypes.Cluster, error) {
			return activeCluster("https://203.0.113.10", "not-base64!"), nil
		},
		mintFunc: nil,
	}
	provisioner := newConnectorProvisioner(t, "eks-default", fake)

	_, err := provisioner.Kubeconfig(t.Context(), "")
	require.ErrorContains(t, err, "decoding eks cluster CA certificate")
}
