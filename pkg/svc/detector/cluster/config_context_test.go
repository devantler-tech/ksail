package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// configWithContext returns a kubeconfig holding one context whose cluster entry
// is clusterRef with the given server URL.
func configWithContext(contextName, clusterRef, server string) *clientcmdapi.Config {
	config := clientcmdapi.NewConfig()
	config.Clusters[clusterRef] = &clientcmdapi.Cluster{Server: server}
	config.Contexts[contextName] = &clientcmdapi.Context{Cluster: clusterRef}

	return config
}

//nolint:funlen // Table-driven test with one case per detection path.
func TestDetectDistributionFromConfigContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		config       *clientcmdapi.Config
		contextName  string
		distribution v1alpha1.Distribution
		clusterName  string
		wantErr      error
	}{
		{
			name:         "context name pattern",
			config:       configWithContext("kind-dev", "kind-dev", "https://127.0.0.1:6443"),
			contextName:  "kind-dev",
			distribution: v1alpha1.DistributionVanilla,
			clusterName:  "dev",
		},
		{
			name: "eksctl context",
			config: configWithContext(
				"ci@shop.eu-west-1.eksctl.io",
				"shop",
				"https://shop.example.com",
			),
			contextName:  "ci@shop.eu-west-1.eksctl.io",
			distribution: v1alpha1.DistributionEKS,
			clusterName:  "shop",
		},
		{
			name: "omni server URL",
			config: configWithContext(
				"acme-prod-sa", "prod", "https://acme.kubernetes.eu-1.omni.siderolabs.io",
			),
			contextName:  "acme-prod-sa",
			distribution: v1alpha1.DistributionTalos,
			clusterName:  "prod",
		},
		{
			name: "unrecognised context",
			config: configWithContext(
				"corp-prod",
				"corp-prod",
				"https://corp-prod.example.com",
			),
			contextName: "corp-prod",
			wantErr:     cluster.ErrUnknownContextPattern,
		},
		{
			name:        "empty cluster name in a known prefix",
			config:      configWithContext("kind-", "kind-", "https://127.0.0.1:6443"),
			contextName: "kind-",
			wantErr:     cluster.ErrEmptyClusterName,
		},
		{
			name: "context missing from the kubeconfig",
			config: configWithContext(
				"corp-prod",
				"corp-prod",
				"https://corp-prod.example.com",
			),
			contextName: "other",
			wantErr:     cluster.ErrContextNotFound,
		},
		{
			name:        "context's cluster missing from the kubeconfig",
			config:      danglingContextConfig("corp-prod"),
			contextName: "corp-prod",
			wantErr:     cluster.ErrClusterNotFound,
		},
		{
			name:        "nil kubeconfig",
			contextName: "corp-prod",
			wantErr:     cluster.ErrContextNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			distribution, clusterName, err := cluster.DetectDistributionFromConfigContext(
				test.config, test.contextName,
			)

			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.distribution, distribution)
			assert.Equal(t, test.clusterName, clusterName)
		})
	}
}

// danglingContextConfig returns a kubeconfig whose only context names a cluster entry
// the kubeconfig does not hold.
func danglingContextConfig(contextName string) *clientcmdapi.Config {
	config := clientcmdapi.NewConfig()
	config.Contexts[contextName] = &clientcmdapi.Context{Cluster: "absent"}

	return config
}
