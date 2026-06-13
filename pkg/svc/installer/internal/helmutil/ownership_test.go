package helmutil_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestIsGitOpsManaged(t *testing.T) {
	t.Parallel()

	t.Run("nil labels", testIsGitOpsManagedNilLabels)
	t.Run("empty labels", testIsGitOpsManagedEmptyLabels)
	t.Run("standard helm labels only", testIsGitOpsManagedStandardHelmLabels)
	t.Run("flux managed", testIsGitOpsManagedFlux)
	t.Run("flux name label only", testIsGitOpsManagedFluxNameOnly)
	t.Run("argocd managed", testIsGitOpsManagedArgoCD)
	t.Run("flux takes precedence over argocd", testIsGitOpsManagedFluxPrecedence)
}

func testIsGitOpsManagedNilLabels(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(nil)

	assert.Empty(t, controller)
	assert.False(t, managed)
}

func testIsGitOpsManagedEmptyLabels(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{})

	assert.Empty(t, controller)
	assert.False(t, managed)
}

func testIsGitOpsManagedStandardHelmLabels(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{
		"name":    "cert-manager",
		"owner":   "helm",
		"version": "1",
		"status":  "deployed",
	})

	assert.Empty(t, controller)
	assert.False(t, managed)
}

func testIsGitOpsManagedFlux(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{
		"name":                             "cert-manager",
		"owner":                            "helm",
		"helm.toolkit.fluxcd.io/name":      "cert-manager",
		"helm.toolkit.fluxcd.io/namespace": "flux-system",
	})

	assert.Equal(t, "Flux", controller)
	assert.True(t, managed)
}

func testIsGitOpsManagedFluxNameOnly(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{
		"helm.toolkit.fluxcd.io/name": "my-release",
	})

	assert.Equal(t, "Flux", controller)
	assert.True(t, managed)
}

func testIsGitOpsManagedArgoCD(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{
		"name":                          "cert-manager",
		"owner":                         "helm",
		"argocd.argoproj.io/managed-by": "argocd",
	})

	assert.Equal(t, "ArgoCD", controller)
	assert.True(t, managed)
}

func testIsGitOpsManagedFluxPrecedence(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{
		"helm.toolkit.fluxcd.io/name":   "cert-manager",
		"argocd.argoproj.io/managed-by": "argocd",
	})

	assert.Equal(t, "Flux", controller)
	assert.True(t, managed)
}

func TestSkipIfGitOpsManaged(t *testing.T) {
	t.Parallel()

	t.Run("not managed proceeds", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseStorageLabels(mock.Anything, "rel", "ns").
			Return(map[string]string{"owner": "helm"}, nil)

		skip, err := helmutil.SkipIfGitOpsManaged(context.Background(), client, "comp", "rel", "ns")

		require.NoError(t, err)
		assert.False(t, skip)
	})

	t.Run("no release storage proceeds", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseStorageLabels(mock.Anything, "rel", "ns").
			Return(nil, helm.ErrNoReleaseStorage)

		skip, err := helmutil.SkipIfGitOpsManaged(context.Background(), client, "comp", "rel", "ns")

		require.NoError(t, err)
		assert.False(t, skip)
	})

	t.Run("flux managed skips", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseStorageLabels(mock.Anything, "rel", "ns").
			Return(map[string]string{"helm.toolkit.fluxcd.io/name": "rel"}, nil)

		skip, err := helmutil.SkipIfGitOpsManaged(context.Background(), client, "comp", "rel", "ns")

		require.NoError(t, err)
		assert.True(t, skip)
	})

	t.Run("ownership error wrapped with component name", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		client.EXPECT().
			GetReleaseStorageLabels(mock.Anything, "rel", "ns").
			Return(nil, assert.AnError)

		skip, err := helmutil.SkipIfGitOpsManaged(context.Background(), client, "comp", "rel", "ns")

		require.Error(t, err)
		assert.False(t, skip)
		assert.Contains(t, err.Error(), "check release ownership for comp")
	})
}
