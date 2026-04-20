package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Package-level sink prevents the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variable is required to prevent compiler optimization.
var benchComputeDiffSink *clusterupdate.UpdateResult

// BenchmarkComputeDiff_NoChanges measures ComputeDiff overhead when old and new
// specs are identical. This is the common steady-state case: re-running
// "ksail cluster update" after the cluster already matches the desired config.
func BenchmarkComputeDiff_NoChanges(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	oldSpec := newBaseSpec()
	newSpec := clone(oldSpec)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(oldSpec, newSpec, nil, nil)
	}
}

// BenchmarkComputeDiff_AllInPlaceChanges measures ComputeDiff when every
// component field (CNI, CSI, metrics-server, load-balancer, cert-manager,
// policy engine, GitOps engine) changes to a non-default value. All are
// in-place (no reboot or recreate needed).
func BenchmarkComputeDiff_AllInPlaceChanges(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	oldSpec := newBaseSpec()
	newSpec := clone(oldSpec)

	newSpec.CNI = v1alpha1.CNICilium
	newSpec.CSI = testValueEnabled
	newSpec.MetricsServer = testValueEnabled
	newSpec.LoadBalancer = testValueEnabled
	newSpec.CertManager = testValueEnabled
	newSpec.PolicyEngine = "Kyverno"
	newSpec.GitOpsEngine = "Flux"

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(oldSpec, newSpec, nil, nil)
	}
}

// BenchmarkComputeDiff_RecreateRequired measures ComputeDiff when changes
// imply that a cluster recreation will be needed (for example, a distribution
// or provider change). This does not exercise any early-exit optimization;
// ComputeDiff always evaluates all change categories.
func BenchmarkComputeDiff_RecreateRequired(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	oldSpec := newBaseSpec()
	newSpec := clone(oldSpec)

	newSpec.Distribution = v1alpha1.DistributionK3s

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(oldSpec, newSpec, nil, nil)
	}
}

// BenchmarkComputeDiff_MixedCategories measures ComputeDiff with a realistic
// mix of changes across two categories (in-place, recreate-required). This
// simulates a major config migration where a component change accompanies a
// distribution or registry change.
func BenchmarkComputeDiff_MixedCategories(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	oldSpec := newBaseSpec()
	newSpec := clone(oldSpec)

	// in-place
	newSpec.CNI = v1alpha1.CNICilium
	newSpec.CertManager = "Enabled"
	// recreate-required
	newSpec.Distribution = v1alpha1.DistributionK3s
	newSpec.LocalRegistry.Registry = "localhost:6060"

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(oldSpec, newSpec, nil, nil)
	}
}

// BenchmarkComputeDiff_TalosOptions measures ComputeDiff for a Talos
// distribution with Talos-specific field changes (control planes, workers, ISO).
func BenchmarkComputeDiff_TalosOptions(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	oldSpec := newBaseSpec()
	oldSpec.Distribution = v1alpha1.DistributionTalos
	newSpec := clone(oldSpec)

	newSpec.Talos.ControlPlanes = 3
	newSpec.Talos.Workers = 2
	newSpec.Talos.ISO = 999999

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(oldSpec, newSpec, nil, nil)
	}
}

// BenchmarkComputeDiff_HetznerOptions measures ComputeDiff for a Hetzner
// provider with Hetzner-specific field changes (server types, location, network).
func BenchmarkComputeDiff_HetznerOptions(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	oldSpec := newBaseSpec()
	oldSpec.Distribution = v1alpha1.DistributionTalos
	oldSpec.Provider = v1alpha1.ProviderHetzner
	newSpec := clone(oldSpec)

	oldProvider := newBaseProviderSpec()
	newProvider := cloneProvider(oldProvider)
	newProvider.Hetzner.WorkerServerType = "cx43"
	newProvider.Hetzner.SSHKeyName = "new-key"

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(oldSpec, newSpec, oldProvider, newProvider)
	}
}

// BenchmarkComputeDiff_NilSpec measures the fast-path when one spec is nil.
// Provisioners that cannot introspect state return nil; this path should be cheap.
func BenchmarkComputeDiff_NilSpec(b *testing.B) {
	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	newSpec := newBaseSpec()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchComputeDiffSink = engine.ComputeDiff(nil, newSpec, nil, nil)
	}
}
