package cluster_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestDetectInfoEksctlContext(t *testing.T) {
	t.Parallel()

	for _, contextName := range []string{
		"arn:aws:iam::123456789012:role/operator@demo.us-west-2.eksctl.io",
		"user@example.com@demo.us-west-2.eksctl.io",
		"admin@demo.us-west-2.eksctl.io",
		"demo.us-west-2.eksctl.io",
		"kind-demo.us-west-2.eksctl.io",
	} {
		t.Run(contextName, func(t *testing.T) {
			t.Parallel()

			config := clientcmdapi.NewConfig()
			config.CurrentContext = contextName
			config.Contexts[contextName] = &clientcmdapi.Context{
				Cluster: "opaque-cluster-reference",
			}
			config.Clusters["opaque-cluster-reference"] = &clientcmdapi.Cluster{
				Server: "https://example.us-west-2.eks.amazonaws.com",
			}
			kubeconfigPath := filepath.Join(t.TempDir(), "config")
			require.NoError(t, clientcmd.WriteToFile(*config, kubeconfigPath))

			info, err := cluster.DetectInfo(t.Context(), kubeconfigPath, "")
			require.NoError(t, err)
			assert.Equal(t, v1alpha1.DistributionEKS, info.Distribution)
			assert.Equal(t, v1alpha1.ProviderAWS, info.Provider)
			wantName := "demo"
			if contextName == "kind-demo.us-west-2.eksctl.io" {
				wantName = "kind-demo"
			}
			assert.Equal(t, wantName, info.ClusterName)
			assert.Equal(t, contextName, info.Context)
		})
	}
}

func TestDetectDistributionRejectsMalformedEksctlContext(t *testing.T) {
	t.Parallel()

	for _, contextName := range []string{
		"demo.eksctl.io",
		"admin@demo.eksctl.io",
		"demo.us.west-2.eksctl.io",
		".us-west-2.eksctl.io",
		"demo..eksctl.io",
	} {
		t.Run(contextName, func(t *testing.T) {
			t.Parallel()

			_, _, err := cluster.DetectDistributionFromContext(contextName)
			require.ErrorIs(t, err, cluster.ErrUnknownContextPattern)
		})
	}
}
