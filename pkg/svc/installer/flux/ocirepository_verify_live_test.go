package fluxinstaller_test

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

// These tests cover the live read-and-compare path — currentOCIRepositoryVerify
// and VerifyDrifted against a real client interface — rather than the pure
// helpers alone. That path is the whole of platform#2922: the drift bit is only
// as trustworthy as the GET that produces it, and stubbing the bit would leave
// a wrong namespace, name, GVR, or nil-map reading undetected.
//
// Deliberately not parallel: the seams they inject through are package-level
// vars, so concurrent cases would overwrite each other's client.

// errUnexpectedClusterRead marks a seam that must never be reached.
var errUnexpectedClusterRead = errors.New("cluster must not be read")

const (
	fluxSystemNamespace  = "flux-system"
	ociRepositoriesRsrc  = "ocirepositories"
	sourceGroup          = "source.toolkit.fluxcd.io"
	sourceVersion        = "v1"
	verifyProviderCosign = "cosign"
)

// ociRepositoryGVR is the resource the verify read targets. A test that built
// its fake against a different GVR would pass while production looked elsewhere.
func ociRepositoryGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    sourceGroup,
		Version:  sourceVersion,
		Resource: ociRepositoriesRsrc,
	}
}

// newFakeOCIRepository builds the root flux-system OCIRepository. verify is set
// only when non-nil, so the "present but unverified" case — the exact drift
// platform#2922 shipped to production — is representable.
func newFakeOCIRepository(verify map[string]any) *unstructured.Unstructured {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   sourceGroup,
		Version: sourceVersion,
		Kind:    "OCIRepository",
	})
	repo.SetName(fluxSystemNamespace)
	repo.SetNamespace(fluxSystemNamespace)

	spec := map[string]any{"url": "oci://ghcr.io/devantler-tech/platform"}
	if verify != nil {
		spec["verify"] = verify
	}

	repo.Object["spec"] = spec

	return repo
}

// withFakeCluster injects a stubbed REST config and a fake dynamic client
// seeded with the given objects, restoring both when the test ends.
func withFakeCluster(t *testing.T, objects ...runtime.Object) {
	t.Helper()

	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{ociRepositoryGVR(): "OCIRepositoryList"},
		objects...,
	)

	restoreLoad := fluxinstaller.SetLoadRESTConfig(func(_, _ string) (*rest.Config, error) {
		return &rest.Config{}, nil
	})
	t.Cleanup(restoreLoad)

	restoreClient := fluxinstaller.SetNewUnstructuredClient(
		func(_ *rest.Config) (dynamic.Interface, error) {
			return client, nil
		},
	)
	t.Cleanup(restoreClient)
}

// verifyingCluster is a Flux cluster configured to verify with cosign — the
// only shape that makes VerifyDrifted read the cluster at all.
func verifyingCluster() *v1alpha1.Cluster {
	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	clusterCfg.Spec.Workload.Flux.Verify = v1alpha1.FluxVerifySpec{
		Provider:  verifyProviderCosign,
		SecretRef: v1alpha1.FluxVerifySecretRef{Name: "cosign-public-key"},
	}

	return clusterCfg
}

func TestCurrentOCIRepositoryVerifyReportsNotFound(t *testing.T) {
	withFakeCluster(t)

	current, found, err := fluxinstaller.CurrentOCIRepositoryVerify(
		context.Background(), "", "",
	)
	require.NoError(t, err, "a missing OCIRepository is an expected state, not an error")
	assert.False(t, found)
	assert.Nil(t, current)
}

func TestCurrentOCIRepositoryVerifyReportsAbsentVerifyAsFound(t *testing.T) {
	withFakeCluster(t, newFakeOCIRepository(nil))

	current, found, err := fluxinstaller.CurrentOCIRepositoryVerify(
		context.Background(), "", "",
	)
	require.NoError(t, err)
	assert.True(t, found, "the resource exists — only its spec.verify is missing")
	assert.Nil(t, current, "a nil block is what distinguishes unverified from absent")
}

func TestCurrentOCIRepositoryVerifyReadsTheLiveBlock(t *testing.T) {
	live := map[string]any{
		"provider":  verifyProviderCosign,
		"secretRef": map[string]any{"name": "cosign-public-key"},
	}
	withFakeCluster(t, newFakeOCIRepository(live))

	current, found, err := fluxinstaller.CurrentOCIRepositoryVerify(
		context.Background(), "", "",
	)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, live, current)
}

func TestVerifyDriftedIsFalseWhenTheClusterIsNotBootstrapped(t *testing.T) {
	withFakeCluster(t)

	drifted, err := fluxinstaller.VerifyDrifted(
		context.Background(), "", "", verifyingCluster(),
	)
	require.NoError(t, err)
	assert.False(
		t, drifted,
		"no OCIRepository means SetupInstance will apply verify on create; "+
			"reporting drift here would emit a change on every update",
	)
}

func TestVerifyDriftedIsTrueWhenTheLiveSourceCarriesNoVerify(t *testing.T) {
	withFakeCluster(t, newFakeOCIRepository(nil))

	drifted, err := fluxinstaller.VerifyDrifted(
		context.Background(), "", "", verifyingCluster(),
	)
	require.NoError(t, err)
	assert.True(
		t, drifted,
		"this is platform#2922: verify configured, live source unverified, deploy green",
	)
}

func TestVerifyDriftedIsFalseWhenTheLiveBlockAlreadyMatches(t *testing.T) {
	clusterCfg := verifyingCluster()
	// Seed the cluster with exactly what the patcher would write, so a
	// re-assertion is provably a no-op rather than a per-update change.
	withFakeCluster(t, newFakeOCIRepository(
		fluxinstaller.BuildVerifyPatch(clusterCfg.Spec.Workload.Flux.Verify),
	))

	drifted, err := fluxinstaller.VerifyDrifted(context.Background(), "", "", clusterCfg)
	require.NoError(t, err)
	assert.False(t, drifted, "an already-verified source must not report drift")
}

func TestVerifyDriftedIsTrueWhenTheLiveBlockDiffers(t *testing.T) {
	withFakeCluster(t, newFakeOCIRepository(map[string]any{"provider": "notation"}))

	drifted, err := fluxinstaller.VerifyDrifted(
		context.Background(), "", "", verifyingCluster(),
	)
	require.NoError(t, err)
	assert.True(t, drifted, "a live block that differs from the configured one is drift")
}

// TestVerifyDriftedShortCircuitsBeforeReadingTheCluster pins that the guards run
// before any client is built. Without this, a non-Flux or verify-disabled
// cluster would pay a live API read — and a missing kubeconfig would surface as
// a spurious warning on every update.
func TestVerifyDriftedShortCircuitsBeforeReadingTheCluster(t *testing.T) {
	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
	}{
		{name: "nil cluster", clusterCfg: nil},
		{
			name: "non-Flux engine",
			clusterCfg: func() *v1alpha1.Cluster {
				clusterCfg := verifyingCluster()
				clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineArgoCD

				return clusterCfg
			}(),
		},
		{
			name: "verify not configured",
			clusterCfg: func() *v1alpha1.Cluster {
				clusterCfg := v1alpha1.NewCluster()
				clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux

				return clusterCfg
			}(),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Both seams fail loudly: reaching either means a guard did not
			// short-circuit. A stub returning success would let that pass.
			restoreLoad := fluxinstaller.SetLoadRESTConfig(
				func(_, _ string) (*rest.Config, error) {
					t.Error("guard must short-circuit before building a REST config")

					return nil, errUnexpectedClusterRead
				},
			)
			t.Cleanup(restoreLoad)

			restoreClient := fluxinstaller.SetNewUnstructuredClient(
				func(_ *rest.Config) (dynamic.Interface, error) {
					t.Error("guard must short-circuit before building a dynamic client")

					return nil, errUnexpectedClusterRead
				},
			)
			t.Cleanup(restoreClient)

			drifted, err := fluxinstaller.VerifyDrifted(
				context.Background(), "", "", testCase.clusterCfg,
			)
			require.NoError(t, err)
			assert.False(t, drifted)
		})
	}
}
