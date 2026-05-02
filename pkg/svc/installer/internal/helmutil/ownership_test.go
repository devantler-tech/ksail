package helmutil_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, "", controller)
	assert.False(t, managed)
}

func testIsGitOpsManagedEmptyLabels(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{})

	assert.Equal(t, "", controller)
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

	assert.Equal(t, "", controller)
	assert.False(t, managed)
}

func testIsGitOpsManagedFlux(t *testing.T) {
	t.Helper()
	t.Parallel()

	controller, managed := helmutil.IsGitOpsManaged(map[string]string{
		"name":                            "cert-manager",
		"owner":                           "helm",
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
