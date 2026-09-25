package ephemeral_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func ownedObject(
	kind, name, namespace, uid string,
	owner *unstructured.Unstructured,
) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": namespace, "uid": uid},
	}}
	if owner != nil {
		obj.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion: owner.GetAPIVersion(),
				Kind:       owner.GetKind(),
				Name:       owner.GetName(),
				UID:        owner.GetUID(),
			},
		})
	}

	return obj
}

type observationAPI struct {
	root     *unstructured.Unstructured
	children []*unstructured.Unstructured
	reads    atomic.Int32
	lists    atomic.Int32
	failure  string
	// gadgets are served only at example.io/v1alpha1, whose group prefers v1.
	gadgets []*unstructured.Unstructured
}

func (api *observationAPI) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")

	var body any

	switch request.URL.Path {
	case "/api":
		body = metav1.APIVersions{Versions: []string{"v1"}}
	case "/apis":
		body = metav1.APIGroupList{}
		if api.gadgets != nil {
			preferred := metav1.GroupVersionForDiscovery{GroupVersion: "example.io/v1", Version: "v1"}
			body = metav1.APIGroupList{Groups: []metav1.APIGroup{{
				Name: "example.io",
				Versions: []metav1.GroupVersionForDiscovery{
					preferred,
					{GroupVersion: "example.io/v1alpha1", Version: "v1alpha1"},
				},
				PreferredVersion: preferred,
			}}}
		}
	case "/apis/example.io/v1":
		body = metav1.APIResourceList{GroupVersion: "example.io/v1", APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}},
		}}
	case "/apis/example.io/v1alpha1":
		// widgets are also served here; listing them twice would be a duplicate.
		body = metav1.APIResourceList{GroupVersion: "example.io/v1alpha1", APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "gadgets", Kind: "Gadget", Namespaced: true, Verbs: []string{"get", "list"}},
		}}
	case "/apis/example.io/v1/widgets":
		body = &unstructured.UnstructuredList{
			Object: map[string]any{"apiVersion": "example.io/v1", "kind": "WidgetList"},
		}
	case "/apis/example.io/v1alpha1/gadgets":
		list := &unstructured.UnstructuredList{
			Object: map[string]any{"apiVersion": "example.io/v1alpha1", "kind": "GadgetList"},
		}
		for _, gadget := range api.gadgets {
			list.Items = append(list.Items, *gadget)
		}

		body = list
	case "/api/v1":
		if api.failure == "discovery" {
			http.Error(writer, "discovery unavailable", http.StatusServiceUnavailable)

			return
		}

		body = metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{
				Name:       "configmaps",
				Kind:       "ConfigMap",
				Namespaced: true,
				Verbs:      []string{"get", "patch", "list"},
			},
			{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list"}},
			{Name: "pods/status", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "patch"}},
			{Name: "componentstatuses", Kind: "ComponentStatus", Verbs: []string{"get", "list"}},
		}}
	case "/api/v1/namespaces/default/configmaps/root":
		body = api.rootResponse(request)
	case "/api/v1/configmaps":
		body = &unstructured.UnstructuredList{
			Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMapList"},
		}
	case "/api/v1/pods":
		api.servePods(t, writer, request)

		return
	case "/api/v1/componentstatuses":
		// Virtual health resources have no UID and cannot belong to an owner chain.
		body = &unstructured.UnstructuredList{
			Object: map[string]any{"apiVersion": "v1", "kind": "ComponentStatusList"},
			Items: []unstructured.Unstructured{
				*ownedObject("ComponentStatus", "scheduler", "", "", nil),
			},
		}
	default:
		t.Errorf("unexpected API request: %s %s", request.Method, request.URL.Path)
		http.NotFound(writer, request)

		return
	}

	assert.NoError(t, json.NewEncoder(writer).Encode(body))
}

func TestObserveChildrenFollowsUIDsTransitivelyAcrossPages(t *testing.T) {
	t.Parallel()

	root := ownedObject("ConfigMap", "root", "default", "root-uid", nil)
	child := ownedObject("Pod", "child", "default", "child-uid", root)
	grandchild := ownedObject("Pod", "grandchild", "default", "grandchild-uid", child)
	stale := root.DeepCopy()
	stale.SetUID("old-root-uid")

	wrongName := root.DeepCopy()
	wrongName.SetName("other-root")

	wrongKind := root.DeepCopy()
	wrongKind.SetKind("Secret")

	cycle := ownedObject("Pod", "cycle", "default", "cycle", nil)
	cycle.SetOwnerReferences(
		[]metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "cycle", UID: "cycle"}},
	)
	api := &observationAPI{root: root, children: []*unstructured.Unstructured{
		grandchild, child,
		ownedObject("Pod", "old-owner", "default", "stale-child", stale),
		ownedObject("Pod", "wrong-namespace", "other", "cross-namespace", root),
		ownedObject("Pod", "unowned", "default", "unowned", nil),
		ownedObject("Pod", "wrong-name", "default", "wrong-name", wrongName),
		ownedObject("Pod", "wrong-kind", "default", "wrong-kind", wrongKind),
		ownedObject("Pod", "cluster-dependent", "", "invalid-scope", root),
		cycle,
	}}

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { api.serve(t, w, r) }),
	)
	defer server.Close()

	client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
	require.NoError(t, err)
	require.NoError(t, client.Apply(t.Context(), root))
	children, err := client.ObserveChildren(
		t.Context(),
		[]*unstructured.Unstructured{root},
		time.Millisecond,
	)
	require.NoError(t, err)
	require.Len(t, children, 2)
	assert.Equal(t, "child", children[0].GetName())
	assert.Equal(t, "grandchild", children[1].GetName())
	assert.EqualValues(t, 2, api.lists.Load(), "all pages must be consumed exactly once")
	assert.GreaterOrEqual(
		t,
		api.reads.Load(),
		int32(2),
		"root identity must be checked around collection",
	)
}

func TestObserveChildrenFindsKindsServedOnlyAtANonPreferredVersion(t *testing.T) {
	t.Parallel()

	root := ownedObject("ConfigMap", "root", "default", "root-uid", nil)
	gadget := ownedObject("Gadget", "gadget", "default", "gadget-uid", root)
	gadget.SetAPIVersion("example.io/v1alpha1")

	api := &observationAPI{root: root, gadgets: []*unstructured.Unstructured{gadget}}

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { api.serve(t, w, r) }),
	)
	defer server.Close()

	client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
	require.NoError(t, err)
	require.NoError(t, client.Apply(t.Context(), root))

	// The fake refuses an unexpected request, so widgets listed a second time at
	// v1alpha1 fail the test as well.
	children, err := client.ObserveChildren(
		t.Context(),
		[]*unstructured.Unstructured{root},
		time.Millisecond,
	)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, "gadget", children[0].GetName())
}

func TestObserveChildrenFailsOnIncompleteInventory(t *testing.T) {
	t.Parallel()

	failures := []string{
		"replacement",
		"late replacement",
		"list",
		"discovery",
		"pagination",
		"missing UID",
		"limit",
	}
	for _, failure := range failures {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()

			root := ownedObject("ConfigMap", "root", "default", "root-uid", nil)
			api := &observationAPI{
				root: root,
				children: []*unstructured.Unstructured{
					ownedObject("Pod", "child", "default", "child", root),
				},
			}

			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) { api.serve(t, w, r) },
				),
			)
			defer server.Close()

			client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
			require.NoError(t, err)
			require.NoError(t, client.Apply(t.Context(), root))

			api.failure = failure
			if failure == "missing UID" {
				api.children[0].SetUID("")
			}

			if failure == "limit" {
				for range 10000 {
					api.children = append(api.children, api.children[0].DeepCopy())
				}
			}

			children, err := client.ObserveChildren(
				t.Context(),
				[]*unstructured.Unstructured{root},
				time.Millisecond,
			)
			require.Error(t, err)
			assert.Empty(t, children, "partial inventory must never escape as successful coverage")
		})
	}
}

func TestObserveChildrenWaitsAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	root := ownedObject("ConfigMap", "root", "default", "root-uid", nil)
	api := &observationAPI{root: root}

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { api.serve(t, w, r) }),
	)
	defer server.Close()

	client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
	require.NoError(t, err)
	require.NoError(t, client.Apply(t.Context(), root))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	_, err = client.ObserveChildren(ctx, []*unstructured.Unstructured{root}, time.Minute)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, api.lists.Load())
	assert.Zero(t, api.reads.Load(), "collection must wait before reading the inventory")
}

func TestObserveChildrenRejectsEmptyAndUnanchoredObservations(t *testing.T) {
	t.Parallel()

	for _, applied := range []bool{true, false} {
		t.Run(
			map[bool]string{true: "no descendants", false: "not applied"}[applied],
			func(t *testing.T) {
				t.Parallel()

				root := ownedObject("ConfigMap", "root", "default", "root-uid", nil)
				api := &observationAPI{root: root}

				server := httptest.NewServer(
					http.HandlerFunc(
						func(w http.ResponseWriter, r *http.Request) { api.serve(t, w, r) },
					),
				)
				defer server.Close()

				client, err := ephemeral.NewApplier(kubeconfigFor(t, server.URL), "isolated")
				require.NoError(t, err)

				if applied {
					require.NoError(t, client.Apply(t.Context(), root))
				}

				_, err = client.ObserveChildren(
					t.Context(),
					[]*unstructured.Unstructured{root},
					time.Millisecond,
				)
				require.ErrorIs(t, err, ephemeral.ErrObservation)
			},
		)
	}
}

func (api *observationAPI) servePods(
	t *testing.T,
	writer http.ResponseWriter,
	request *http.Request,
) {
	t.Helper()
	api.lists.Add(1)

	if api.failure == "list" {
		http.Error(writer, "forbidden", http.StatusForbidden)

		return
	}

	page := &unstructured.UnstructuredList{
		Object: map[string]any{"apiVersion": "v1", "kind": "PodList"},
	}
	if request.URL.Query().Get("continue") == "" && len(api.children) > 0 {
		page.Items = []unstructured.Unstructured{*api.children[0]}
		page.SetContinue("page-2")
	} else if len(api.children) > 0 {
		for _, obj := range api.children[1:] {
			page.Items = append(page.Items, *obj)
		}
	}

	if api.failure == "pagination" {
		page.SetContinue("page-2")
	}

	assert.NoError(t, json.NewEncoder(writer).Encode(page))
}

func (api *observationAPI) rootResponse(request *http.Request) *unstructured.Unstructured {
	root := api.root.DeepCopy()
	if request.Method == http.MethodGet {
		reads := api.reads.Add(1)

		if api.failure == "replacement" || (api.failure == "late replacement" && reads > 1) {
			root.SetUID(types.UID("replacement"))
		}
	}

	return root
}
