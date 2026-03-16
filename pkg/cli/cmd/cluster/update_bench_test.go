package cluster_test

import (
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
)

// Package-level sink prevents the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variable is required to prevent compiler optimization.
var benchFormatDiffTableSink string

// newInPlaceDiff builds an UpdateResult with count in-place changes.
func newInPlaceDiff(count int) *clusterupdate.UpdateResult {
	result := clusterupdate.NewEmptyUpdateResult()

	fields := []struct{ field, old, new string }{
		{"cluster.cni", "Default", "Cilium"},
		{"cluster.csi", "Default", "Enabled"},
		{"cluster.metricsServer", "Default", "Enabled"},
		{"cluster.loadBalancer", "Default", "Enabled"},
		{"cluster.certManager", "Disabled", "Enabled"},
		{"cluster.policyEngine", "None", "Kyverno"},
		{"cluster.gitOpsEngine", "None", "Flux"},
		{"cluster.localRegistry.registry", "", "localhost:5050"},
		{"cluster.talos.workers", "0", "2"},
		{"cluster.hetzner.sshKeyName", "old-key", "new-key"},
	}

	for i := range count {
		idx := i % len(fields)
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    fields[idx].field,
			OldValue: fields[idx].old,
			NewValue: fields[idx].new,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "component can be switched via Helm",
		})
	}

	return result
}

// newMixedDiff builds an UpdateResult with changes across all three categories.
func newMixedDiff() *clusterupdate.UpdateResult {
	result := clusterupdate.NewEmptyUpdateResult()

	result.RecreateRequired = []clusterupdate.Change{
		{
			Field:    "cluster.distribution",
			OldValue: "Vanilla",
			NewValue: "K3s",
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "changing distribution requires recreation",
		},
		{
			Field:    "cluster.localRegistry.registry",
			OldValue: "localhost:5050",
			NewValue: "localhost:6060",
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "Kind requires recreate for registry changes",
		},
	}
	result.RebootRequired = []clusterupdate.Change{
		{
			Field:    "cluster.talos.iso",
			OldValue: "122630",
			NewValue: "999999",
			Category: clusterupdate.ChangeCategoryRebootRequired,
			Reason:   "ISO change affects provisioned nodes",
		},
	}
	result.InPlaceChanges = []clusterupdate.Change{
		{
			Field:    "cluster.cni",
			OldValue: "Default",
			NewValue: "Cilium",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "CNI can be switched via Helm",
		},
		{
			Field:    "cluster.policyEngine",
			OldValue: "None",
			NewValue: "Kyverno",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "policy engine can be switched via Helm",
		},
	}

	return result
}

// BenchmarkFormatDiffTable_SingleChange measures table formatting with exactly
// one in-place change. This is the smallest realistic non-zero input.
func BenchmarkFormatDiffTable_SingleChange(b *testing.B) {
	diff := newInPlaceDiff(1)
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = clusterpkg.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_SmallDiff measures table formatting with a typical
// small diff (3 in-place changes). This represents a common incremental update.
func BenchmarkFormatDiffTable_SmallDiff(b *testing.B) {
	diff := newInPlaceDiff(3)
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = clusterpkg.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_MixedCategories measures table formatting with
// changes across all three severity categories (recreate, reboot, in-place).
func BenchmarkFormatDiffTable_MixedCategories(b *testing.B) {
	diff := newMixedDiff()
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = clusterpkg.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_LargeDiff measures table formatting with many
// changes (10 rows). This stress-tests column-width computation and
// strings.Builder pre-allocation sizing.
func BenchmarkFormatDiffTable_LargeDiff(b *testing.B) {
	diff := newInPlaceDiff(10)
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = clusterpkg.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_WideValues measures formatting overhead when
// field names and values are longer than the column headers, exercising
// the dynamic column-width computation path.
func BenchmarkFormatDiffTable_WideValues(b *testing.B) {
	result := clusterupdate.NewEmptyUpdateResult()
	result.InPlaceChanges = []clusterupdate.Change{
		{
			Field:    "cluster.hetzner.controlPlaneServerType",
			OldValue: "cx23",
			NewValue: "cx53",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "new worker servers will use the new type; existing workers unchanged",
		},
		{
			Field:    "cluster.hetzner.networkName",
			OldValue: "legacy-network-name",
			NewValue: "production-network-name",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "cannot migrate servers between networks",
		},
	}

	total := result.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = clusterpkg.ExportFormatDiffTable(result, total)
	}
}
