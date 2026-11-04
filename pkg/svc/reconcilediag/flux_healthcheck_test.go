package reconcilediag_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// TestFluxCollector_HelmReleaseFailingHealthCheckExpr pins how `workload reconcile`
// treats a HelmRelease generated with --health-check-expr. Flux evaluates the CEL
// expressions in helm-controller and folds the verdict into the release's Ready
// condition, so a false expression must surface as a not-ready release carrying the
// controller's reason and message, while a passing one stays out of the report.
func TestFluxCollector_HelmReleaseFailingHealthCheckExpr(t *testing.T) {
	t.Parallel()

	helmReleaseGVK := schema.GroupVersionKind{
		Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease",
	}
	healthCheckExprs := []any{
		map[string]any{
			"apiVersion": "bitnami.com/v1alpha1",
			"kind":       "SealedSecret",
			"current":    "status.conditions.filter(e, e.type == 'Synced').all(e, e.status == 'True')",
		},
	}

	failing := newFluxCR(helmReleaseGVK, "sealed-secrets", "kube-system", "False",
		"UpgradeFailed", "SealedSecret/kube-system/app status: 'InProgress'")
	failing.Object["spec"] = map[string]any{"healthCheckExprs": healthCheckExprs}

	passing := newFluxCR(helmReleaseGVK, "healthy", "kube-system", "True",
		"UpgradeSucceeded", "Helm upgrade succeeded")
	passing.Object["spec"] = map[string]any{"healthCheckExprs": healthCheckExprs}

	collector := &reconcilediag.FluxCollector{
		Dynamic:   newDynamicClient(failing, passing),
		Clientset: k8sfake.NewClientset(),
	}

	report := collector.Collect(t.Context())
	require.False(t, report.IsEmpty())

	section := report.Sections[1]
	assert.Equal(t, helmReleasesHeading, section.Heading)
	require.Len(t, section.Resources, 1, "only the release whose health check fails is reported")
	assert.Equal(t, "sealed-secrets", section.Resources[0].Name)
	assert.Equal(t, "UpgradeFailed", section.Resources[0].Reason)
	assert.Contains(t, section.Resources[0].Message, "SealedSecret")
}
