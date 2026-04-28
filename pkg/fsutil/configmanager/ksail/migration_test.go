package configmanager_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
)

func TestMigrateDeprecatedNodeCounts_CopiesWhenNewUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Talos.ControlPlanes = 3
	cfg.Spec.Cluster.Talos.Workers = 2

	var out bytes.Buffer

	err := configmanager.MigrateDeprecatedNodeCountsForTest(cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Spec.Cluster.ControlPlanes != 3 {
		t.Fatalf("expected ControlPlanes=3, got %d", cfg.Spec.Cluster.ControlPlanes)
	}

	if cfg.Spec.Cluster.Workers != 2 {
		t.Fatalf("expected Workers=2, got %d", cfg.Spec.Cluster.Workers)
	}

	if cfg.Spec.Cluster.Talos.ControlPlanes != 0 || cfg.Spec.Cluster.Talos.Workers != 0 {
		t.Fatalf("expected legacy fields to be zeroed after migration, got cp=%d workers=%d",
			cfg.Spec.Cluster.Talos.ControlPlanes, cfg.Spec.Cluster.Talos.Workers)
	}

	warning := out.String()
	if !strings.Contains(warning, "spec.cluster.talos.controlPlanes is deprecated") {
		t.Fatalf("expected deprecation warning for controlPlanes, got %q", warning)
	}

	if !strings.Contains(warning, "spec.cluster.talos.workers is deprecated") {
		t.Fatalf("expected deprecation warning for workers, got %q", warning)
	}
}

func TestMigrateDeprecatedNodeCounts_NoOpWhenLegacyUnset(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ControlPlanes = 5

	var out bytes.Buffer

	err := configmanager.MigrateDeprecatedNodeCountsForTest(cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Spec.Cluster.ControlPlanes != 5 {
		t.Fatalf("expected ControlPlanes=5, got %d", cfg.Spec.Cluster.ControlPlanes)
	}

	if out.Len() != 0 {
		t.Fatalf("expected no warning output, got %q", out.String())
	}
}

func TestMigrateDeprecatedNodeCounts_ConflictReturnsError(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ControlPlanes = 3
	cfg.Spec.Cluster.Talos.ControlPlanes = 5

	err := configmanager.MigrateDeprecatedNodeCountsForTest(cfg, nil)
	if !errors.Is(err, configmanager.ErrDeprecatedFieldConflict) {
		t.Fatalf("expected ErrDeprecatedFieldConflict, got %v", err)
	}
}

func TestMigrateDeprecatedNodeCounts_MatchingValuesAreNotConflicts(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ControlPlanes = 3
	cfg.Spec.Cluster.Talos.ControlPlanes = 3

	var out bytes.Buffer

	err := configmanager.MigrateDeprecatedNodeCountsForTest(cfg, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Spec.Cluster.ControlPlanes != 3 {
		t.Fatalf("expected ControlPlanes=3, got %d", cfg.Spec.Cluster.ControlPlanes)
	}

	if cfg.Spec.Cluster.Talos.ControlPlanes != 0 {
		t.Fatalf("expected legacy field zeroed, got %d", cfg.Spec.Cluster.Talos.ControlPlanes)
	}

	if !strings.Contains(out.String(), "deprecated") {
		t.Fatalf("expected deprecation warning, got %q", out.String())
	}
}
