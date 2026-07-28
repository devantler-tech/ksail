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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

// TestEnsureRegistryCredentialsSkipsClusterWithoutCredentials is the write-side
// twin of the drift-check skip above. EnsureRegistryCredentials documents itself
// as a no-op when the registry carries nothing KSail would write, but it used to
// resolve a REST config first — so on a machine with no usable kubeconfig the
// no-op failed. The stub fails any REST config build, so reaching for one at all
// fails the test.
//
//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestEnsureRegistryCredentialsSkipsClusterWithoutCredentials(t *testing.T) {
	restore := fluxinstaller.SetLoadRESTConfig(
		func(_, _ string) (*rest.Config, error) { return nil, errStubREST },
	)
	defer restore()

	err := fluxinstaller.EnsureRegistryCredentials(
		context.Background(), "/tmp/kubeconfig", testKubeContext, v1alpha1.NewCluster(),
	)

	require.NoError(t, err, "a cluster with no external credential must be a no-op, not a failure")
}

// newFakeCoreV1Client returns a seam replacement serving the given objects, and
// a restore func.
func newFakeCoreV1Client(t *testing.T, objects ...client.Object) func() {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	restoreREST := fluxinstaller.SetLoadRESTConfig(
		func(_, _ string) (*rest.Config, error) { return &rest.Config{}, nil },
	)
	restoreClient := fluxinstaller.SetNewCoreV1Client(
		func(*rest.Config) (client.Client, error) { return fakeClient, nil },
	)

	return func() {
		restoreClient()
		restoreREST()
	}
}

// registrySecret builds a Secret at the name/namespace the drift check reads,
// with the given ownership label and docker config.
func registrySecret(owned bool, dockerConfig []byte) *corev1.Secret {
	secret := &corev1.Secret{}
	secret.Name = fluxinstaller.ExternalRegistrySecretName
	secret.Namespace = "flux-system"
	secret.Type = corev1.SecretTypeDockerConfigJson

	if owned {
		secret.Labels = map[string]string{"app.kubernetes.io/managed-by": "ksail"}
	} else {
		secret.Labels = map[string]string{"app.kubernetes.io/managed-by": "external-secrets"}
	}

	if dockerConfig != nil {
		secret.Data = map[string][]byte{corev1.DockerConfigJsonKey: dockerConfig}
	}

	return secret
}

// TestRegistryCredentialDriftedRepairsAnOwnedSecretWithNoDockerConfig covers the
// case the ownership suppression used to swallow: a Secret KSail owns but whose
// docker config is missing or empty is malformed, not off-limits. Reporting no
// drift there leaves Flux unable to authenticate with nothing to trigger a
// repair, whereas the Secret is KSail's own so rewriting it is in bounds.
//
//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestRegistryCredentialDriftedRepairsAnOwnedSecretWithNoDockerConfig(t *testing.T) {
	for name, dockerConfig := range map[string][]byte{
		"missing key": nil,
		"empty value": {},
	} {
		t.Run(name, func(t *testing.T) {
			restore := newFakeCoreV1Client(t, registrySecret(true, dockerConfig))
			defer restore()

			drifted, err := fluxinstaller.RegistryCredentialDrifted(
				context.Background(), "/tmp/kubeconfig", testKubeContext,
				newExternalRegistryCluster("a-token"),
			)

			require.NoError(t, err)
			assert.True(t, drifted,
				"a KSail-owned Secret with no docker config must be refreshed, not suppressed")
		})
	}
}

// TestRegistryCredentialDriftedSuppressesSecretsKSailDoesNotOwn is the boundary
// the case above must not erode: an absent Secret and one managed by something
// else both stay suppressed, because writing either would have KSail overwrite a
// Secret it does not own.
//
//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestRegistryCredentialDriftedSuppressesSecretsKSailDoesNotOwn(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		restore := newFakeCoreV1Client(t)
		defer restore()

		drifted, err := fluxinstaller.RegistryCredentialDrifted(
			context.Background(), "/tmp/kubeconfig", testKubeContext,
			newExternalRegistryCluster("a-token"),
		)

		require.NoError(t, err)
		assert.False(t, drifted)
	})

	t.Run("externally managed and empty", func(t *testing.T) {
		restore := newFakeCoreV1Client(t, registrySecret(false, nil))
		defer restore()

		drifted, err := fluxinstaller.RegistryCredentialDrifted(
			context.Background(), "/tmp/kubeconfig", testKubeContext,
			newExternalRegistryCluster("a-token"),
		)

		require.NoError(t, err)
		assert.False(t, drifted,
			"ownership, not emptiness, is what suppresses a Secret KSail did not write")
	})
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
