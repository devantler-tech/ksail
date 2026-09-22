package fluxinstaller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// jsonStoreClient stands in for the API server the way the upsert sees it: the stored object
// keeps every field, and a read decodes it into whatever object the caller passes. A typed
// read therefore drops fields the type does not model, exactly as the real client does, which
// a controller-runtime fake cannot show — it stores the typed object and has already lost them.
type jsonStoreClient struct {
	client.Client

	stored  map[string]any
	updates int
}

func (c *jsonStoreClient) Get(
	_ context.Context,
	_ client.ObjectKey,
	obj client.Object,
	_ ...client.GetOption,
) error {
	raw, err := json.Marshal(c.stored)
	if err != nil {
		return fmt.Errorf("encode stored object: %w", err)
	}

	err = json.Unmarshal(raw, obj)
	if err != nil {
		return fmt.Errorf("decode stored object: %w", err)
	}

	return nil
}

func (c *jsonStoreClient) Update(
	_ context.Context,
	obj client.Object,
	_ ...client.UpdateOption,
) error {
	raw, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode updated object: %w", err)
	}

	stored := map[string]any{}

	err = json.Unmarshal(raw, &stored)
	if err != nil {
		return fmt.Errorf("decode updated object: %w", err)
	}

	c.stored = stored
	c.updates++

	return nil
}

// liveFluxInstance is a FluxInstance as a consumer declares it in Git: KSail's modelled
// fields plus several the flux-operator CRD carries and KSail does not model.
func liveFluxInstance() map[string]any {
	return map[string]any{
		"apiVersion": fluxInstanceGroupVersion.String(),
		"kind":       fluxInstanceKind,
		"metadata": map[string]any{
			"name":            fluxInstanceDefaultName,
			"namespace":       "flux-system",
			"resourceVersion": "23",
		},
		"spec": map[string]any{
			"distribution": map[string]any{
				"version":  "2.7.x",
				"registry": "ghcr.io/fluxcd",
			},
			"components": []any{"source-controller", "kustomize-controller"},
			"cluster": map[string]any{
				"type":          "kubernetes",
				"networkPolicy": true,
			},
			"sharding": map[string]any{
				"shards": []any{"shard1"},
			},
			"sync": map[string]any{
				"kind":       "OCIRepository",
				"url":        "oci://old.example/repo",
				"ref":        "old",
				"path":       "k8s",
				"pullSecret": "stale-secret",
			},
		},
	}
}

func desiredFluxInstance() *FluxInstance {
	return &FluxInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fluxInstanceDefaultName,
			Namespace: "flux-system",
		},
		Spec: InstanceSpec{
			Distribution: Distribution{Version: "2.8.x", Registry: "ghcr.io/fluxcd"},
			Sync: &Sync{
				Kind:     "OCIRepository",
				URL:      "oci://new.example/repo",
				Ref:      "new",
				Path:     "k8s",
				Interval: &metav1.Duration{Duration: time.Minute},
			},
		},
	}
}

func upsertInto(t *testing.T, store *jsonStoreClient, desired *FluxInstance) map[string]any {
	t.Helper()

	manager := &instanceManager{}

	err := manager.tryUpsert(
		context.Background(),
		store,
		client.ObjectKeyFromObject(desired),
		desired,
	)
	require.NoError(t, err)
	require.Equal(t, 1, store.updates, "an existing FluxInstance must be updated exactly once")

	spec, ok := store.stored["spec"].(map[string]any)
	require.True(t, ok, "the updated FluxInstance must still carry a spec")

	return spec
}

// TestUpsertPreservesUnmodelledFluxInstanceFields pins #6455. KSail models only part of the
// FluxInstance spec; replacing the whole spec on update silently removed the rest, so every
// `cluster update` stripped a consumer's components, cluster and sharding settings until
// GitOps put them back.
func TestUpsertPreservesUnmodelledFluxInstanceFields(t *testing.T) {
	t.Parallel()

	live := liveFluxInstance()
	liveSpec, _ := live["spec"].(map[string]any)

	store := &jsonStoreClient{stored: live}
	spec := upsertInto(t, store, desiredFluxInstance())

	for _, field := range []string{"components", "cluster", "sharding"} {
		assert.Equalf(
			t,
			liveSpec[field],
			spec[field],
			"spec.%s is not modelled by KSail and must survive the update unchanged",
			field,
		)
	}
}

// TestUpsertStillOwnsModelledFluxInstanceFields pins the other half: preserving what KSail
// does not model must not turn the fields it does model into a merge. Each value KSail models is
// still replaced, so a value KSail stops setting is removed rather than left stale.
func TestUpsertStillOwnsModelledFluxInstanceFields(t *testing.T) {
	t.Parallel()

	store := &jsonStoreClient{stored: liveFluxInstance()}
	spec := upsertInto(t, store, desiredFluxInstance())

	distribution, ok := spec["distribution"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2.8.x", distribution["version"])

	sync, ok := spec["sync"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "oci://new.example/repo", sync["url"])
	assert.Equal(
		t,
		"1m0s",
		sync["interval"],
		"typed values such as durations must be written in their API form",
	)
	assert.NotContains(
		t,
		sync,
		"pullSecret",
		"spec.sync is KSail's; a pull secret it no longer sets must not survive from the old spec",
	)

	liveWithKustomize := liveFluxInstance()
	liveSpec, _ := liveWithKustomize["spec"].(map[string]any)
	liveSpec["kustomize"] = map[string]any{"patches": []any{ksailVerifyPatch(t, "cosign")}}

	store = &jsonStoreClient{stored: liveWithKustomize}
	spec = upsertInto(t, store, desiredFluxInstance())

	assert.NotContains(
		t,
		spec,
		"kustomize",
		"KSail's verify patch is its own; when KSail sets none, it must be removed, and with no "+
			"other patch left spec.kustomize goes with it",
	)
}

// TestModelledInstanceSpecKeysFollowTheType keeps the owned-key list derived from the type, so
// a field added to InstanceSpec is owned the moment it is modelled instead of being preserved
// as if it were a consumer's.
func TestModelledInstanceSpecKeysFollowTheType(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(
		t,
		[]string{"distribution", "kustomize", "sync"},
		modelledInstanceSpecKeys(),
	)
}
