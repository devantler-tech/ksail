package diff_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/require"
)

const (
	testValueEnabled       = "Enabled"
	testRegistryAlt        = "localhost:6060"
	testFieldControlPlanes = "cluster.controlPlanes"
	testFieldWorkers       = "cluster.workers"
	testTalosVersionOld    = "v1.11.2"
	testServerTypeNew      = "cpx41"

	testFieldKubernetesVersion = "cluster.kubernetesVersion"
	testK8sVersionOld          = "1.32.0"
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
		ControlPlanes: 1,
		Workers:       0,
		Talos: v1alpha1.OptionsTalos{
			ISO: v1alpha1.DefaultTalosISO,
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

	if spec.Autoscaler.Node.Pools != nil {
		out.Autoscaler.Node.Pools = make([]v1alpha1.NodePool, len(spec.Autoscaler.Node.Pools))
		copy(out.Autoscaler.Node.Pools, spec.Autoscaler.Node.Pools)
	}

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

func TestEngine_LocalRegistryChange_RedactsPassword(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.LocalRegistry.Registry = "GITHUB_ACTOR:ghp_oldsecret@ghcr.io/org"

	newer := clone(old)
	newer.LocalRegistry.Registry = "GITHUB_ACTOR:ghp_newsecret@ghcr.io/neworg"

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	// Vanilla registry changes are recreate-required; the PAT must be masked while
	// the username, host, and path stay visible.
	assertSingleChange(t, result.RecreateRequired, "cluster.localRegistry.registry",
		"GITHUB_ACTOR:****@ghcr.io/org", "GITHUB_ACTOR:****@ghcr.io/neworg",
		clusterupdate.ChangeCategoryRecreateRequired)

	// Defense-in-depth: the resolved PAT must not appear in any emitted change value.
	for _, change := range result.AllChanges() {
		if strings.Contains(change.OldValue, "ghp_oldsecret") ||
			strings.Contains(change.NewValue, "ghp_newsecret") {
			t.Errorf("registry diff leaked PAT: old=%q new=%q", change.OldValue, change.NewValue)
		}
	}
}

// TestEngine_LocalRegistryChange_PasswordOnly_NotSurfaced verifies that a
// password-only rotation is NOT reported as a diff. Because the persisted
// baseline no longer stores the password (state.SaveClusterSpec masks it so a
// GHCR PAT is never written to disk), checkLocalRegistryChange compares the
// redacted forms — so a change confined to the password yields two identical
// redacted values and is intentionally not surfaced. This supersedes the
// earlier raw-comparison behaviour, which could only detect such a rotation by
// keeping the cleartext secret in the baseline.
func TestEngine_LocalRegistryChange_PasswordOnly_NotSurfaced(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.LocalRegistry.Registry = "user:OLDPAT@ghcr.io/org"

	newer := clone(old)
	newer.LocalRegistry.Registry = "user:NEWPAT@ghcr.io/org"

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.localRegistry.registry" {
			t.Fatalf("password-only change must not be surfaced; got %+v", change)
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

// talosOptionsChangeCases drives TestEngine_TalosOptionsChange. Kept at package
// scope so the test function stays within the funlen limit.
//
//nolint:gochecknoglobals // table-driven test cases.
var talosOptionsChangeCases = []struct {
	name     string
	mutate   func(spec *v1alpha1.ClusterSpec)
	field    string
	oldValue string
	newValue string
}{
	{
		name:     "version pin change",
		mutate:   func(s *v1alpha1.ClusterSpec) { s.Talos.Version = "v1.12.0" },
		field:    "cluster.talos.version",
		oldValue: "",
		newValue: "v1.12.0",
	},
	{
		name:     "kubernetes version pin change",
		mutate:   func(s *v1alpha1.ClusterSpec) { s.KubernetesVersion = "1.34.0" },
		field:    testFieldKubernetesVersion,
		oldValue: "",
		newValue: "1.34.0",
	},
	{
		name:     "control plane count change",
		mutate:   func(s *v1alpha1.ClusterSpec) { s.ControlPlanes = 3 },
		field:    testFieldControlPlanes,
		oldValue: "1",
		newValue: "3",
	},
	{
		name:     "worker count change",
		mutate:   func(s *v1alpha1.ClusterSpec) { s.Workers = 2 },
		field:    testFieldWorkers,
		oldValue: "0",
		newValue: "2",
	},
	{
		name:     "ISO change",
		mutate:   func(s *v1alpha1.ClusterSpec) { s.Talos.ISO = v1alpha1.DefaultTalosISO - 1 },
		field:    "cluster.talos.iso",
		oldValue: strconv.FormatInt(v1alpha1.DefaultTalosISO, 10),
		newValue: strconv.FormatInt(v1alpha1.DefaultTalosISO-1, 10),
	},
}

func TestEngine_TalosOptionsChange(t *testing.T) {
	t.Parallel()

	for _, testCase := range talosOptionsChangeCases {
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
	newer.ControlPlanes = 5

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if change.Field == testFieldControlPlanes {
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

func TestEngine_HetznerServerType_RollingRecreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		controlPlanes int32
		workers       int32
		mutate        func(spec *v1alpha1.ProviderSpec)
		field         string
	}{
		{
			name:          "control plane server type change with quorum redundancy",
			controlPlanes: 3,
			workers:       0,
			mutate:        func(s *v1alpha1.ProviderSpec) { s.Hetzner.ControlPlaneServerType = testServerTypeNew },
			field:         "provider.hetzner.controlPlaneServerType",
		},
		{
			name:          "worker server type change with existing workers",
			controlPlanes: 1,
			workers:       2,
			mutate:        func(s *v1alpha1.ProviderSpec) { s.Hetzner.WorkerServerType = testServerTypeNew },
			field:         "provider.hetzner.workerServerType",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			old.ControlPlanes = testCase.controlPlanes
			old.Workers = testCase.workers
			newer := clone(old)
			oldProvider := newBaseProviderSpec()
			newProvider := cloneProvider(oldProvider)
			testCase.mutate(newProvider)

			engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer, oldProvider, newProvider)

			if !result.HasRollingRecreate() {
				t.Fatal("server type change should require rolling recreate")
			}

			if result.HasRecreateRequired() {
				t.Fatal("rolling-capable server type change should not require full recreate")
			}

			assertSingleChange(t, result.RollingRecreate, testCase.field,
				"cx23", testServerTypeNew, clusterupdate.ChangeCategoryRollingRecreate)
		})
	}
}

func TestEngine_HetznerControlPlaneServerType_RecreateBelowQuorum(t *testing.T) {
	t.Parallel()

	// With fewer than MinControlPlanesForRollingReplace control planes, a CP
	// server-type change cannot roll without losing etcd quorum.
	for _, controlPlanes := range []int32{1, 2} {
		t.Run(strconv.Itoa(int(controlPlanes))+" control planes", func(t *testing.T) {
			t.Parallel()

			old := newBaseSpec()
			old.ControlPlanes = controlPlanes
			newer := clone(old)
			oldProvider := newBaseProviderSpec()
			newProvider := cloneProvider(oldProvider)
			newProvider.Hetzner.ControlPlaneServerType = testServerTypeNew

			engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
			result := engine.ComputeDiff(old, newer, oldProvider, newProvider)

			if result.HasRollingRecreate() {
				t.Fatal("control-plane change below quorum should not roll")
			}

			assertSingleChange(
				t,
				result.RecreateRequired,
				"provider.hetzner.controlPlaneServerType",
				"cx23",
				testServerTypeNew,
				clusterupdate.ChangeCategoryRecreateRequired,
			)
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

// TestEngine_ComponentDefaults_NoFalsePositive reproduces the scenario where a
// freshly initialised ksail.yaml leaves cni/certManager/policyEngine/gitOpsEngine
// unset (empty). The detected baseline carries the applied defaults
// (CNIDefault/Disabled/None/None), so without default normalisation these unset
// fields would produce phantom in-place drift on an otherwise-unchanged cluster.
func TestEngine_ComponentDefaults_NoFalsePositive(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()

	// Simulate an unpinned config: the user never set these component fields.
	newer := clone(old)
	newer.CNI = ""
	newer.CertManager = ""
	newer.PolicyEngine = ""
	newer.GitOpsEngine = ""

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if result.TotalChanges() != 0 {
		t.Errorf(
			"unset component fields should normalise to their defaults (0 changes), got %d",
			result.TotalChanges(),
		)

		for _, change := range result.AllChanges() {
			t.Logf("  change: %s %q -> %q", change.Field, change.OldValue, change.NewValue)
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

const (
	distVerOld = "2.x"
	distVerNew = "2.8.x"
)

func TestEngine_CheckFluxDistributionVersion(t *testing.T) {
	t.Parallel()

	engine := diff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)

	tests := []struct {
		name         string
		oldVersion   string
		newVersion   string
		gitOpsEngine v1alpha1.GitOpsEngine
		wantChanges  int
	}{
		{"no gitops engine", distVerOld, distVerNew, v1alpha1.GitOpsEngineNone, 0},
		{"argocd not applicable", distVerOld, distVerNew, v1alpha1.GitOpsEngineArgoCD, 0},
		{"same version no change", distVerNew, distVerNew, v1alpha1.GitOpsEngineFlux, 0},
		{"flux version drift", distVerOld, distVerNew, v1alpha1.GitOpsEngineFlux, 1},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := &clusterupdate.UpdateResult{}
			engine.CheckFluxDistributionVersion(
				testCase.oldVersion, testCase.newVersion, testCase.gitOpsEngine, result,
			)

			if got := result.TotalChanges(); got != testCase.wantChanges {
				t.Errorf("want %d changes, got %d", testCase.wantChanges, got)
			}

			if testCase.wantChanges > 0 {
				assertFluxDistributionVersionChange(
					t, result, testCase.oldVersion, testCase.newVersion,
				)
			}
		})
	}
}

func assertFluxDistributionVersionChange(
	t *testing.T,
	result *clusterupdate.UpdateResult,
	oldVersion, newVersion string,
) {
	t.Helper()

	changes := result.InPlaceChanges
	if len(changes) != 1 {
		t.Fatalf("expected 1 in-place change, got %d", len(changes))
	}

	change := changes[0]
	if change.Field != "cluster.workload.flux.distributionVersion" {
		t.Errorf("expected field cluster.workload.flux.distributionVersion, got %s", change.Field)
	}

	if change.OldValue != oldVersion {
		t.Errorf("expected old value %q, got %q", oldVersion, change.OldValue)
	}

	if change.NewValue != newVersion {
		t.Errorf("expected new value %q, got %q", newVersion, change.NewValue)
	}
}

func TestEngine_TalosBaselineNodeCountDetected_WhenAutoscalingEnabled(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.ControlPlanes = 5
	newer.Workers = 3
	newer.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	foundCP, foundWorkers := false, false

	for _, change := range result.AllChanges() {
		if change.Field == "cluster.controlPlanes" {
			foundCP = true
		}

		if change.Field == "cluster.workers" {
			foundWorkers = true
		}
	}

	if !foundCP {
		t.Error(
			"baseline cluster.controlPlanes diff should be detected when autoscaling is enabled",
		)
	}

	if !foundWorkers {
		t.Error(
			"baseline cluster.workers diff should be detected when autoscaling is enabled",
		)
	}
}

func TestEngine_TalosISOStillDetected_WhenAutoscalingEnabled(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Talos.ISO = v1alpha1.DefaultTalosISO + 1
	newer.NodeAutoscaling = v1alpha1.NodeAutoscalingEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("ISO change should still be detected when autoscaling is enabled")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.talos.iso",
		strconv.FormatInt(v1alpha1.DefaultTalosISO, 10),
		strconv.FormatInt(v1alpha1.DefaultTalosISO+1, 10),
		clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNodeEnabledChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler node enabled change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.enabled",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNodeCapacityBuffersChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler.Node.CapacityBuffers = true

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler node capacity buffers change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.capacityBuffers",
		"false", "true", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNodeIgnoreDaemonsetsUtilizationChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler.Node.IgnoreDaemonsetsUtilization = true

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler node ignoreDaemonsetsUtilization change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges,
		"cluster.autoscaler.node.ignoreDaemonsetsUtilization",
		"false", "true", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNodeSkipNodesWithLocalStorageChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	disabled := false
	// The unset (nil) old value inherits the upstream default true, so setting it
	// explicitly false must surface as a true→false in-place change.
	newer.Autoscaler.Node.SkipNodesWithLocalStorage = &disabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler node skipNodesWithLocalStorage change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges,
		"cluster.autoscaler.node.skipNodesWithLocalStorage",
		"true", "false", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNodeSkipNodesWithSystemPodsChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	disabled := false
	newer.Autoscaler.Node.SkipNodesWithSystemPods = &disabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler node skipNodesWithSystemPods change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges,
		"cluster.autoscaler.node.skipNodesWithSystemPods",
		"true", "false", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNodeSkipNodesUnsetNoChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	// old nil (unset → upstream default true) vs newer explicit true → both
	// effectively true → no change.
	enabled := true
	newer.Autoscaler.Node.SkipNodesWithLocalStorage = &enabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, change := range result.InPlaceChanges {
		if change.Field == "cluster.autoscaler.node.skipNodesWithLocalStorage" {
			t.Fatalf("nil→true skipNodesWithLocalStorage should not change, got %+v", change)
		}
	}
}

func TestEngine_AutoscalerExpanderChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Autoscaler.Node.Expander = v1alpha1.AutoscalerExpanderList{
		v1alpha1.AutoscalerExpanderLeastWaste,
	}
	newer := clone(old)
	newer.Autoscaler.Node.Expander = v1alpha1.AutoscalerExpanderList{
		v1alpha1.AutoscalerExpanderPrice,
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler expander change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.expander",
		"LeastWaste", "Price", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerExpanderListChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Autoscaler.Node.Expander = v1alpha1.AutoscalerExpanderList{
		v1alpha1.AutoscalerExpanderLeastWaste,
	}
	newer := clone(old)
	newer.Autoscaler.Node.Expander = v1alpha1.AutoscalerExpanderList{
		v1alpha1.AutoscalerExpanderLeastNodes,
		v1alpha1.AutoscalerExpanderLeastWaste,
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("autoscaler expander list change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.expander",
		"LeastWaste", "LeastNodes,LeastWaste", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerPoolAdded(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("pool addition should produce an in-place change")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1]",
		"", "Added", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerPoolRemoved(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
	}
	newer := clone(old)
	newer.Autoscaler.Node.Pools = nil

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("pool removal should produce an in-place change")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1]",
		"Removed", "", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerPoolModified(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
	}
	newer := clone(old)
	newer.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 2, Max: 10},
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("pool modification should produce in-place changes")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1].min",
		"1", "2", clusterupdate.ChangeCategoryInPlace)
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1].max",
		"5", "10", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerPoolLabelsAndTaintsModified(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
	}
	newer := clone(old)
	newer.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{
			Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5,
			Labels: map[string]string{"workload": "gpu"},
			Taints: []v1alpha1.NodePoolTaint{
				{Key: "dedicated", Value: "gpu", Effect: v1alpha1.TaintEffectNoSchedule},
			},
		},
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("pool labels/taints modification should produce in-place changes")
	}

	assertSingleChange(
		t,
		result.InPlaceChanges,
		"cluster.autoscaler.node.pools[workers-fsn1].labels",
		"",
		"workload=gpu",
		clusterupdate.ChangeCategoryInPlace,
	)
	assertSingleChange(
		t,
		result.InPlaceChanges,
		"cluster.autoscaler.node.pools[workers-fsn1].taints",
		"",
		"dedicated=gpu:NoSchedule",
		clusterupdate.ChangeCategoryInPlace,
	)
}

func TestEngine_AutoscalerPodHorizontalChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler.Pod.Horizontal = v1alpha1.PodAutoscalerHorizontalEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("HPA change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.pod.horizontal",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerPodVerticalChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler.Pod.Vertical = v1alpha1.PodAutoscalerVerticalEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	if !result.HasInPlaceChanges() {
		t.Fatal("VPA change should be in-place")
	}

	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.pod.vertical",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_AutoscalerNoChange(t *testing.T) {
	t.Parallel()

	spec := newBaseSpec()
	spec.Autoscaler = v1alpha1.AutoscalerConfig{
		Node: v1alpha1.NodeAutoscalerConfig{
			Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
			MaxNodesTotal: 20,
			Expander: v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
			ScaleDownUnneededTime: "10m",
			Pools: []v1alpha1.NodePool{
				{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
			},
		},
		Pod: v1alpha1.PodAutoscalerConfig{
			Horizontal: v1alpha1.PodAutoscalerHorizontalEnabled,
			Vertical:   v1alpha1.PodAutoscalerVerticalDisabled,
		},
	}
	newer := clone(spec)

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(spec, newer, nil, nil)

	for _, change := range result.AllChanges() {
		if strings.HasPrefix(change.Field, "cluster.autoscaler") {
			t.Errorf(
				"identical autoscaler config should produce no changes, got: %s %q -> %q",
				change.Field, change.OldValue, change.NewValue,
			)
		}
	}
}

func TestEngine_TalosNodeCountDetected_WhenAutoscalerNodeEnabled(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.ControlPlanes = 5
	newer.Workers = 3
	newer.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	foundCP, foundWorkers := false, false

	for _, change := range result.AllChanges() {
		if change.Field == testFieldControlPlanes {
			foundCP = true
		}

		if change.Field == testFieldWorkers {
			foundWorkers = true
		}
	}

	if !foundCP {
		t.Error(
			"baseline cluster.controlPlanes diff should be detected even when autoscaler.node.enabled is set",
		)
	}

	if !foundWorkers {
		t.Error(
			"baseline cluster.workers diff should be detected even when autoscaler.node.enabled is set",
		)
	}
}

// TestEngine_AutoscalerFullConfigChange verifies that a single ComputeDiff call detects all
// expected changes when transitioning from no autoscaler config to a fully-configured autoscaler.
// This cross-cutting test exercises the entire autoscaler diff path in one operation.
//
// Default-value substitution: appendChange replaces empty-string old/new values with defaultVal
// before comparing. Fields with a non-empty defaultVal will therefore show the default as OldValue
// when the old spec has a zero-value field (e.g. false for "enabled").
// Setting Expander to AutoscalerExpanderPrice (not the default LeastWaste) ensures that the
// expander change is actually detectable.
func TestEngine_AutoscalerFullConfigChange(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Autoscaler = v1alpha1.AutoscalerConfig{
		Node: v1alpha1.NodeAutoscalerConfig{
			Enabled:       v1alpha1.NodeAutoscalerEnabledEnabled,
			MaxNodesTotal: 20,
			Expander: v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderPrice,
			},
			ScaleDownUnneededTime:         "10m",
			ScaleDownUtilizationThreshold: "0.7",
			Pools: []v1alpha1.NodePool{
				{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
			},
		},
		Pod: v1alpha1.PodAutoscalerConfig{
			Horizontal: v1alpha1.PodAutoscalerHorizontalEnabled,
			Vertical:   v1alpha1.PodAutoscalerVerticalEnabled,
		},
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	require.True(
		t, result.HasInPlaceChanges(),
		"full autoscaler config change should produce in-place changes",
	)

	// "Disabled" is the defaultVal substituted when old spec has the zero-value toggle.
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.enabled",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.maxNodesTotal",
		"0", "20", clusterupdate.ChangeCategoryInPlace)
	// "LeastWaste" is the defaultVal; using Price ensures old("LeastWaste") != new("Price").
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.expander",
		"LeastWaste", "Price", clusterupdate.ChangeCategoryInPlace)
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.scaleDownUnneededTime",
		"", "10m", clusterupdate.ChangeCategoryInPlace)
	assertSingleChange(
		t,
		result.InPlaceChanges,
		"cluster.autoscaler.node.scaleDownUtilizationThreshold",
		"",
		"0.7",
		clusterupdate.ChangeCategoryInPlace,
	)
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1]",
		"", "Added", clusterupdate.ChangeCategoryInPlace)
	// "Disabled" is the defaultVal substituted when old spec has zero-value PodAutoscalerHorizontal.
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.pod.horizontal",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
	// "Disabled" is the defaultVal substituted when old spec has zero-value PodAutoscalerVertical.
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.pod.vertical",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
}

// TestEngine_WorkersAndAutoscalerPools_BothDetected_WhenAutoscalerDisabled verifies that
// a workers node-count change and an autoscaler pool change are both detected independently
// in a single ComputeDiff call when the node autoscaler is disabled (no suppression active).
func TestEngine_WorkersAndAutoscalerPools_BothDetected_WhenAutoscalerDisabled(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	newer := clone(old)
	newer.Workers = 2
	newer.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
	}
	// Autoscaler node enabled is NOT set → node count diffs must NOT be suppressed.

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	require.True(
		t, result.HasInPlaceChanges(),
		"workers and pool change should produce in-place changes",
	)

	assertSingleChange(t, result.InPlaceChanges, testFieldWorkers,
		"0", "2", clusterupdate.ChangeCategoryInPlace)
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1]",
		"", "Added", clusterupdate.ChangeCategoryInPlace)
}

// TestEngine_AutoscalerToggle_NodeCountAlwaysDetected verifies that enabling the
// node autoscaler in the same update that also changes workers and adds a pool results in:
//   - the autoscaler.node.enabled change being detected,
//   - the pool change being detected,
//   - the workers node-count change being detected (node-count diffs are always emitted,
//     regardless of autoscaler state — autoscaler manages node pools, not baseline counts).
func TestEngine_AutoscalerToggle_NodeCountAlwaysDetected(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Workers = 1
	newer := clone(old)
	newer.Workers = 5
	newer.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled
	newer.Autoscaler.Node.Pools = []v1alpha1.NodePool{
		{Name: "workers-fsn1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
	}

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	require.True(
		t, result.HasInPlaceChanges(),
		"autoscaler toggle, workers change, and pool addition should produce in-place changes",
	)

	// "Disabled" is the defaultVal substituted for the zero-value toggle.
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.enabled",
		"Disabled", "Enabled", clusterupdate.ChangeCategoryInPlace)
	assertSingleChange(t, result.InPlaceChanges, "cluster.autoscaler.node.pools[workers-fsn1]",
		"", "Added", clusterupdate.ChangeCategoryInPlace)

	// Node-count diffs are always emitted for Talos regardless of autoscaler state.
	assertSingleChange(t, result.InPlaceChanges, testFieldWorkers,
		"1", "5", clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_TalosISO_SuppressedWhenOldUnknown(t *testing.T) {
	t.Parallel()

	// Simulate GetCurrentConfig returning 0 (unset) for ISO — the live cluster
	// can't report what ISO it booted from, and no persisted state is available
	// (e.g. a stateless CI runner).
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.ISO = 0

	// Pin a non-default ISO, as a real cluster does. This is the case that
	// previously produced a perpetual false-positive diff: the unknown baseline
	// was filled with DefaultTalosISO and compared against the pinned value.
	newer := clone(old)
	newer.Talos.ISO = v1alpha1.DefaultTalosISO + 1

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	// ISO must NOT appear as a change: with no baseline to compare against, the
	// rule skips the field entirely rather than diffing against the default.
	for _, c := range result.AllChanges() {
		if c.Field == "cluster.talos.iso" {
			t.Fatalf("expected no ISO diff when old value is 0 (unknown), got %+v", c)
		}
	}
}

func TestEngine_TalosISO_DetectedWhenBothNonZero(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.ISO = v1alpha1.DefaultTalosISO

	newer := clone(old)
	newer.Talos.ISO = v1alpha1.DefaultTalosISO + 1

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	assertSingleChange(t, result.InPlaceChanges, "cluster.talos.iso",
		strconv.FormatInt(v1alpha1.DefaultTalosISO, 10),
		strconv.FormatInt(v1alpha1.DefaultTalosISO+1, 10),
		clusterupdate.ChangeCategoryInPlace)
}

func TestEngine_TalosISO_NoChangeWhenDesiredUnsetMatchesDefaultBaseline(t *testing.T) {
	t.Parallel()

	// Persisted state supplies a known baseline equal to the default ISO while the
	// config leaves talos.iso unset (0). defaultVal must normalise the desired side
	// to the default so an unpinned config does not produce a false-positive diff
	// against the baseline. skipWhenOldEmpty does not apply here — the baseline is
	// known and non-zero.
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.ISO = v1alpha1.DefaultTalosISO

	newer := clone(old)
	newer.Talos.ISO = 0

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, c := range result.AllChanges() {
		if c.Field == "cluster.talos.iso" {
			t.Fatalf(
				"expected no ISO diff when desired is unset and baseline equals default, got %+v",
				c,
			)
		}
	}
}

func TestEngine_TalosVersion_NoChangeWhenBothSet(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.Version = testTalosVersionOld

	newer := clone(old)
	newer.Talos.Version = testTalosVersionOld

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, c := range result.AllChanges() {
		if c.Field == "cluster.talos.version" {
			t.Fatalf("expected no talos.version diff when values match, got %+v", c)
		}
	}
}

func TestEngine_TalosVersion_NoChangeWhenNewEmpty(t *testing.T) {
	t.Parallel()

	// Old spec has detected version (live cluster); new spec has no version pinned.
	// This is the regression case: introspectTalosVersion detects a version but
	// the user hasn't pinned one in ksail.yaml — no diff should be emitted.
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.Version = "v1.13.0"

	newer := clone(old)
	newer.Talos.Version = ""

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, c := range result.AllChanges() {
		if c.Field == "cluster.talos.version" {
			t.Fatalf("expected no talos.version diff when new version is empty, got %+v", c)
		}
	}
}

func TestEngine_TalosVersion_ChangeWhenNewDiffers(t *testing.T) {
	t.Parallel()

	// Both specs pin a version but they differ — a diff should be emitted.
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.Version = testTalosVersionOld

	newer := clone(old)
	newer.Talos.Version = "v1.13.0"

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	assertSingleChange(
		t, result.InPlaceChanges,
		"cluster.talos.version", testTalosVersionOld, "v1.13.0",
		clusterupdate.ChangeCategoryInPlace,
	)
}

func TestEngine_TalosKubernetesVersion_NoChangeWhenNewEmpty(t *testing.T) {
	t.Parallel()

	// The cluster reports a running Kubernetes version (introspected baseline) but
	// the user has not pinned one — the provisioner tracks the running version, so
	// no spec-level change should be emitted.
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.KubernetesVersion = testK8sVersionOld

	newer := clone(old)
	newer.KubernetesVersion = ""

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, c := range result.AllChanges() {
		if c.Field == testFieldKubernetesVersion {
			t.Fatalf("expected no kubernetesVersion diff when new version is empty, got %+v", c)
		}
	}
}

func TestEngine_TalosKubernetesVersion_NoChangeWhenMatches(t *testing.T) {
	t.Parallel()

	// User pins the same version the cluster runs; the "v" prefix must not matter.
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.KubernetesVersion = testK8sVersionOld

	newer := clone(old)
	newer.KubernetesVersion = "v" + testK8sVersionOld

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	for _, c := range result.AllChanges() {
		if c.Field == testFieldKubernetesVersion {
			t.Fatalf("expected no kubernetesVersion diff when values match, got %+v", c)
		}
	}
}

func TestEngine_TalosKubernetesVersion_ChangeWhenNewDiffers(t *testing.T) {
	t.Parallel()

	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.KubernetesVersion = testK8sVersionOld

	newer := clone(old)
	newer.KubernetesVersion = "v1.34.0"

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	assertSingleChange(
		t, result.InPlaceChanges,
		testFieldKubernetesVersion, testK8sVersionOld, "1.34.0",
		clusterupdate.ChangeCategoryInPlace,
	)
}

func TestEngine_TalosVersion_ChangeWhenOldEmptyNewSet(t *testing.T) {
	t.Parallel()

	// Old spec has no detected version; new spec pins one.
	// Detection failed (e.g., no API access) but user wants a specific version.
	old := newBaseSpec()
	old.Distribution = v1alpha1.DistributionTalos
	old.Talos.Version = ""

	newer := clone(old)
	newer.Talos.Version = testTalosVersionOld

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
	result := engine.ComputeDiff(old, newer, nil, nil)

	assertSingleChange(
		t, result.InPlaceChanges,
		"cluster.talos.version", "", testTalosVersionOld,
		clusterupdate.ChangeCategoryInPlace,
	)
}

// findChange returns the first change with the given field, or nil.
func findChange(changes []clusterupdate.Change, field string) *clusterupdate.Change {
	for i := range changes {
		if changes[i].Field == field {
			return &changes[i]
		}
	}

	return nil
}

func TestEngine_UnknownBaseline_SurfacesUnknownInsteadOfInPlace(t *testing.T) {
	t.Parallel()

	// Baseline could not be read from the cluster: every detector-derived
	// component field is the Unknown sentinel.
	old := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	clusterupdate.MarkComponentsUnknown(old)

	// Desired config requests several active components, and leaves the load
	// balancer and CSI at their defaults.
	newer := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	newer.CNI = v1alpha1.CNICilium
	newer.MetricsServer = v1alpha1.MetricsServerEnabled
	newer.CertManager = v1alpha1.CertManagerEnabled
	newer.PolicyEngine = v1alpha1.PolicyEngineKyverno
	newer.GitOpsEngine = v1alpha1.GitOpsEngineFlux

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	// No fabricated in-place / reboot / recreate changes from the unknown baseline.
	require.Zero(t, result.TotalChanges(),
		"unknown baseline must not produce applicable changes")
	require.True(t, result.HasUnknownBaseline())

	wantFields := []string{
		"cluster.cni",
		"cluster.metricsServer",
		"cluster.certManager",
		"cluster.policyEngine",
		"cluster.gitOpsEngine",
	}
	require.Len(t, result.UnknownBaseline, len(wantFields))

	for _, field := range wantFields {
		change := findChange(result.UnknownBaseline, field)
		require.NotNilf(t, change, "expected unknown-baseline entry for %s", field)
		require.Equal(t, clusterupdate.UnknownBaselineValue, change.OldValue)
		require.Equal(t, clusterupdate.ChangeCategoryUnknown, change.Category)
		require.NotEqual(t, clusterupdate.UnknownBaselineValue, change.NewValue)
	}

	// Components left at their defaults must not be reported, even as Unknown.
	require.Nil(t, findChange(result.UnknownBaseline, "cluster.loadBalancer"))
	require.Nil(t, findChange(result.UnknownBaseline, "cluster.csi"))
}

func TestEngine_UnknownBaseline_NoNoiseWhenDesiredMatchesDefaults(t *testing.T) {
	t.Parallel()

	old := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	clusterupdate.MarkComponentsUnknown(old)

	// Desired config leaves every component at its default.
	newer := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	require.Zero(t, result.TotalChanges())
	require.False(t, result.HasUnknownBaseline(),
		"defaults on both sides must not produce Unknown rows")
}

func TestEngine_UnknownBaseline_SkipsNodeAutoscalerDiff(t *testing.T) {
	t.Parallel()

	old := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	clusterupdate.MarkComponentsUnknown(old)

	newer := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	newer.Autoscaler.Node.Enabled = v1alpha1.NodeAutoscalerEnabledEnabled
	newer.Autoscaler.Node.MaxNodesTotal = 5

	engine := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker)
	result := engine.ComputeDiff(old, newer, nil, nil)

	// The node autoscaler is detector-derived; an unknown baseline must not
	// fabricate an in-place "enable autoscaler" change.
	require.Nil(t, findChange(result.InPlaceChanges, "cluster.autoscaler.node.enabled"))
	require.Nil(t, findChange(result.UnknownBaseline, "cluster.autoscaler.node.enabled"))
}
