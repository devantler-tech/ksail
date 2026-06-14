package clusterapi_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	k8stesting "k8s.io/client-go/testing"
)

// applyTestMapper is a static REST mapper registering ConfigMap (namespaced), so apply tests resolve
// GVK→resource without a live discovery client.
func applyTestMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"},
		meta.RESTScopeNamespace,
	)

	return mapper
}

func injectFakeApply(service *clusterapi.Service) {
	client := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)

	// The fake dynamic client implements Apply as a patch that fails to create a missing object, so
	// stub the patch (apply) verb on configmaps to echo the applied object back as success — the test
	// exercises the parse/resolve/namespace/error-handling logic, not the fake's SSA persistence.
	client.PrependReactor(
		"patch",
		"configmaps",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			patch, _ := action.(k8stesting.PatchAction)
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion("v1")
			obj.SetKind("ConfigMap")
			obj.SetName(patch.GetName())
			obj.SetNamespace(patch.GetNamespace())

			return true, obj, nil
		},
	)

	mapper := applyTestMapper()

	service.SetApplyClientForTest(
		func(_ context.Context, _ string) (dynamic.Interface, meta.RESTMapper, error) {
			return client, mapper, nil
		},
	)
}

func TestApplyManifestsApplies(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	injectFakeApply(service)

	manifest := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n  namespace: x\ndata:\n  k: v\n",
	)

	results, err := service.ApplyManifests(context.Background(), "default", "c1", manifest, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "applied", results[0].Status)
	assert.Equal(t, "ConfigMap", results[0].Kind)
	assert.Equal(t, "cm1", results[0].Name)
	assert.Equal(t, "x", results[0].Namespace)
	assert.Empty(t, results[0].Error)
}

func TestApplyManifestsMultiDocPartialError(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	injectFakeApply(service)

	// First doc is a registered ConfigMap (applies); second is an unknown kind (mapper rejects it).
	manifest := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n  namespace: x\n" +
			"---\napiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w1\n  namespace: x\n",
	)

	results, err := service.ApplyManifests(context.Background(), "default", "c1", manifest, false)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "applied", results[0].Status)
	assert.Equal(t, "error", results[1].Status)
	assert.NotEmpty(t, results[1].Error)
}

func TestApplyManifestsEmpty(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	injectFakeApply(service)

	_, err := service.ApplyManifests(context.Background(), "default", "c1", []byte("\n\n"), false)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestApplyManifestsRejectsMalformedSeparator(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	injectFakeApply(service)

	// A separator line with trailing junk makes the YAML reader error mid-stream. It must be surfaced
	// (422), not swallowed — otherwise only the first doc applies and a false success is reported.
	manifest := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n  namespace: x\n" +
			"--- junk\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm2\n  namespace: x\n",
	)

	_, err := service.ApplyManifests(context.Background(), "default", "c1", manifest, false)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestApplyManifestsRejectsMissingName(t *testing.T) {
	t.Parallel()

	service := clusterapi.NewTestService(nil)
	injectFakeApply(service)

	manifest := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  namespace: x\n")

	results, err := service.ApplyManifests(context.Background(), "default", "c1", manifest, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "error", results[0].Status)
}
