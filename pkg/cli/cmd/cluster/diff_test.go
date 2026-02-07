//nolint:testpackage // Testing internal DiffEngine requires same package
package cluster

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

const (
	// testValueEnabled is a common test value for component settings.
	testValueEnabled = "Enabled"
	// testRegistryAlt is an alternative registry address used in diff tests.
	testRegistryAlt = "localhost:6060"
)

func newBaseSpec() *v1alpha1.ClusterSpec {
	return &v1alpha1.ClusterSpec{
		Distribution:  v1alpha1.DistributionVanilla,
		Provider:      v1alpha1.ProviderDocker,
		CNI:           v1alpha1.CNIDefault,
		CSI:           v1alpha1.CSIDefault,
		MetricsServer: "Default",
		LoadBalancer:  "Default",
		CertManager:   "Disabled",
		PolicyEngine:  "None",
		GitOpsEngine:  "None",
		LocalRegistry: v1alpha1.LocalRegistry{Registry: "localhost:5050"},
		Vanilla:       v1alpha1.OptionsVanilla{MirrorsDir: "kind/mirrors"},
		Talos: v1alpha1.OptionsTalos{
			ControlPlanes: 1,
			Workers:       0,
			ISO:           122630,
		},
		Hetzner: v1alpha1.OptionsHetzner{
			ControlPlaneServerType: "cx23",
			WorkerServerType:       "cx23",
			Location:               "fsn1",
			NetworkName:            "test-network",
			NetworkCIDR:            "10.0.0.0/16",
			SSHKeyName:             "my-key",
		},
	}
}

// clone returns a deep-enough copy of ClusterSpec for diff testing.
func clone(spec *v1alpha1.ClusterSpec) *v1alpha1.ClusterSpec {
	out := *spec
	out.Vanilla = spec.Vanilla
	out.Talos = spec.Talos
	out.Hetzner = spec.Hetzner
	out.LocalRegistry = spec.LocalRegistry

	return &out
}

func TestDiffEngine_NilSpecs(t *testing.T) {
	t.Parallel()

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	tests := []struct {
		name    string
		oldSpec *v1alpha1.ClusterSpec
		newSpec *v1alpha1.ClusterSpec
	}{
		{name: "both nil", oldSpec: nil, newSpec: nil},
		{name: "old nil", oldSpec: nil, newSpec: newBaseSpec()},
		{name: "new nil", oldSpec: newBaseSpec(), newSpec: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := engine.ComputeDiff(testCase.oldSpec, testCase.newSpec)
			if result.TotalChanges() != 0 {
				t.Errorf("expected 0 changes for nil spec, got %d", result.TotalChanges())
			}
		})
	}
}

func TestDiffEngine_NoChanges(t *testing.T) {
	t.Parallel()

	spec := newBaseSpec()
	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	result := engine.ComputeDiff(spec, spec)

	if result.TotalChanges() != 0 {
		t.Errorf("identical specs should produce 0 changes, got %d", result.TotalChanges())
	}
}

func TestDiffEngine_DistributionChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Distribution = v1alpha1.DistributionTalos

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if !result.HasRecreateRequired() {
		t.Fatal("distribution change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.distribution",
		"Vanilla", "Talos", types.ChangeCategoryRecreateRequired)
}

func TestDiffEngine_ProviderChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Provider = v1alpha1.ProviderHetzner

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if !result.HasRecreateRequired() {
		t.Fatal("provider change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.provider",
		"Docker", "Hetzner", types.ChangeCategoryRecreateRequired)
}

//nolint:funlen // Table-driven test with multiple component scenarios is clearer as single function
func TestDiffEngine_ComponentChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(spec *v1alpha1.ClusterSpec)
		field    string
		oldValue string
		newValue string
	}{
		{
			name:     "CNI change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.CNI = v1alpha1.CNICilium },
			field:    "cluster.cni",
			oldValue: "Default",
			newValue: "Cilium",
		},
		{
			name:     "CSI change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.CSI = v1alpha1.CSIEnabled },
			field:    "cluster.csi",
			oldValue: "Default",
			newValue: testValueEnabled,
		},
		{
			name:     "MetricsServer change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.MetricsServer = testValueEnabled },
			field:    "cluster.metricsServer",
			oldValue: "Default",
			newValue: testValueEnabled,
		},
		{
			name:     "LoadBalancer change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.LoadBalancer = testValueEnabled },
			field:    "cluster.loadBalancer",
			oldValue: "Default",
			newValue: testValueEnabled,
		},
		{
			name:     "CertManager change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.CertManager = testValueEnabled },
			field:    "cluster.certManager",
			oldValue: "Disabled",
			newValue: testValueEnabled,
		},
		{
			name:     "PolicyEngine change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.PolicyEngine = "Kyverno" },
			field:    "cluster.policyEngine",
			oldValue: "None",
			newValue: "Kyverno",
		},
		{
			name:     "GitOpsEngine change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.GitOpsEngine = "Flux" },
			field:    "cluster.gitOpsEngine",
			oldValue: "None",
			newValue: "Flux",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			testCase.mutate(newer)

			engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
			result := engine.ComputeDiff(old, newer)

			if !result.HasInPlaceChanges() {
				t.Fatal("component change should be in-place")
			}

			if result.HasRecreateRequired() {
				t.Fatal("component change should not require recreate")
			}

			assertSingleChange(t, result.InPlaceChanges, testCase.field,
				testCase.oldValue, testCase.newValue, types.ChangeCategoryInPlace)
		})
	}
}

func TestDiffEngine_LocalRegistryChange_Vanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.LocalRegistry.Registry = testRegistryAlt

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if !result.HasRecreateRequired() {
		t.Fatal("Vanilla local registry change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.localRegistry.registry",
		"localhost:5050", testRegistryAlt, types.ChangeCategoryRecreateRequired)
}

func TestDiffEngine_LocalRegistryChange_Talos(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos

	newer := clone(old)
	newer.LocalRegistry.Registry = testRegistryAlt

	engine := NewDiffEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if !result.HasInPlaceChanges() {
		t.Fatal("Talos local registry change should be in-place")
	}

	if result.HasRecreateRequired() {
		t.Fatal("Talos local registry change should not require recreate")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.localRegistry.registry",
		"localhost:5050", testRegistryAlt, types.ChangeCategoryInPlace)
}

func TestDiffEngine_LocalRegistryChange_K3s(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionK3s

	newer := clone(old)
	newer.LocalRegistry.Registry = testRegistryAlt

	engine := NewDiffEngine(v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if !result.HasInPlaceChanges() {
		t.Fatal("K3s local registry change should be in-place")
	}

	if result.HasRecreateRequired() {
		t.Fatal("K3s local registry change should not require recreate")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.localRegistry.registry",
		"localhost:5050", testRegistryAlt, types.ChangeCategoryInPlace)
}

func TestDiffEngine_VanillaOptionsChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Vanilla.MirrorsDir = "other/mirrors"

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if !result.HasRecreateRequired() {
		t.Fatal("Vanilla mirrorsDir change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.vanilla.mirrorsDir",
		"kind/mirrors", "other/mirrors", types.ChangeCategoryRecreateRequired)
}

func TestDiffEngine_VanillaOptionsChange_SkippedForNonVanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Vanilla.MirrorsDir = "other/mirrors"

	engine := NewDiffEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	// Vanilla options should be ignored when distribution is Talos
	for _, change := range result.AllChanges() {
		if change.Field == "cluster.vanilla.mirrorsDir" {
			t.Fatal("Vanilla mirrorsDir change should be ignored for non-Vanilla distributions")
		}
	}
}

func TestDiffEngine_TalosOptionsChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(spec *v1alpha1.ClusterSpec)
		field    string
		oldValue string
		newValue string
	}{
		{
			name:     "control plane count change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Talos.ControlPlanes = 3 },
			field:    "cluster.talos.controlPlanes",
			oldValue: "1",
			newValue: "3",
		},
		{
			name:     "worker count change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Talos.Workers = 2 },
			field:    "cluster.talos.workers",
			oldValue: "0",
			newValue: "2",
		},
		{
			name:     "ISO change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Talos.ISO = 122629 },
			field:    "cluster.talos.iso",
			oldValue: "122630",
			newValue: "122629",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			testCase.mutate(newer)

			engine := NewDiffEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
			result := engine.ComputeDiff(old, newer)

			if !result.HasInPlaceChanges() {
				t.Fatal("Talos option change should be in-place")
			}

			assertSingleChange(t, result.InPlaceChanges, testCase.field,
				testCase.oldValue, testCase.newValue, types.ChangeCategoryInPlace)
		})
	}
}

func TestDiffEngine_TalosOptionsChange_SkippedForNonTalos(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Talos.ControlPlanes = 5

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.talos.controlPlanes" {
			t.Fatal("Talos options should be ignored for non-Talos distributions")
		}
	}
}

func TestDiffEngine_HetznerOptionsChange_RecreateRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(spec *v1alpha1.ClusterSpec)
		field    string
		oldValue string
		newValue string
	}{
		{
			name:     "control plane server type change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Hetzner.ControlPlaneServerType = "cpx21" },
			field:    "cluster.hetzner.controlPlaneServerType",
			oldValue: "cx23",
			newValue: "cpx21",
		},
		{
			name:     "location change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Hetzner.Location = "nbg1" },
			field:    "cluster.hetzner.location",
			oldValue: "fsn1",
			newValue: "nbg1",
		},
		{
			name:     "network name change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Hetzner.NetworkName = "new-network" },
			field:    "cluster.hetzner.networkName",
			oldValue: "test-network",
			newValue: "new-network",
		},
		{
			name:     "network CIDR change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Hetzner.NetworkCIDR = "10.1.0.0/16" },
			field:    "cluster.hetzner.networkCidr",
			oldValue: "10.0.0.0/16",
			newValue: "10.1.0.0/16",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			testCase.mutate(newer)

			engine := NewDiffEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer)

			if !result.HasRecreateRequired() {
				t.Fatal("Hetzner change should require recreate")
			}

			assertSingleChange(t, result.RecreateRequired, testCase.field,
				testCase.oldValue, testCase.newValue, types.ChangeCategoryRecreateRequired)
		})
	}
}

func TestDiffEngine_HetznerOptionsChange_InPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(spec *v1alpha1.ClusterSpec)
		field    string
		oldValue string
		newValue string
	}{
		{
			name:     "worker server type change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Hetzner.WorkerServerType = "cpx21" },
			field:    "cluster.hetzner.workerServerType",
			oldValue: "cx23",
			newValue: "cpx21",
		},
		{
			name:     "SSH key name change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.Hetzner.SSHKeyName = "other-key" },
			field:    "cluster.hetzner.sshKeyName",
			oldValue: "my-key",
			newValue: "other-key",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			testCase.mutate(newer)

			engine := NewDiffEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer)

			if !result.HasInPlaceChanges() {
				t.Fatal("Hetzner change should be in-place")
			}

			if result.HasRecreateRequired() {
				t.Fatal("Hetzner worker/SSH change should not require recreate")
			}

			assertSingleChange(t, result.InPlaceChanges, testCase.field,
				testCase.oldValue, testCase.newValue, types.ChangeCategoryInPlace)
		})
	}
}

func TestDiffEngine_HetznerOptionsChange_SkippedForDocker(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Hetzner.Location = "nbg1"

	engine := NewDiffEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.hetzner.location" {
			t.Fatal("Hetzner options should be ignored for Docker provider")
		}
	}
}

func TestDiffEngine_MultipleChanges(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.CNI = v1alpha1.CNICilium       // in-place
	newer.CSI = v1alpha1.CSIEnabled      // in-place
	newer.Vanilla.MirrorsDir = "changed" // recreate-required

	engine := NewDiffEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer)

	if len(result.InPlaceChanges) != 2 {
		t.Errorf("expected 2 in-place changes, got %d", len(result.InPlaceChanges))
	}

	if len(result.RecreateRequired) != 1 {
		t.Errorf("expected 1 recreate-required change, got %d", len(result.RecreateRequired))
	}

	if result.TotalChanges() != 3 {
		t.Errorf("expected 3 total changes, got %d", result.TotalChanges())
	}

	if !result.NeedsUserConfirmation() {
		t.Error("should need user confirmation with recreate-required changes")
	}
}

//nolint:funlen // Table-driven test with multiple sub-tests is clearer as single function
func TestMergeProvisionerDiff(t *testing.T) {
	t.Parallel()

	t.Run("nil provisioner diff is no-op", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges: []types.Change{
				{Field: "cluster.cni", Category: types.ChangeCategoryInPlace},
			},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{},
		}

		mergeProvisionerDiff(main, nil)

		if len(main.InPlaceChanges) != 1 {
			t.Errorf("expected 1 in-place change, got %d", len(main.InPlaceChanges))
		}
	})

	t.Run("adds unique provisioner changes", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "cluster.cni"}},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{},
		}

		provisioner := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "talos.workers"}},
			RebootRequired:   []types.Change{{Field: "machine.install"}},
			RecreateRequired: []types.Change{},
		}

		mergeProvisionerDiff(main, provisioner)

		if len(main.InPlaceChanges) != 2 {
			t.Errorf("expected 2 in-place changes, got %d", len(main.InPlaceChanges))
		}

		if len(main.RebootRequired) != 1 {
			t.Errorf("expected 1 reboot-required change, got %d", len(main.RebootRequired))
		}
	})

	t.Run("deduplicates existing fields", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "cluster.cni"}},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{{Field: "cluster.distribution"}},
		}

		provisioner := &types.UpdateResult{
			InPlaceChanges:   []types.Change{{Field: "cluster.cni"}}, // duplicate
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{{Field: "cluster.distribution"}}, // duplicate
		}

		mergeProvisionerDiff(main, provisioner)

		if len(main.InPlaceChanges) != 1 {
			t.Errorf("expected 1 in-place change (deduplicated), got %d", len(main.InPlaceChanges))
		}

		if len(main.RecreateRequired) != 1 {
			t.Errorf(
				"expected 1 recreate change (deduplicated), got %d",
				len(main.RecreateRequired),
			)
		}
	})

	t.Run("deduplicates fields with cluster prefix mismatch", func(t *testing.T) {
		t.Parallel()

		main := &types.UpdateResult{
			InPlaceChanges:   []types.Change{},
			RebootRequired:   []types.Change{},
			RecreateRequired: []types.Change{{Field: "cluster.vanilla.mirrorsDir"}},
		}

		provisioner := &types.UpdateResult{
			InPlaceChanges: []types.Change{},
			RebootRequired: []types.Change{},
			RecreateRequired: []types.Change{
				{Field: "vanilla.mirrorsDir"},
			}, // same field, no prefix
		}

		mergeProvisionerDiff(main, provisioner)

		if len(main.RecreateRequired) != 1 {
			t.Errorf(
				"expected 1 recreate change (deduplicated across prefix), got %d",
				len(main.RecreateRequired),
			)
		}
	})
}

// assertSingleChange validates that exactly one change matches the expected parameters.
func assertSingleChange(
	t *testing.T,
	changes []types.Change,
	expectedField, expectedOld, expectedNew string,
	expectedCategory types.ChangeCategory,
) {
	t.Helper()

	found := false

	for _, change := range changes {
		if change.Field == expectedField {
			found = true

			if change.OldValue != expectedOld {
				t.Errorf(
					"field %s: expected OldValue %q, got %q",
					expectedField,
					expectedOld,
					change.OldValue,
				)
			}

			if change.NewValue != expectedNew {
				t.Errorf(
					"field %s: expected NewValue %q, got %q",
					expectedField,
					expectedNew,
					change.NewValue,
				)
			}

			if change.Category != expectedCategory {
				t.Errorf(
					"field %s: expected Category %v, got %v",
					expectedField,
					expectedCategory,
					change.Category,
				)
			}

			if change.Reason == "" {
				t.Errorf("field %s: expected non-empty Reason", expectedField)
			}
		}
	}

	if !found {
		t.Errorf(
			"expected change for field %q not found in %d changes",
			expectedField,
			len(changes),
		)
	}
}
