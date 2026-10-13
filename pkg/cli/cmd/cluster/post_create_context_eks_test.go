package cluster_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	eksContextTestCluster = "st-eks"
	eksContextTestRegion  = "eu-west-1"
	// eksContextTestEndpoint is the API endpoint of the cluster KSail owns in these tests.
	eksContextTestEndpoint = "https://ABCDEF0123456789ABCDEF0123456789.gr7.eu-west-1.eks.amazonaws.com"
	// eksContextTestOtherEndpoint belongs to some other cluster of the same name and region.
	eksContextTestOtherEndpoint = "https://FEDCBA9876543210FEDCBA9876543210.gr7.eu-west-1.eks.amazonaws.com"
)

// eksContextFixture describes one kubeconfig context and where its cluster entry points.
// An empty server leaves the context without a cluster entry.
type eksContextFixture struct {
	name   string
	server string
}

// writeEKSContextKubeconfig writes the fixtures the way eksctl lays a kubeconfig out: a context per
// identity, each naming a cluster entry that holds the API endpoint.
func writeEKSContextKubeconfig(
	t *testing.T,
	currentContext string,
	fixtures ...eksContextFixture,
) string {
	t.Helper()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	config := clientcmdapi.NewConfig()
	config.CurrentContext = currentContext

	for _, fixture := range fixtures {
		kubeContext := &clientcmdapi.Context{}

		if fixture.server != "" {
			kubeContext.Cluster = "cluster-for-" + fixture.name
			config.Clusters[kubeContext.Cluster] = &clientcmdapi.Cluster{Server: fixture.server}
		}

		config.Contexts[fixture.name] = kubeContext
	}

	require.NoError(t, clientcmd.WriteToFile(*config, kubeconfigPath))

	return kubeconfigPath
}

func newImplicitEKSPostCreateContext(kubeconfigPath string) *localregistry.Context {
	return &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionEKS,
					Provider:     v1alpha1.ProviderAWS,
					Connection:   v1alpha1.Connection{Kubeconfig: kubeconfigPath},
				},
			},
		},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:   eksContextTestCluster,
			Region: eksContextTestRegion,
		},
		EKSEndpoint: eksContextTestEndpoint,
	}
}

//nolint:funlen // table keeps all fail-closed EKS selection cases together
func TestResolvePostCreateContext_EKS(t *testing.T) {
	t.Parallel()

	const (
		// eksctl names a context after the last path segment of the caller's ARN. An assumed
		// role contributes its session name, an IAM user its user name and the account root a
		// fixed word: never an ARN and never an account.
		sessionMatch = "ksail-system-test-eks-38400764426@st-eks.eu-west-1.eksctl.io"
		userMatch    = "operator@st-eks.eu-west-1.eksctl.io"
		rootMatch    = "iam-root-account@st-eks.eu-west-1.eksctl.io"
		unrelated    = "unrelated-context"
	)

	owned := func(name string) eksContextFixture {
		return eksContextFixture{name: name, server: eksContextTestEndpoint}
	}

	testCases := []struct {
		name           string
		currentContext string
		fixtures       []eksContextFixture
		wantContext    string
		wantErr        string
	}{
		{
			name:           "assumed-role session context is selected",
			currentContext: unrelated,
			fixtures:       []eksContextFixture{{name: unrelated}, owned(sessionMatch)},
			wantContext:    sessionMatch,
		},
		{
			name:           "IAM user context is selected",
			currentContext: unrelated,
			fixtures:       []eksContextFixture{{name: unrelated}, owned(userMatch)},
			wantContext:    userMatch,
		},
		{
			name:           "account root context is selected",
			currentContext: unrelated,
			fixtures:       []eksContextFixture{{name: unrelated}, owned(rootMatch)},
			wantContext:    rootMatch,
		},
		{
			name:           "current context wins among multiple matches",
			currentContext: userMatch,
			fixtures:       []eksContextFixture{owned(sessionMatch), owned(userMatch)},
			wantContext:    userMatch,
		},
		{
			name:           "endpoint match is case-insensitive and ignores a trailing slash",
			currentContext: unrelated,
			fixtures: []eksContextFixture{{
				name:   sessionMatch,
				server: "https://abcdef0123456789abcdef0123456789.gr7.eu-west-1.eks.amazonaws.com/",
			}},
			wantContext: sessionMatch,
		},
		{
			name:           "current context of another cluster with the same name is never selected",
			currentContext: userMatch,
			fixtures: []eksContextFixture{
				{name: userMatch, server: eksContextTestOtherEndpoint},
				owned(sessionMatch),
			},
			wantContext: sessionMatch,
		},
		{
			name:           "only another cluster with the same name fails closed",
			currentContext: sessionMatch,
			fixtures: []eksContextFixture{
				{name: sessionMatch, server: eksContextTestOtherEndpoint},
			},
			wantErr: `no kubeconfig context matches EKS cluster "st-eks" in region "eu-west-1"`,
		},
		{
			name:           "context without a cluster entry fails closed",
			currentContext: sessionMatch,
			fixtures:       []eksContextFixture{{name: sessionMatch}},
			wantErr:        `no kubeconfig context matches EKS cluster "st-eks" in region "eu-west-1"`,
		},
		{
			name:           "plain HTTP endpoint fails closed",
			currentContext: sessionMatch,
			fixtures: []eksContextFixture{{
				name:   sessionMatch,
				server: "http://ABCDEF0123456789ABCDEF0123456789.gr7.eu-west-1.eks.amazonaws.com",
			}},
			wantErr: `no kubeconfig context matches EKS cluster "st-eks" in region "eu-west-1"`,
		},
		{
			name:           "missing match fails closed",
			currentContext: unrelated,
			fixtures:       []eksContextFixture{{name: unrelated, server: eksContextTestEndpoint}},
			wantErr:        `no kubeconfig context matches EKS cluster "st-eks" in region "eu-west-1"`,
		},
		{
			name:           "ambiguous matches fail closed",
			currentContext: unrelated,
			fixtures: []eksContextFixture{
				owned(userMatch), {name: unrelated}, owned(sessionMatch),
			},
			wantErr: `multiple kubeconfig contexts match EKS cluster "st-eks" in region "eu-west-1"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := newImplicitEKSPostCreateContext(
				writeEKSContextKubeconfig(t, testCase.currentContext, testCase.fixtures...),
			)

			err := cluster.ExportResolvePostCreateContext(ctx)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				require.ErrorContains(t, err, "set spec.cluster.connection.context explicitly")
				assert.Empty(t, ctx.ClusterCfg.Spec.Cluster.Connection.Context)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantContext, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
		})
	}
}

func TestResolvePostCreateContext_EKSExplicitContextIsPreserved(t *testing.T) {
	t.Parallel()

	ctx := newImplicitEKSPostCreateContext(filepath.Join(t.TempDir(), "absent"))
	ctx.ClusterCfg.Spec.Cluster.Connection.Context = "operator-selected-context"

	require.NoError(t, cluster.ExportResolvePostCreateContext(ctx))
	assert.Equal(
		t,
		"operator-selected-context",
		ctx.ClusterCfg.Spec.Cluster.Connection.Context,
	)
}

// Without a verified endpoint and without credentials to read one, no context may be selected:
// the name alone cannot tell the owned cluster from another of the same name and region.
func TestResolvePostCreateContext_EKSWithoutVerifiedEndpointFailsClosed(t *testing.T) {
	t.Parallel()

	const sessionMatch = "ksail-system-test-eks-38400764426@st-eks.eu-west-1.eksctl.io"

	ctx := newImplicitEKSPostCreateContext(writeEKSContextKubeconfig(
		t,
		sessionMatch,
		eksContextFixture{name: sessionMatch, server: eksContextTestEndpoint},
	))
	ctx.EKSEndpoint = ""

	err := cluster.ExportResolvePostCreateContext(ctx)
	require.ErrorContains(t, err, "EKS configuration is unavailable")
	require.ErrorContains(t, err, "set spec.cluster.connection.context explicitly")
	assert.Empty(t, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
}
