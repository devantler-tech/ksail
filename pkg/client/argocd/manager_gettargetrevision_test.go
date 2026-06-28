package argocd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

var errSimulatedGetFailure = errors.New("simulated get failure")

func TestManagerGetCurrentTargetRevision_ReturnsErrorForNilContext(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	//nolint:staticcheck // SA1012: intentionally testing nil context error handling
	rev, err := testMgr.mgr.GetCurrentTargetRevision(nil, "ksail")
	require.Error(t, err)
	require.ErrorContains(t, err, "context is nil")
	require.Empty(t, rev)
}

func TestManagerGetCurrentTargetRevision_ReturnsEmptyForNonExistentApplication(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	rev, err := testMgr.mgr.GetCurrentTargetRevision(context.Background(), "ksail")
	require.NoError(t, err)
	require.Empty(t, rev)
}

func TestManagerGetCurrentTargetRevision_ReturnsConfiguredRevision(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	err := testMgr.mgr.Ensure(context.Background(), argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v1",
	})
	require.NoError(t, err)

	rev, err := testMgr.mgr.GetCurrentTargetRevision(context.Background(), "ksail")
	require.NoError(t, err)
	require.Equal(t, "v1", rev)
}

func TestManagerGetCurrentTargetRevision_EmptyNameDefaultsToKsail(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	err := testMgr.mgr.Ensure(context.Background(), argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v9",
	})
	require.NoError(t, err)

	// An empty application name falls back to the default "ksail" Application.
	rev, err := testMgr.mgr.GetCurrentTargetRevision(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "v9", rev)
}

func TestManagerGetCurrentTargetRevision_ReturnsEmptyWhenFieldAbsent(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	// Seed an Application whose spec.source omits targetRevision entirely.
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      "no-rev",
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"repoURL": "oci://local-registry:5000/demo",
			},
		},
	}}

	_, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Create(context.Background(), app, metav1.CreateOptions{})
	require.NoError(t, err)

	rev, err := testMgr.mgr.GetCurrentTargetRevision(context.Background(), "no-rev")
	require.NoError(t, err)
	require.Empty(t, rev)
}

func TestManagerGetCurrentTargetRevision_ReturnsErrorForMalformedRevision(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	// Seed an Application whose spec.source.targetRevision is a non-string,
	// which makes unstructured.NestedString return a type error.
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      "bad-rev",
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"targetRevision": int64(42),
			},
		},
	}}

	_, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Create(context.Background(), app, metav1.CreateOptions{})
	require.NoError(t, err)

	rev, err := testMgr.mgr.GetCurrentTargetRevision(context.Background(), "bad-rev")
	require.Error(t, err)
	require.ErrorContains(t, err, "read targetRevision")
	require.Empty(t, rev)
}

func TestManagerGetCurrentTargetRevision_ReturnsErrorWhenGetFails(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	// A non-NotFound Get failure must be wrapped and surfaced, not swallowed.
	testMgr.dyn.PrependReactor("get", "applications", func(
		_ k8stesting.Action,
	) (bool, runtime.Object, error) {
		return true, nil, errSimulatedGetFailure
	})

	rev, err := testMgr.mgr.GetCurrentTargetRevision(context.Background(), "ksail")
	require.Error(t, err)
	require.ErrorContains(t, err, "get Argo CD Application")
	require.ErrorContains(t, err, "simulated get failure")
	require.Empty(t, rev)
}
