package lifecycle_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

//nolint:paralleltest // Standalone resolution reads the working directory and environment.
func TestResolveClusterInfoFromSelectedEksctlContext(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AWS_REGION", "")
	kubeconfigPath := writeSelectedEKSContext(t)

	for _, provider := range []v1alpha1.Provider{"", v1alpha1.ProviderAWS} {
		t.Run(string(provider), func(t *testing.T) {
			resolved, err := lifecycle.ResolveClusterInfoStrict(nil, "", provider, kubeconfigPath)
			require.NoError(t, err)
			assert.Equal(t, "demo", resolved.ClusterName)
			assert.Equal(t, v1alpha1.ProviderAWS, resolved.Provider)
			assert.Equal(t, "us-west-2", resolved.AWSRegion)
			assert.False(t, resolved.AWSRegionFromConfig)
		})
	}

	t.Run("environment region overrides selected context", func(t *testing.T) {
		t.Setenv("AWS_REGION", "eu-west-1")

		resolved, err := lifecycle.ResolveClusterInfoStrict(nil, "", "", kubeconfigPath)
		require.NoError(t, err)
		assert.Equal(t, "eu-west-1", resolved.AWSRegion)
	})

	t.Run("explicit name does not inherit another context target", func(t *testing.T) {
		resolved, err := lifecycle.ResolveClusterInfoStrict(
			nil,
			"explicit",
			v1alpha1.ProviderAWS,
			kubeconfigPath,
		)
		require.NoError(t, err)
		assert.Equal(t, "explicit", resolved.ClusterName)
		assert.Empty(t, resolved.AWSRegion)
	})

	t.Run("explicit provider is retained", func(t *testing.T) {
		resolved, err := lifecycle.ResolveClusterInfoStrict(
			nil,
			"",
			v1alpha1.ProviderDocker,
			kubeconfigPath,
		)
		require.NoError(t, err)
		assert.Equal(t, v1alpha1.ProviderDocker, resolved.Provider)
		assert.Empty(t, resolved.AWSRegion)
	})
}

func TestSimpleLifecycleEksctlContextReachesGuard(t *testing.T) {
	guarded := executeSimpleLifecycleGuard(t, []string{"--provider", "AWS"})

	assert.Equal(t, v1alpha1.ProviderAWS, guarded.Provider)
	assert.Equal(t, "demo", guarded.ClusterName)
	assert.Equal(t, "us-west-2", guarded.AWSRegion)
}

// TestSimpleLifecycleEksctlContextInfersProviderWithoutFlags covers the documented
// standalone form: with no flags at all, the selected eksctl context alone must
// identify the cluster, its region, and the AWS provider that reaches the guard.
func TestSimpleLifecycleEksctlContextInfersProviderWithoutFlags(t *testing.T) {
	guarded := executeSimpleLifecycleGuard(t, []string{})

	assert.Equal(t, v1alpha1.ProviderAWS, guarded.Provider)
	assert.Equal(t, "demo", guarded.ClusterName)
	assert.Equal(t, "us-west-2", guarded.AWSRegion)
}

// executeSimpleLifecycleGuard runs a simple lifecycle command against a kubeconfig
// whose selected context is an eksctl cluster and returns what reached the guard.
func executeSimpleLifecycleGuard(t *testing.T, args []string) *lifecycle.ResolvedClusterInfo {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("AWS_REGION", "")
	t.Setenv("KUBECONFIG", writeSelectedEKSContext(t))

	var guarded *lifecycle.ResolvedClusterInfo

	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use: "start",
		Guard: func(_ context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			guarded = resolved

			return errTestError
		},
	})
	cmd.SetArgs(args)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))

	require.ErrorIs(t, cmd.ExecuteContext(t.Context()), errTestError)
	require.NotNil(t, guarded)

	return guarded
}

func writeSelectedEKSContext(t *testing.T) string {
	t.Helper()

	const selected = "arn:aws:iam::123456789012:role/operator@demo.us-west-2.eksctl.io"

	config := clientcmdapi.NewConfig()
	config.CurrentContext = selected
	config.Clusters["eks"] = &clientcmdapi.Cluster{
		Server: "https://example.us-west-2.eks.amazonaws.com",
	}
	config.Contexts[selected] = &clientcmdapi.Context{Cluster: "eks"}
	config.Contexts["demo.eu-west-1.eksctl.io"] = &clientcmdapi.Context{Cluster: "eks"}
	kubeconfigPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(*config, kubeconfigPath))

	return kubeconfigPath
}
