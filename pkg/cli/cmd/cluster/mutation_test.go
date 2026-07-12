package cluster_test

import (
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestAllDistributions(t *testing.T) {
	t.Parallel()

	dists := cluster.ExportAllDistributions()
	assert.NotEmpty(t, dists)
	assert.GreaterOrEqual(t, len(dists), 4)
}

func TestAllProviders(t *testing.T) {
	t.Parallel()

	providers := cluster.ExportAllProviders()
	assert.NotEmpty(t, providers)
	assert.GreaterOrEqual(t, len(providers), 3)
}

// writeTestKubeconfig writes a kubeconfig with the given contexts to a temp
// dir and returns its path.
func writeTestKubeconfig(t *testing.T, currentContext string, contexts ...string) string {
	t.Helper()

	config := clientcmdapi.NewConfig()
	config.CurrentContext = currentContext

	for _, name := range contexts {
		config.Contexts[name] = clientcmdapi.NewContext()
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(*config, path))

	return path
}

// newEKSContext builds a localregistry.Context for an EKS cluster whose
// kubeconfig is the given path.
func newEKSContext(kubeconfigPath, explicitContext, region string) *localregistry.Context {
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath
	clusterCfg.Spec.Cluster.Connection.Context = explicitContext

	return &localregistry.Context{
		ClusterCfg: clusterCfg,
		EKSConfig:  &clusterprovisioner.EKSConfig{Name: "my-cluster", Region: region},
	}
}

func TestResolveCreatedContextPreservesExplicitContext(t *testing.T) {
	t.Parallel()

	// No kubeconfig exists at this path; an explicit context must be returned
	// unchanged without any kubeconfig read.
	ctx := newEKSContext(
		filepath.Join(t.TempDir(), "missing"),
		"admin@my-cluster.eu-north-1.eksctl.io",
		"eu-north-1",
	)

	resolved, err := cluster.ExportResolveCreatedContext(ctx, "my-cluster")
	require.NoError(t, err)
	assert.Equal(t, "admin@my-cluster.eu-north-1.eksctl.io", resolved)
}

func TestResolveCreatedContextNonEKSUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		want         string
	}{
		{
			name:         "vanilla kind context",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			want:         "kind-my-cluster",
		},
		{
			name:         "k3s on kubernetes provider uses k3k prefix",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderKubernetes,
			want:         "k3k-my-cluster",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{}
			clusterCfg.Spec.Cluster.Distribution = testCase.distribution
			clusterCfg.Spec.Cluster.Provider = testCase.provider

			resolved, err := cluster.ExportResolveCreatedContext(
				&localregistry.Context{ClusterCfg: clusterCfg},
				"my-cluster",
			)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, resolved)
		})
	}
}

func TestResolveCreatedContextEKSReadsKubeconfig(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeTestKubeconfig(
		t,
		"admin@my-cluster.eu-north-1.eksctl.io",
		"admin@my-cluster.eu-north-1.eksctl.io",
	)
	ctx := newEKSContext(kubeconfigPath, "", "eu-north-1")

	resolved, err := cluster.ExportResolveCreatedContext(ctx, "my-cluster")
	require.NoError(t, err)
	assert.Equal(t, "admin@my-cluster.eu-north-1.eksctl.io", resolved)
}

// eksContextCase is one TestResolveEKSCreatedContext table entry.
type eksContextCase struct {
	name           string
	currentContext string
	contexts       []string
	region         string
	want           string
	wantErr        error
}

// runEKSContextCase asserts resolveEKSCreatedContext's outcome for one case.
func runEKSContextCase(t *testing.T, testCase eksContextCase) {
	t.Helper()

	kubeconfigPath := writeTestKubeconfig(
		t,
		testCase.currentContext,
		testCase.contexts...,
	)

	resolved, err := cluster.ExportResolveEKSCreatedContext(
		kubeconfigPath,
		"my-cluster",
		testCase.region,
	)

	if testCase.wantErr != nil {
		require.ErrorIs(t, err, testCase.wantErr)

		return
	}

	require.NoError(t, err)
	assert.Equal(t, testCase.want, resolved)
}

func TestResolveEKSCreatedContextSelection(t *testing.T) {
	t.Parallel()

	tests := []eksContextCase{
		{
			name:           "prefers matching current context over other matches",
			currentContext: "role-b@my-cluster.eu-north-1.eksctl.io",
			contexts: []string{
				"role-a@my-cluster.eu-north-1.eksctl.io",
				"role-b@my-cluster.eu-north-1.eksctl.io",
			},
			region: "eu-north-1",
			want:   "role-b@my-cluster.eu-north-1.eksctl.io",
		},
		{
			name:           "accepts unique match when current context is unrelated",
			currentContext: "kind-other",
			contexts: []string{
				"kind-other",
				"admin@my-cluster.eu-north-1.eksctl.io",
			},
			region: "eu-north-1",
			want:   "admin@my-cluster.eu-north-1.eksctl.io",
		},
		{
			name:           "unknown region matches any region segment",
			currentContext: "",
			contexts:       []string{"admin@my-cluster.us-east-1.eksctl.io"},
			region:         "",
			want:           "admin@my-cluster.us-east-1.eksctl.io",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runEKSContextCase(t, testCase)
		})
	}
}

func TestResolveEKSCreatedContextFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []eksContextCase{
		{
			name:           "synthetic suffix-only context does not match",
			currentContext: "my-cluster.eksctl.io",
			contexts:       []string{"my-cluster.eksctl.io"},
			region:         "eu-north-1",
			wantErr:        cluster.ErrEKSContextNotFound,
		},
		{
			name:           "wrong region does not match",
			currentContext: "",
			contexts:       []string{"admin@my-cluster.us-east-1.eksctl.io"},
			region:         "eu-north-1",
			wantErr:        cluster.ErrEKSContextNotFound,
		},
		{
			name:           "no contexts fails closed",
			currentContext: "",
			contexts:       nil,
			region:         "eu-north-1",
			wantErr:        cluster.ErrEKSContextNotFound,
		},
		{
			name:           "ambiguous matches without matching current context fail closed",
			currentContext: "kind-other",
			contexts: []string{
				"kind-other",
				"role-a@my-cluster.eu-north-1.eksctl.io",
				"role-b@my-cluster.eu-north-1.eksctl.io",
			},
			region:  "eu-north-1",
			wantErr: cluster.ErrEKSContextAmbiguous,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runEKSContextCase(t, testCase)
		})
	}
}
