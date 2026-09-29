package diff_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

func newAutoscalerEngine() *specdiff.Engine {
	return specdiff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
}

// TestCheckAutoscalerValuesSurfacesRenderedValuesDrift is the core of
// ksail#7366: a KSail upgrade that changes only the autoscaler values it
// renders produces no spec change, so it must surface here as an in-place
// change or `cluster update` reports success and leaves the old values running.
func TestCheckAutoscalerValuesSurfacesRenderedValuesDrift(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newAutoscalerEngine().CheckAutoscalerValues(true, result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf(
			"drifted autoscaler values must produce exactly one in-place change, got %d",
			len(result.InPlaceChanges),
		)
	}

	change := result.InPlaceChanges[0]
	if change.Field != specdiff.AutoscalerValuesField {
		t.Errorf("field = %q, want %q", change.Field, specdiff.AutoscalerValuesField)
	}

	// The reconciler routes every cluster.autoscaler.node.* field to the one
	// autoscaler Helm upgrade; a key outside that prefix would be detected and
	// then never applied.
	if !strings.HasPrefix(change.Field, "cluster.autoscaler.node.") {
		t.Errorf("field %q is outside the autoscaler reconcile prefix", change.Field)
	}

	if len(result.RecreateRequired) != 0 || len(result.RebootRequired) != 0 {
		t.Errorf("a values-only upgrade must never demand recreation or a reboot")
	}
}

// TestCheckAutoscalerValuesStaysSilentWithoutDrift keeps an up-to-date release
// out of the change summary.
func TestCheckAutoscalerValuesStaysSilentWithoutDrift(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newAutoscalerEngine().CheckAutoscalerValues(false, result)

	if result.TotalChanges() != 0 {
		t.Fatalf("undrifted autoscaler values must produce no change, got %+v", result)
	}
}
