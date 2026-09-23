package fluxinstaller

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

// verifyEnabledCluster returns a cluster config with Flux signature verification switched on.
func verifyEnabledCluster(provider string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Workload.Flux.Verify.Provider = provider

	return cfg
}

// ksailVerifyPatch returns the verify patch KSail writes for provider, in the unstructured form
// the API server stores it in.
func ksailVerifyPatch(t *testing.T, provider string) map[string]any {
	t.Helper()

	kustomize, err := buildSyncKustomize(verifyEnabledCluster(provider))
	require.NoError(t, err)
	require.Len(t, kustomize.Patches, 1)

	patch, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&kustomize.Patches[0])
	require.NoError(t, err)

	return patch
}

// consumerPatches are patches an operator declares on the FluxInstance in Git. The second one
// targets the same OCIRepository as KSail's verify patch but changes something else, so only its
// content tells it apart from KSail's.
func consumerPatches() []any {
	return []any{
		map[string]any{
			"target": map[string]any{"kind": "Deployment", "name": "kustomize-controller"},
			"patch":  "- op: replace\n  path: /spec/replicas\n  value: 2\n",
		},
		map[string]any{
			"target": map[string]any{
				"kind": fluxOCIRepositoryKind,
				"name": defaultOCIRepositoryName,
			},
			"patch": "- op: replace\n  path: /spec/interval\n  value: 5m\n",
		},
	}
}

func liveFluxInstanceWithPatches(patches []any) map[string]any {
	live := liveFluxInstance()
	liveSpec, _ := live["spec"].(map[string]any)
	liveSpec["kustomize"] = map[string]any{"patches": patches}

	return live
}

func desiredFluxInstanceWithVerify(t *testing.T, provider string) *FluxInstance {
	t.Helper()

	desired := desiredFluxInstance()

	if provider != "" {
		kustomize, err := buildSyncKustomize(verifyEnabledCluster(provider))
		require.NoError(t, err)

		desired.Spec.Kustomize = kustomize
	}

	return desired
}

func storedPatches(t *testing.T, spec map[string]any) []any {
	t.Helper()

	kustomize, ok := spec["kustomize"].(map[string]any)
	require.True(t, ok, "spec.kustomize must remain while it carries patches")

	patches, ok := kustomize["patches"].([]any)
	require.True(t, ok, "spec.kustomize.patches must be a list")

	return patches
}

type consumerPatchCase struct {
	name     string
	provider string
	live     []any
	want     []any
}

// consumerPatchCases covers verification off, switched on, switched off, and changed, each with
// consumer patches around KSail's own.
func consumerPatchCases(t *testing.T) []consumerPatchCase {
	t.Helper()

	cosign := ksailVerifyPatch(t, "cosign")
	notation := ksailVerifyPatch(t, "notation")
	consumer := consumerPatches()

	return []consumerPatchCase{
		{name: "verify_off", live: consumerPatches(), want: consumerPatches()},
		{
			name:     "verify_on_adds_ksail_patch",
			provider: "cosign",
			live:     consumerPatches(),
			want:     append(consumerPatches(), cosign),
		},
		{
			name: "verify_turned_off_removes_ksail_patch",
			live: append(consumerPatches(), cosign),
			want: consumerPatches(),
		},
		{
			name:     "verify_changed_replaces_ksail_patch_in_place",
			provider: "notation",
			live:     []any{consumer[0], cosign, consumer[1]},
			want:     []any{consumer[0], notation, consumer[1]},
		},
	}
}

// TestUpsertKeepsConsumerKustomizePatches pins #7092. spec.kustomize is written by both KSail (its
// verify patch) and the consumer (their own patches in Git). Replacing it whole on update deleted
// the consumer's patches, or swapped them for KSail's, until GitOps put them back.
func TestUpsertKeepsConsumerKustomizePatches(t *testing.T) {
	t.Parallel()

	for _, testCase := range consumerPatchCases(t) {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &jsonStoreClient{stored: liveFluxInstanceWithPatches(testCase.live)}
			spec := upsertInto(t, store, desiredFluxInstanceWithVerify(t, testCase.provider))

			assert.Equal(t, testCase.want, storedPatches(t, spec))
		})
	}
}

// TestUpsertOfKSailPatchIsIdempotent guards against the verify patch being appended again on
// every update: a second update against the result of the first must change nothing.
func TestUpsertOfKSailPatchIsIdempotent(t *testing.T) {
	t.Parallel()

	store := &jsonStoreClient{stored: liveFluxInstanceWithPatches(consumerPatches())}
	first := storedPatches(t, upsertInto(t, store, desiredFluxInstanceWithVerify(t, "cosign")))

	store.updates = 0
	second := storedPatches(t, upsertInto(t, store, desiredFluxInstanceWithVerify(t, "cosign")))

	assert.Equal(t, first, second)
}

// TestIsKSailVerifyPatch documents the ownership rule: a patch is KSail's when it targets the
// flux-system OCIRepository and consists of one add on /spec/verify. Everything else, even with
// the same target, is the consumer's.
func TestIsKSailVerifyPatch(t *testing.T) {
	t.Parallel()

	sameTarget := map[string]any{"kind": fluxOCIRepositoryKind, "name": defaultOCIRepositoryName}

	for _, testCase := range []struct {
		name  string
		patch any
		want  bool
	}{
		{name: "ksail_cosign", patch: ksailVerifyPatch(t, "cosign"), want: true},
		{name: "ksail_notation", patch: ksailVerifyPatch(t, "notation"), want: true},
		{
			name: "same_target_other_path",
			patch: map[string]any{
				"target": sameTarget,
				"patch":  "- op: replace\n  path: /spec/interval\n  value: 5m\n",
			},
		},
		{
			name: "same_target_verify_plus_other_operation",
			patch: map[string]any{
				"target": sameTarget,
				"patch": "- op: add\n  path: /spec/verify\n  value:\n    provider: cosign\n" +
					"- op: replace\n  path: /spec/interval\n  value: 5m\n",
			},
		},
		{
			name: "other_repository",
			patch: map[string]any{
				"target": map[string]any{"kind": fluxOCIRepositoryKind, "name": "apps"},
				"patch":  "- op: add\n  path: /spec/verify\n  value:\n    provider: cosign\n",
			},
		},
		{
			name: "strategic_merge_patch",
			patch: map[string]any{
				"target": sameTarget,
				"patch":  "spec:\n  verify:\n    provider: cosign\n",
			},
		},
		{name: "no_target", patch: map[string]any{"patch": "stale"}},
		{name: "not_an_object", patch: "stale"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, isKSailVerifyPatch(testCase.patch))
		})
	}
}

// consumerVerifyOperation returns a consumer patch on target whose single operation touches
// /spec/verify with something other than KSail's add.
func consumerVerifyOperation(target map[string]any, body string) map[string]any {
	return map[string]any{"target": target, "patch": body}
}

// consumerVerifyOperationBodies are single JSON6902 operations on /spec/verify that KSail never
// writes, since its own verify patch only ever adds that path.
func consumerVerifyOperationBodies() map[string]string {
	return map[string]string{
		"remove":  "- op: remove\n  path: /spec/verify\n",
		"replace": "- op: replace\n  path: /spec/verify\n  value:\n    provider: cosign\n",
		"test":    "- op: test\n  path: /spec/verify\n  value:\n    provider: cosign\n",
	}
}

// TestIsKSailVerifyPatchRejectsOtherVerifyOperations guards the operation half of the ownership
// rule: a single remove, replace or test on /spec/verify of flux-system is the consumer's.
func TestIsKSailVerifyPatchRejectsOtherVerifyOperations(t *testing.T) {
	t.Parallel()

	sameTarget := map[string]any{"kind": fluxOCIRepositoryKind, "name": defaultOCIRepositoryName}

	for operation, body := range consumerVerifyOperationBodies() {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			assert.False(t, isKSailVerifyPatch(consumerVerifyOperation(sameTarget, body)))
		})
	}
}

// TestMergeKustomizePatchesKeepsConsumerVerifyOperations guards the operation half of the
// ownership rule: KSail only ever adds /spec/verify, so a consumer's remove, replace or test on
// that path is theirs and survives both turning verification on and turning it off.
func TestMergeKustomizePatchesKeepsConsumerVerifyOperations(t *testing.T) {
	t.Parallel()

	sameTarget := map[string]any{"kind": fluxOCIRepositoryKind, "name": defaultOCIRepositoryName}
	ksailPatch := ksailVerifyPatch(t, "cosign")

	for _, body := range consumerVerifyOperationBodies() {
		consumer := consumerVerifyOperation(sameTarget, body)

		assert.Equal(t,
			[]any{consumer, ksailPatch},
			mergeKustomizePatches([]any{consumer}, []any{ksailPatch}),
			"verify on: %q", body)
		assert.Equal(t,
			[]any{consumer},
			mergeKustomizePatches([]any{consumer, ksailPatch}, nil),
			"verify off: %q", body)
	}
}

// TestUpsertKeepsUnmodelledDistributionFields extends #6455 one level down: KSail models the
// Flux version, registry and artifact of spec.distribution, but not the variant or the pull
// secrets, and those belong to the consumer.
func TestUpsertKeepsUnmodelledDistributionFields(t *testing.T) {
	t.Parallel()

	live := liveFluxInstance()
	liveSpec, _ := live["spec"].(map[string]any)
	liveSpec["distribution"] = map[string]any{
		"version":            "2.7.x",
		"registry":           "ghcr.io/fluxcd",
		"artifact":           "oci://stale.example/artifact",
		"variant":            "upstream-alpine",
		"artifactPullSecret": "artifact-creds",
		"imagePullSecret":    "image-creds",
	}

	store := &jsonStoreClient{stored: live}
	spec := upsertInto(t, store, desiredFluxInstance())

	assert.Equal(t, map[string]any{
		"version":            "2.8.x",
		"registry":           "ghcr.io/fluxcd",
		"variant":            "upstream-alpine",
		"artifactPullSecret": "artifact-creds",
		"imagePullSecret":    "image-creds",
	}, spec["distribution"],
		"KSail's modelled keys are replaced or removed; the consumer's keys survive")
}
