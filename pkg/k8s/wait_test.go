package k8s_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestWaitForAuthorizedRead_Succeeds verifies the authorized read returns nil
// as soon as listing namespaces succeeds.
func TestWaitForAuthorizedRead_Succeeds(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	err := k8s.WaitForAuthorizedReadForTest(context.Background(), clientset)

	require.NoError(t, err)
}

// TestWaitForAuthorizedRead_RetriesTransientForbidden verifies a transient 403
// (authorizer warm-up) is retried rather than treated as fatal, and the wait
// succeeds once the read is authorized.
func TestWaitForAuthorizedRead_RetriesTransientForbidden(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	var calls atomic.Int32

	clientset.PrependReactor(
		"list", "namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			// Fail the first call with Forbidden, then allow the default
			// tracker to serve the (empty) list.
			if calls.Add(1) == 1 {
				return true, nil, apierrors.NewForbidden(
					schema.GroupResource{Resource: "namespaces"},
					"",
					assert.AnError,
				)
			}

			return false, nil, nil
		},
	)

	err := k8s.WaitForAuthorizedReadForTest(context.Background(), clientset)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls.Load(), int32(2), "expected the forbidden read to be retried")
}

// TestWaitForAuthorizedRead_TimesOut verifies the wait surfaces
// ErrClusterNotReady (wrapping the last error) when the read never succeeds
// before the context deadline.
func TestWaitForAuthorizedRead_TimesOut(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	clientset.PrependReactor(
		"list", "namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Resource: "namespaces"},
				"",
				assert.AnError,
			)
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := k8s.WaitForAuthorizedReadForTest(ctx, clientset)

	require.ErrorIs(t, err, k8s.ErrClusterNotReady)
}

// TestWaitForClusterReady_InvalidKubeconfig verifies the public entry point
// surfaces an error for a bogus context with no reachable API server (the
// API-server wait fails fast against the empty/short deadline).
func TestWaitForClusterReady_InvalidKubeconfig(t *testing.T) {
	t.Parallel()

	// An empty path resolves to the default kubeconfig; pair it with a
	// context that will not exist so client construction or the API-server
	// wait fails rather than hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := k8s.WaitForClusterReady(ctx, t.TempDir()+"/missing-kubeconfig", "does-not-exist")

	require.Error(t, err)
}
