package workload_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The same folder built from a Git top level and from an artifact root reads
// different files, so the two are not the same application of it.
func TestFluxBindingSameAsComparesRoot(t *testing.T) {
	t.Parallel()

	empty := map[string]any{}

	assert.False(t, workload.FluxBindingsSameAs("/repo", "/repo/k8s", empty, empty))
	assert.True(t, workload.FluxBindingsSameAs("/repo", "/repo", empty, empty))
}

// Only fields validation reproduces or knows to leave the applied objects
// unchanged are modelled; kubeConfig and an unknown field are named.
func TestUnmodelledFluxFields(t *testing.T) {
	t.Parallel()

	object := map[string]any{"spec": map[string]any{
		"path": "./apps", "sourceRef": map[string]any{}, "interval": "10m",
		"patches": []any{}, "commonMetadata": map[string]any{}, "postBuild": map[string]any{},
		"decryption": map[string]any{}, "kubeConfig": map[string]any{}, "futureField": true,
	}}

	assert.Equal(t, []string{"futureField", "kubeConfig"}, workload.UnmodelledFluxFields(object))
	assert.Empty(
		t,
		workload.UnmodelledFluxFields(map[string]any{"spec": map[string]any{"path": "."}}),
	)
}

// commonMetadata replaces same-named keys and keeps every other key, on every
// document; without it the output is returned unchanged.
func TestWithCommonMetadata(t *testing.T) {
	t.Parallel()

	data := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  labels:\n" +
		"    team: old\n    keep: me\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n")
	object := map[string]any{"spec": map[string]any{"commonMetadata": map[string]any{
		"labels":      map[string]any{"team": "platform"},
		"annotations": map[string]any{"owner": "flux"},
	}}}

	out, err := workload.WithCommonMetadata(data, object)
	require.NoError(t, err)

	docs := workload.DecodeDocuments(out)
	require.Len(t, docs, 2)

	for _, doc := range docs {
		metadata, _ := doc["metadata"].(map[string]any)
		labels, _ := metadata["labels"].(map[string]any)
		annotations, _ := metadata["annotations"].(map[string]any)

		assert.Equal(t, "platform", labels["team"])
		assert.Equal(t, "flux", annotations["owner"])
	}

	first, _ := docs[0]["metadata"].(map[string]any)
	firstLabels, _ := first["labels"].(map[string]any)
	assert.Equal(t, "me", firstLabels["keep"])

	unchanged, err := workload.WithCommonMetadata(data, map[string]any{"spec": map[string]any{}})
	require.NoError(t, err)
	assert.Equal(t, data, unchanged)

	_, err = workload.WithCommonMetadata(data, map[string]any{"spec": map[string]any{
		"commonMetadata": map[string]any{"labels": map[string]any{"n": 1}},
	}})
	require.ErrorIs(t, err, workload.ErrFluxCommonMetadataInvalid)
}

// An omitted list and an empty one, and an omitted flag and a false one, change
// nothing Flux builds, so they compose a folder the same way; a set value does.
func TestFluxCompositionNormalizesNoOpValues(t *testing.T) {
	t.Parallel()

	omitted := workload.FluxComposition(map[string]any{"spec": map[string]any{"path": "."}})
	noOps := workload.FluxComposition(map[string]any{"spec": map[string]any{
		"path": ".", "patches": []any{}, "components": []any{}, "ignoreMissingComponents": false,
	}})
	set := workload.FluxComposition(map[string]any{"spec": map[string]any{
		"path": ".", "ignoreMissingComponents": true,
	}})

	assert.Empty(t, omitted)
	assert.Empty(t, noOps)
	assert.True(t, workload.FluxBindingsSameAs("/repo", "/repo", omitted, noOps))
	assert.Equal(t, map[string]any{"ignoreMissingComponents": true}, set)
	assert.False(t, workload.FluxBindingsSameAs("/repo", "/repo", omitted, set))
}
