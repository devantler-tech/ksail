package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// errBoom is a sentinel non-Kubernetes error used to assert that the service wraps (rather than
// swallows) arbitrary client failures, and that the original error is preserved via %w.
var errBoom = errors.New("boom")

// newInterceptedClient builds a fake client wired with interceptor funcs so individual CRUD
// operations can be made to fail on demand, exercising the service's error paths directly.
func newInterceptedClient(
	t *testing.T,
	funcs interceptor.Funcs,
	objects ...client.Object,
) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha1.Cluster{}).
		WithInterceptorFuncs(funcs).
		Build()
}

func TestListReturnsEmptySliceNotNil(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newClient(t))

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.NotNil(t, list)
	// The service documents that it emits an empty array rather than null so clients don't have to
	// special-case a missing items field.
	require.NotNil(t, list.Items)
	assert.Empty(t, list.Items)
}

// TestListNormalizesNilItems pins the null-vs-empty contract at the source: when the backing client
// returns a list whose Items is nil, the service replaces it with an empty (non-nil) slice so the
// JSON response is [] rather than null.
func TestListNormalizesNilItems(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			// Return success without populating Items, leaving it nil for the service to normalize.
			return nil
		},
	}))

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.NotNil(t, list.Items)
	assert.Empty(t, list.Items)
}

func TestGetReturnsCluster(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newClient(t, sampleCluster()))

	cluster, err := service.Get(context.Background(), defaultNS, "c1")
	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, "c1", cluster.Name)
	assert.Equal(t, defaultNS, cluster.Namespace)
}

func TestListWrapsClientError(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errBoom
		},
	}))

	_, err := service.List(context.Background())
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "list clusters")
}

func TestGetWrapsClientError(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return errBoom
		},
	}))

	_, err := service.Get(context.Background(), defaultNS, "c1")
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "get cluster")
}

// TestCreatePropagatesNamespaceCheckError pins that a non-NotFound failure while checking whether
// the target namespace exists is propagated, not mistaken for "namespace missing" and swallowed —
// otherwise a transient API outage would be misread as "create the namespace".
func TestCreatePropagatesNamespaceCheckError(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Get: func(
			ctx context.Context,
			clt client.WithWatch,
			key client.ObjectKey,
			obj client.Object,
			opts ...client.GetOption,
		) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				return errBoom
			}

			return clt.Get(ctx, key, obj, opts...)
		},
	}))

	_, err := service.Create(context.Background(), sampleCluster())
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "check namespace")
}

// TestCreateToleratesNamespaceAlreadyExists pins the concurrency-race tolerance: when two requests
// race to create the on-demand namespace, the loser sees AlreadyExists and must still succeed.
func TestCreateToleratesNamespaceAlreadyExists(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Create: func(ctx context.Context, clt client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if ns, ok := obj.(*corev1.Namespace); ok {
				return apierrors.NewAlreadyExists(
					schema.GroupResource{Resource: "namespaces"},
					ns.GetName(),
				)
			}

			return clt.Create(ctx, obj, opts...)
		},
	}))

	cluster := sampleCluster()
	cluster.Namespace = "fresh-ns"

	result, err := service.Create(context.Background(), cluster)
	require.NoError(t, err)
	assert.Equal(t, "fresh-ns", result.Namespace)
}

// TestCreatePropagatesNamespaceCreateError pins that a genuine namespace-creation failure aborts the
// cluster create (the namespace is a prerequisite) rather than being silently tolerated.
func TestCreatePropagatesNamespaceCreateError(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Create: func(ctx context.Context, clt client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				return errBoom
			}

			return clt.Create(ctx, obj, opts...)
		},
	}))

	cluster := sampleCluster()
	cluster.Namespace = "fresh-ns"

	_, err := service.Create(context.Background(), cluster)
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "create namespace")
}

func TestCreateWrapsClusterCreateError(t *testing.T) {
	t.Parallel()

	// The namespace already exists, so ensureNamespace short-circuits and the failing call under
	// test is the Cluster create itself.
	existingNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: defaultNS}}

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Create: func(ctx context.Context, clt client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*v1alpha1.Cluster); ok {
				return errBoom
			}

			return clt.Create(ctx, obj, opts...)
		},
	}, existingNS))

	_, err := service.Create(context.Background(), sampleCluster())
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "create cluster")
}

func TestUpdateWrapsGetError(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return errBoom
		},
	}))

	_, err := service.Update(context.Background(), defaultNS, "c1", sampleCluster())
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "get cluster")
}

func TestUpdateWrapsUpdateError(t *testing.T) {
	t.Parallel()

	// The cluster exists so the Get succeeds; the failing call under test is the Update.
	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
			return errBoom
		},
	}, sampleCluster()))

	_, err := service.Update(context.Background(), defaultNS, "c1", sampleCluster())
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "update cluster")
}

func TestDeleteWrapsClientError(t *testing.T) {
	t.Parallel()

	service := api.NewCRClusterService(newInterceptedClient(t, interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
			return errBoom
		},
	}))

	err := service.Delete(context.Background(), defaultNS, "c1")
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "delete cluster")
}
