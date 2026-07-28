package fluxinstaller_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// errStubREST stops the call before it reaches a real API server: this test is
// about which context the write resolves, not about the write itself.
var errStubREST = errors.New("stub rest config")

// newExternalRegistryCluster builds a cluster whose registry carries inline
// credentials. token is the password half — the value a rotation replaces.
func newExternalRegistryCluster(kubeContext, token string) *v1alpha1.Cluster {
	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	clusterCfg.Spec.Cluster.Connection.Context = kubeContext
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

	clusterCfg := newExternalRegistryCluster("admin@prod", "rotated-token")

	err := fluxinstaller.EnsureRegistryCredentials(
		context.Background(), "/tmp/kubeconfig", "admin@prod", clusterCfg,
	)

	require.ErrorIs(t, err, errStubREST)
	assert.Equal(t, "admin@prod", gotContext,
		"the credential write must target the context the drift check read from, "+
			"not the ambient current-context")
}

// TestDesiredRegistryCredentialDigestSeparatesRotations is the property the
// drift check depends on: two different credentials must not produce the same
// digest, and an unchanged credential must reproduce its digest exactly.
func TestDesiredRegistryCredentialDigestSeparatesRotations(t *testing.T) {
	t.Parallel()

	before := newExternalRegistryCluster("admin@prod", "first-token")

	firstDigest, err := fluxinstaller.DesiredRegistryCredentialDigest(before)
	require.NoError(t, err)
	require.NotEmpty(t, firstDigest, "an external registry with credentials must yield a digest")

	repeatDigest, err := fluxinstaller.DesiredRegistryCredentialDigest(before)
	require.NoError(t, err)
	assert.Equal(t, firstDigest, repeatDigest,
		"an unrotated credential must be stable, or every update reports false drift")

	after := newExternalRegistryCluster("admin@prod", "a-different-token")

	rotatedDigest, err := fluxinstaller.DesiredRegistryCredentialDigest(after)
	require.NoError(t, err)
	assert.NotEqual(t, firstDigest, rotatedDigest,
		"a rotated credential must change the digest, or the rotation is invisible")
}

// TestDesiredRegistryCredentialDigestEmptyWithoutCredentials keeps the drift
// check inert when there is nothing for KSail to write.
func TestDesiredRegistryCredentialDigestEmptyWithoutCredentials(t *testing.T) {
	t.Parallel()

	clusterCfg := v1alpha1.NewCluster()

	digest, err := fluxinstaller.DesiredRegistryCredentialDigest(clusterCfg)

	require.NoError(t, err)
	assert.Empty(t, digest)
}
