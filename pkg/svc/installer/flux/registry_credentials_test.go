package fluxinstaller_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

// errStubREST stops the call before it reaches a real API server: this test is
// about which context the write resolves, not about the write itself.
var errStubREST = errors.New("stub rest config")

// testKubeContext is the context every case below resolves. The drift read and
// the credential write must both land on it: a test that let them differ would
// pass while production delivered a rotated credential to the wrong cluster.
const testKubeContext = "admin@prod"

// newExternalRegistryCluster builds a cluster whose registry carries inline
// credentials. token is the password half — the value a rotation replaces.
func newExternalRegistryCluster(token string) *v1alpha1.Cluster {
	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	clusterCfg.Spec.Cluster.Connection.Context = testKubeContext
	clusterCfg.Spec.Cluster.LocalRegistry.Registry = "ksail-bot:" + token + "@ghcr.io/devantler-tech/repo"

	return clusterCfg
}

// TestEnsureRegistryCredentialsUsesTheGivenContext pins the wiring, not the
// logic. An empty kube context falls back to the kubeconfig's ambient
// current-context, so a credential rotation detected on one cluster would be
// written to whichever cluster the operator's kubeconfig happens to point at.
// The threading is invisible to an end-to-end test that runs with a single
// context, so assert the value the production call actually passes.
//
//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestEnsureRegistryCredentialsUsesTheGivenContext(t *testing.T) {
	var gotContext string

	restore := fluxinstaller.SetLoadRESTConfig(
		func(_, kubeContext string) (*rest.Config, error) {
			gotContext = kubeContext

			return nil, errStubREST
		},
	)
	defer restore()

	clusterCfg := newExternalRegistryCluster("rotated-token")

	err := fluxinstaller.EnsureRegistryCredentials(
		context.Background(), "/tmp/kubeconfig", testKubeContext, clusterCfg,
	)

	require.ErrorIs(t, err, errStubREST)
	assert.Equal(t, testKubeContext, gotContext,
		"the credential write must target the context the drift check read from, "+
			"not the ambient current-context")
}

// TestRegistryCredentialDriftedReadsTheGivenContext is the read-side twin of the
// test above. The drift check and the credential write must resolve the same
// cluster: detecting a rotation on one and repairing it on another would leave
// the rotated cluster authenticating with the revoked value while KSail reports
// success.
//
//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestRegistryCredentialDriftedReadsTheGivenContext(t *testing.T) {
	var gotContext string

	restore := fluxinstaller.SetLoadRESTConfig(
		func(_, kubeContext string) (*rest.Config, error) {
			gotContext = kubeContext

			return nil, errStubREST
		},
	)
	defer restore()

	clusterCfg := newExternalRegistryCluster("rotated-token")

	_, err := fluxinstaller.RegistryCredentialDrifted(
		context.Background(), "/tmp/kubeconfig", testKubeContext, clusterCfg,
	)

	require.ErrorIs(t, err, errStubREST)
	assert.Equal(t, testKubeContext, gotContext,
		"the drift check must read the context the credential write targets, "+
			"not the ambient current-context")
}

// TestRegistryCredentialDriftedSkipsClusterWithoutCredentials keeps the drift
// check inert — and off the network — when there is nothing for KSail to write.
// The stub fails any REST config build, so reaching the cluster at all fails the
// test.
//
//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestRegistryCredentialDriftedSkipsClusterWithoutCredentials(t *testing.T) {
	restore := fluxinstaller.SetLoadRESTConfig(
		func(_, _ string) (*rest.Config, error) { return nil, errStubREST },
	)
	defer restore()

	drifted, err := fluxinstaller.RegistryCredentialDrifted(
		context.Background(), "/tmp/kubeconfig", testKubeContext, v1alpha1.NewCluster(),
	)

	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestHasExternalRegistryCredentials pins the cheap predicate that decides
// whether the cluster is worth querying at all.
func TestHasExternalRegistryCredentials(t *testing.T) {
	t.Parallel()

	assert.False(t, fluxinstaller.HasExternalRegistryCredentials(nil))
	assert.False(t, fluxinstaller.HasExternalRegistryCredentials(v1alpha1.NewCluster()),
		"a default cluster has no external registry")
	assert.True(t,
		fluxinstaller.HasExternalRegistryCredentials(
			newExternalRegistryCluster("a-token"),
		),
		"an external registry with inline credentials is worth a drift check")
}

// TestBuildRegistrySecretSeparatesRotations is the property the drift check
// depends on: rotating the password must change the docker config KSail would
// write, or the comparison has nothing to see.
func TestBuildRegistrySecretSeparatesRotations(t *testing.T) {
	t.Parallel()

	before, err := fluxinstaller.BuildRegistrySecret(
		newExternalRegistryCluster("first-token"),
	)
	require.NoError(t, err)

	repeat, err := fluxinstaller.BuildRegistrySecret(
		newExternalRegistryCluster("first-token"),
	)
	require.NoError(t, err)

	after, err := fluxinstaller.BuildRegistrySecret(
		newExternalRegistryCluster("a-different-token"),
	)
	require.NoError(t, err)

	firstConfig := before.Data[corev1.DockerConfigJsonKey]
	require.NotEmpty(
		t,
		firstConfig,
		"an external registry with credentials must yield a docker config",
	)

	assert.False(t,
		fluxinstaller.DockerConfigsDiffer(firstConfig, repeat.Data[corev1.DockerConfigJsonKey]),
		"an unrotated credential must be stable, or every update reports false drift")
	assert.True(t,
		fluxinstaller.DockerConfigsDiffer(firstConfig, after.Data[corev1.DockerConfigJsonKey]),
		"a rotated credential must be visible, or the rotation is invisible to the update")
}

// TestDockerConfigsDifferSuppressesAnAbsentSide is the ownership boundary: an
// absent current config means the Secret does not exist or is not KSail-managed,
// and reporting drift there would have KSail overwrite a Secret owned by
// something else (an ExternalSecret, a platform bootstrap).
func TestDockerConfigsDifferSuppressesAnAbsentSide(t *testing.T) {
	t.Parallel()

	present := []byte(`{"auths":{"ghcr.io":{"auth":"dXNlcjpwYXNz"}}}`)

	assert.False(t, fluxinstaller.DockerConfigsDiffer(nil, present),
		"an absent current config is not drift — KSail does not own that Secret")
	assert.False(t, fluxinstaller.DockerConfigsDiffer([]byte{}, present),
		"an empty current config is not drift either")
	assert.False(t, fluxinstaller.DockerConfigsDiffer(present, nil),
		"no desired credential means there is nothing to write")
	assert.False(t, fluxinstaller.DockerConfigsDiffer(present, present),
		"an unrotated credential must not produce a change every update")
}
