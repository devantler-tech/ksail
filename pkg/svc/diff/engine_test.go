package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

const (
	testValueEnabled = "Enabled"
	testRegistryAlt  = "localhost:6060"
)

func newBaseSpec() *v1alpha1.ClusterSpec {
	return &v1alpha1.ClusterSpec{
		Distribution:  v1alpha1.DistributionVanilla,
		Provider:      v1alpha1.ProviderDocker,
		CNI:           v1alpha1.CNIDefault,
		CSI:           v1alpha1.CSIDefault,
		CDI:           v1alpha1.CDIDefault,
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
	}
}

func newBaseProviderSpec() *v1alpha1.ProviderSpec {
	return &v1alpha1.ProviderSpec{
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

func clone(spec *v1alpha1.ClusterSpec) *v1alpha1.ClusterSpec {
	out := *spec
	out.Vanilla = spec.Vanilla
	out.Talos = spec.Talos
	out.LocalRegistry = spec.LocalRegistry

	return &out
}

func cloneProvider(spec *v1alpha1.ProviderSpec) *v1alpha1.ProviderSpec {
	out := *spec
	out.Hetzner = spec.Hetzner

	return &out
}

func TestEngine_NilSpecs(t *testing.T) {
	t.Parallel()

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

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

			result := engine.ComputeDiff(testCase.oldSpec, testCase.newSpec, nil, nil)
			if result.TotalChanges() != 0 {
				t.Errorf("expected 0 changes for nil spec, got %d", result.TotalChanges())
			}
		})
	}
}

func TestEngine_NoChanges(t *testing.T) {
	t.Parallel()

	spec := newBaseSpec()
	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	result := engine.ComputeDiff(spec, spec, nil, nil)

	if result.TotalChanges() != 0 {
		t.Errorf("identical specs should produce 0 changes, got %d", result.TotalChanges())
	}
}

func TestEngine_DistributionChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Distribution = v1alpha1.DistributionTalos

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasRecreateRequired() {
		t.Fatal("distribution change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.distribution",
		"Vanilla", "Talos", clusterupdate.ChangeCategoryRecreateRequired)
}

func TestEngine_ProviderChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Provider = v1alpha1.ProviderHetzner

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasRecreateRequired() {
		t.Fatal("provider change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.provider",
		"Docker", "Hetzner", clusterupdate.ChangeCategoryRecreateRequired)
}

//nolint:funlen // Table-driven test with multiple component scenarios is clearer as single function
func TestEngine_ComponentChanges(t *testing.T) {
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
			name:     "MetricsServer change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.MetricsServer = testValueEnabled },
			field:    "cluster.metricsServer",
			oldValue: "Disabled",
			newValue: testValueEnabled,
		},
		{
			name:     "LoadBalancer change",
			mutate:   func(s *v1alpha1.ClusterSpec) { s.LoadBalancer = testValueEnabled },
			field:    "cluster.loadBalancer",
			oldValue: "Disabled",
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

			engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
			result := engine.ComputeDiff(old, newer, nil, nil)

			if !result.HasInPlaceChanges() {
				t.Fatal("component change should be in-place")
			}

			if result.HasRecreateRequired() {
				t.Fatal("component change should not require recreate")
			}

			assertSingleChange(t, result.InPlaceChanges, testCase.field,
				testCase.oldValue, testCase.newValue, clusterupdate.ChangeCategoryInPlace)
		})
	}
}

func TestEngine_CSIChange_SkippedForVanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.CSI = v1alpha1.CSIDisabled

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.csi" {
			t.Fatal(
				"CSI changes should be skipped for Vanilla" +
					" (Kind always bundles local-path-provisioner)",
			)
		}
	}
}

func TestEngine_CSIChange_DetectedForTalos(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	newer := clone(old)
	newer.CSI = v1alpha1.CSIEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("CSI change should be detected for Talos")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.csi",
		"Disabled", testValueEnabled, clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_CDIChange_RecreateRequiredForVanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.CDI = v1alpha1.CDIEnabled

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasRecreateRequired() {
		t.Fatal("CDI change should require recreate for Vanilla (Kind)")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.cdi",
		"Disabled", testValueEnabled, clusterupdate.ChangeCategoryRecreateRequired)
}

func TestEngine_CDIChange_RebootRequiredForTalos(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.CDI = v1alpha1.CDIDefault
	newer := clone(old)
	newer.CDI = v1alpha1.CDIDisabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasRebootRequired() {
		t.Fatal("CDI change should require reboot for Talos")
	}

	assertSingleChange(t, result.RebootRequired, "cluster.cdi",
		testValueEnabled, "Disabled", clusterupdate.ChangeCategoryRebootRequired)
}

func TestEngine_CDIChange_SuppressedForK3s(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionK3s
	newer := clone(old)
	newer.CDI = v1alpha1.CDIEnabled

	engine := diff.NewEngine(v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.cdi" {
			t.Fatal("CDI changes should be suppressed for K3s (no runtime wiring)")
		}
	}
}

func TestEngine_LocalRegistryChange_Vanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.LocalRegistry.Registry = testRegistryAlt

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasRecreateRequired() {
		t.Fatal("Vanilla local registry change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.localRegistry.registry",
		"localhost:5050", testRegistryAlt, clusterupdate.ChangeCategoryRecreateRequired)
}

func TestEngine_LocalRegistryChange_Talos(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos

	newer := clone(old)
	newer.LocalRegistry.Registry = testRegistryAlt

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("Talos local registry change should be in-place")
	}

	if result.HasRecreateRequired() {
		t.Fatal("Talos local registry change should not require recreate")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.localRegistry.registry",
		"localhost:5050", testRegistryAlt, clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_LocalRegistryChange_K3s(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionK3s

	newer := clone(old)
	newer.LocalRegistry.Registry = testRegistryAlt

	engine := diff.NewEngine(v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("K3s local registry change should be in-place")
	}

	if result.HasRecreateRequired() {
		t.Fatal("K3s local registry change should not require recreate")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.localRegistry.registry",
		"localhost:5050", testRegistryAlt, clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_LocalRegistryChange_OldEmpty_Skipped(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.LocalRegistry.Registry = ""

	newer := clone(old)
	newer.LocalRegistry.Registry = "ghcr.io/org/repo"

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.localRegistry.registry" {
			t.Fatal("should not report local registry diff when old is empty (undetectable)")
		}
	}
}

func TestEngine_VanillaOptionsChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Vanilla.MirrorsDir = "other/mirrors"

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasRecreateRequired() {
		t.Fatal("Vanilla mirrorsDir change should require recreate")
	}

	assertSingleChange(t, result.RecreateRequired, "cluster.vanilla.mirrorsDir",
		"kind/mirrors", "other/mirrors", clusterupdate.ChangeCategoryRecreateRequired)
}

func TestEngine_VanillaOptionsChange_SkippedForNonVanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Vanilla.MirrorsDir = "other/mirrors"

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.vanilla.mirrorsDir" {
			t.Fatal("Vanilla mirrorsDir change should be ignored for non-Vanilla distributions")
		}
	}
}

func TestEngine_TalosOptionsChange(t *testing.T) {
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

			engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
			result := engine.ComputeDiff(old, newer, nil, nil)

			if !result.HasInPlaceChanges() {
				t.Fatal("Talos option change should be in-place")
			}

			assertSingleChange(t, result.InPlaceChanges, testCase.field,
				testCase.oldValue, testCase.newValue, clusterupdate.ChangeCategoryInPlace)
		})
	}
}

func TestEngine_TalosOptionsChange_SkippedForNonTalos(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Talos.ControlPlanes = 5

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.talos.controlPlanes" {
			t.Fatal("Talos options should be ignored for non-Talos distributions")
		}
	}
}

func TestEngine_HetznerOptionsChange_RecreateRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(spec *v1alpha1.ProviderSpec)
		field    string
		oldValue string
		newValue string
	}{
		{
			name:     "control plane server type change",
			mutate:   func(s *v1alpha1.ProviderSpec) { s.Hetzner.ControlPlaneServerType = "cpx21" },
			field:    "provider.hetzner.controlPlaneServerType",
			oldValue: "cx23",
			newValue: "cpx21",
		},
		{
			name:     "location change",
			mutate:   func(s *v1alpha1.ProviderSpec) { s.Hetzner.Location = "nbg1" },
			field:    "provider.hetzner.location",
			oldValue: "fsn1",
			newValue: "nbg1",
		},
		{
			name:     "network name change",
			mutate:   func(s *v1alpha1.ProviderSpec) { s.Hetzner.NetworkName = "new-network" },
			field:    "provider.hetzner.networkName",
			oldValue: "test-network",
			newValue: "new-network",
		},
		{
			name:     "network CIDR change",
			mutate:   func(s *v1alpha1.ProviderSpec) { s.Hetzner.NetworkCIDR = "10.1.0.0/16" },
			field:    "provider.hetzner.networkCidr",
			oldValue: "10.0.0.0/16",
			newValue: "10.1.0.0/16",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			oldProvider := newBaseProviderSpec()
			newProvider := cloneProvider(oldProvider)
			testCase.mutate(newProvider)

			engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer, oldProvider, newProvider)

			if !result.HasRecreateRequired() {
				t.Fatal("Hetzner change should require recreate")
			}

			assertSingleChange(t, result.RecreateRequired, testCase.field,
				testCase.oldValue, testCase.newValue, clusterupdate.ChangeCategoryRecreateRequired)
		})
	}
}

func TestEngine_HetznerOptionsChange_InPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(spec *v1alpha1.ProviderSpec)
		field    string
		oldValue string
		newValue string
	}{
		{
			name:     "worker server type change",
			mutate:   func(s *v1alpha1.ProviderSpec) { s.Hetzner.WorkerServerType = "cpx21" },
			field:    "provider.hetzner.workerServerType",
			oldValue: "cx23",
			newValue: "cpx21",
		},
		{
			name:     "SSH key name change",
			mutate:   func(s *v1alpha1.ProviderSpec) { s.Hetzner.SSHKeyName = "other-key" },
			field:    "provider.hetzner.sshKeyName",
			oldValue: "my-key",
			newValue: "other-key",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			oldProvider := newBaseProviderSpec()
			newProvider := cloneProvider(oldProvider)
			testCase.mutate(newProvider)

			engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer, oldProvider, newProvider)

			if !result.HasInPlaceChanges() {
				t.Fatal("Hetzner change should be in-place")
			}

			if result.HasRecreateRequired() {
				t.Fatal("Hetzner worker/SSH change should not require recreate")
			}

			assertSingleChange(t, result.InPlaceChanges, testCase.field,
				testCase.oldValue, testCase.newValue, clusterupdate.ChangeCategoryInPlace)
		})
	}
}

func TestEngine_HetznerOptionsChange_SkippedForDocker(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	oldProvider := newBaseProviderSpec()
	newProvider := cloneProvider(oldProvider)
	newProvider.Hetzner.Location = "nbg1"

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, oldProvider, newProvider)

	for _, change := range result.AllChanges() {
		if change.Field == "provider.hetzner.location" {
			t.Fatal("Hetzner options should be ignored for Docker provider")
		}
	}
}

func TestEngine_HetznerOptionsChange_EmptyNewValueUsesDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(spec *v1alpha1.ProviderSpec)
		field  string
	}{
		{
			name:   "empty networkCidr treated as default",
			mutate: func(s *v1alpha1.ProviderSpec) { s.Hetzner.NetworkCIDR = "" },
			field:  "provider.hetzner.networkCidr",
		},
		{
			name:   "empty controlPlaneServerType treated as default",
			mutate: func(s *v1alpha1.ProviderSpec) { s.Hetzner.ControlPlaneServerType = "" },
			field:  "provider.hetzner.controlPlaneServerType",
		},
		{
			name:   "empty workerServerType treated as default",
			mutate: func(s *v1alpha1.ProviderSpec) { s.Hetzner.WorkerServerType = "" },
			field:  "provider.hetzner.workerServerType",
		},
		{
			name:   "empty location treated as default",
			mutate: func(s *v1alpha1.ProviderSpec) { s.Hetzner.Location = "" },
			field:  "provider.hetzner.location",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			newer := clone(old)
			oldProvider := newBaseProviderSpec()
			newProvider := cloneProvider(oldProvider)
			testCase.mutate(newProvider)

			engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer, oldProvider, newProvider)

			for _, change := range result.AllChanges() {
				if change.Field == testCase.field {
					t.Fatalf(
						"empty new value for %s should be treated as default (no change), "+
							"but got diff: %q -> %q",
						testCase.field, change.OldValue, change.NewValue,
					)
				}
			}
		})
	}
}

func TestEngine_MultipleChanges(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.CNI = v1alpha1.CNICilium
	newer.CSI = v1alpha1.CSIDisabled
	newer.Vanilla.MirrorsDir = "changed"

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	// CSI change is skipped for Vanilla, so only CNI counts as in-place
	if len(result.InPlaceChanges) != 1 {
		t.Errorf(
			"expected 1 in-place change (CNI only, CSI skipped for Vanilla), got %d",
			len(result.InPlaceChanges),
		)
	}

	if len(result.RecreateRequired) != 1 {
		t.Errorf("expected 1 recreate-required change, got %d", len(result.RecreateRequired))
	}

	if result.TotalChanges() != 2 {
		t.Errorf("expected 2 total changes, got %d", result.TotalChanges())
	}

	if !result.NeedsUserConfirmation() {
		t.Error("should need user confirmation with recreate-required changes")
	}
}

func TestEngine_DefaultVsDisabled_NoFalsePositive_Vanilla(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.CSI = v1alpha1.CSIDisabled
	newer.MetricsServer = v1alpha1.MetricsServerDisabled
	newer.LoadBalancer = v1alpha1.LoadBalancerDisabled

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	// MetricsServer and LoadBalancer: Default and Disabled are semantically
	// equivalent on Vanilla/Docker, so no changes should be detected.
	// CSI: skipped entirely for Vanilla because Kind always bundles
	// local-path-provisioner and the detector can't distinguish bundled
	// from KSail-installed CSI.
	if result.TotalChanges() != 0 {
		t.Errorf(
			"Default vs Disabled on Vanilla/Docker should produce 0 changes, got %d",
			result.TotalChanges(),
		)

		for _, change := range result.AllChanges() {
			t.Logf(
				"  change: %s %q -> %q",
				change.Field, change.OldValue, change.NewValue,
			)
		}
	}
}

func TestEngine_DefaultVsDisabled_DetectedOnK3s(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionK3s

	newer := clone(old)
	newer.Distribution = v1alpha1.DistributionK3s
	newer.CSI = v1alpha1.CSIDisabled
	newer.MetricsServer = v1alpha1.MetricsServerDisabled
	newer.LoadBalancer = v1alpha1.LoadBalancerDisabled

	engine := diff.NewEngine(v1alpha1.DistributionK3s, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	expectedChanges := 3
	if len(result.InPlaceChanges) != expectedChanges {
		t.Errorf(
			"Default vs Disabled should produce %d in-place changes on K3s, got %d",
			expectedChanges,
			len(result.InPlaceChanges),
		)
	}
}

func TestEngine_VCluster_LoadBalancerIgnored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		oldLB v1alpha1.LoadBalancer
		newLB v1alpha1.LoadBalancer
	}{
		{
			name:  "Default vs Disabled produces no diff",
			oldLB: v1alpha1.LoadBalancerDefault,
			newLB: v1alpha1.LoadBalancerDisabled,
		},
		{
			name:  "Default vs Enabled produces no diff",
			oldLB: v1alpha1.LoadBalancerDefault,
			newLB: v1alpha1.LoadBalancerEnabled,
		},
		{
			name:  "Enabled vs Disabled produces no diff",
			oldLB: v1alpha1.LoadBalancerEnabled,
			newLB: v1alpha1.LoadBalancerDisabled,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			old.Distribution = v1alpha1.DistributionVCluster
			old.LoadBalancer = testCase.oldLB

			newer := clone(old)
			newer.LoadBalancer = testCase.newLB

			engine := diff.NewEngine(v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker)
			result := engine.ComputeDiff(old, newer, nil, nil)

			for _, change := range result.AllChanges() {
				if change.Field == "cluster.loadBalancer" {
					t.Errorf(
						"VCluster should not report LoadBalancer diff (%s -> %s)",
						testCase.oldLB, testCase.newLB,
					)
				}
			}
		})
	}
}

func assertSingleChange(
	t *testing.T,
	changes []clusterupdate.Change,
	expectedField, expectedOld, expectedNew string,
	expectedCategory clusterupdate.ChangeCategory,
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

func TestEngine_CheckWorkloadTag(t *testing.T) {
	t.Parallel()

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	tests := []struct {
		name         string
		oldTag       string
		newTag       string
		gitOpsEngine v1alpha1.GitOpsEngine
		wantChanges  int
	}{
		{"no gitops engine", "dev", "latest", v1alpha1.GitOpsEngineNone, 0},
		{"empty gitops engine string", "dev", "latest", "", 0},
		{"same tag no change", "latest", "latest", v1alpha1.GitOpsEngineFlux, 0},
		{"flux tag drift", "dev", "latest", v1alpha1.GitOpsEngineFlux, 1},
		{"argocd tag drift", "dev", "v1.0.0", v1alpha1.GitOpsEngineArgoCD, 1},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := &clusterupdate.UpdateResult{}
			engine.CheckWorkloadTag(testCase.oldTag, testCase.newTag, testCase.gitOpsEngine, result)

			if got := result.TotalChanges(); got != testCase.wantChanges {
				t.Errorf("want %d changes, got %d", testCase.wantChanges, got)
			}

			if testCase.wantChanges > 0 {
				assertWorkloadTagChange(t, result, testCase.oldTag, testCase.newTag)
			}
		})
	}
}

func assertWorkloadTagChange(
	t *testing.T,
	result *clusterupdate.UpdateResult,
	oldTag, newTag string,
) {
	t.Helper()

	changes := result.InPlaceChanges
	if len(changes) != 1 {
		t.Fatalf("expected 1 in-place change, got %d", len(changes))
	}

	change := changes[0]
	if change.Field != "cluster.workload.tag" {
		t.Errorf("expected field cluster.workload.tag, got %s", change.Field)
	}

	if change.OldValue != oldTag {
		t.Errorf("expected old value %q, got %q", oldTag, change.OldValue)
	}

	if change.NewValue != newTag {
		t.Errorf("expected new value %q, got %q", newTag, change.NewValue)
	}
}
