package cluster_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasConfirmFlagAnnotation reports whether the named flag on cmd carries the
// ai.toolgen.confirm-flag annotation (the marker the chat assistant uses to
// auto-inject a prompt-skip after permission approval).
func hasConfirmFlagAnnotation(cmd *cobra.Command, flagName string) bool {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil {
		return false
	}

	return slices.Contains(
		flag.Annotations[annotations.AnnotationConfirmFlag],
		annotations.AnnotationValueTrue,
	)
}

// TestUpdateConfirmFlagRepointedToYes verifies item 5.2's re-point: only --yes
// carries the ai.toolgen.confirm-flag annotation, so the chat/MCP assistant
// auto-confirms cluster updates via --yes. The destructive --force-drain and the
// deprecated --force alias must NOT be auto-injected.
func TestUpdateConfirmFlagRepointedToYes(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewUpdateCmd()

	assert.True(t, hasConfirmFlagAnnotation(cmd, "yes"),
		"--yes must carry the confirm-flag annotation so chat/MCP auto-confirms via --yes")
	assert.False(t, hasConfirmFlagAnnotation(cmd, "force-drain"),
		"--force-drain must NOT be a confirm flag — it is destructive")
	assert.False(t, hasConfirmFlagAnnotation(cmd, "force"),
		"the deprecated --force alias must NOT be a confirm flag")
}

// TestUpdateForceFlagSplit verifies item 5.2's flag split: --force survives as a
// hidden deprecated alias, and --force-drain is the new destructive flag.
func TestUpdateForceFlagSplit(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewUpdateCmd()

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag, "--force should survive as a hidden deprecated alias")
	assert.NotEmpty(t, forceFlag.Deprecated, "--force should be marked deprecated")
	assert.True(t, forceFlag.Hidden, "--force should be hidden")

	forceDrainFlag := cmd.Flags().Lookup("force-drain")
	require.NotNil(t, forceDrainFlag, "--force-drain flag should exist")
	assert.False(t, forceDrainFlag.Hidden, "--force-drain should be visible in help")
}

func TestApplyDistributionSpecOverrides(t *testing.T) { //nolint:funlen
	t.Parallel()

	tests := []struct {
		name  string
		input v1alpha1.ClusterSpec
		want  v1alpha1.ClusterSpec
	}{
		{
			name: "KWOK with Flux and Kyverno and LoadBalancer Enabled normalises all three",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				PolicyEngine: v1alpha1.PolicyEngineKyverno,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineNone,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CNI:          v1alpha1.CNIDefault,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "KWOK with ArgoCD keeps GitOpsEngine unchanged",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CNI:          v1alpha1.CNIDefault,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "KWOK with Cilium CNI normalises CNI to Default",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				CNI:          v1alpha1.CNICilium,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				CNI:          v1alpha1.CNIDefault,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "KWOK with Calico normalises CNI to Default",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				CNI:          v1alpha1.CNICalico,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CNI:          v1alpha1.CNIDefault,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "non-KWOK distribution is unchanged",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				PolicyEngine: v1alpha1.PolicyEngineKyverno,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				PolicyEngine: v1alpha1.PolicyEngineKyverno,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
		},
	}

	for _, tt := range tests { //nolint:varnamelen // tt is conventional for table-driven tests
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := tt.input
			cluster.ExportApplyDistributionSpecOverrides(&spec)
			assert.Equal(t, tt.want, spec)
		})
	}
}

// fakeUpdater is a test clusterprovisioner.Updater whose Update returns a preset
// result and error, letting tests drive applyInPlaceChanges deterministically.
type fakeUpdater struct {
	result  *clusterupdate.UpdateResult
	err     error
	diffErr error
}

func (f *fakeUpdater) Update(
	_ context.Context,
	_ string,
	_, _ *v1alpha1.ClusterSpec,
	_ clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	return f.result, f.err
}

func (f *fakeUpdater) DiffConfig(
	_ context.Context,
	_ string,
	_, _ *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	return clusterupdate.NewEmptyUpdateResult(), f.diffErr
}

func (f *fakeUpdater) GetCurrentConfig(
	_ context.Context,
	_ string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	return nil, nil, nil
}

// TestComputeUpdateDiff_PropagatesProvisionerError verifies live provisioner
// detection failures abort the update preview instead of becoming a false
// no-change result.
func TestComputeUpdateDiff_PropagatesProvisionerError(t *testing.T) {
	t.Parallel()

	wantErr := assert.AnError
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	ctx := &localregistry.Context{ClusterCfg: &v1alpha1.Cluster{}}

	_, _, err := cluster.ExportComputeUpdateDiff(
		cmd, ctx, "test", &fakeUpdater{diffErr: wantErr},
	)

	require.ErrorIs(t, err, wantErr)
}

func TestNewUpdateCmd(t *testing.T) { //nolint:cyclop // flag assertion test
	t.Parallel()

	cmd := cluster.NewUpdateCmd()

	// Verify command basics
	if cmd.Use != "update" {
		t.Errorf("expected Use to be 'update', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	if cmd.Long == "" {
		t.Error("expected Long description to be set")
	}

	nameFlag := cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("expected --name flag to exist")
	}

	mirrorRegistryFlag := cmd.Flags().Lookup("mirror-registry")
	if mirrorRegistryFlag == nil {
		t.Error("expected --mirror-registry flag to exist")
	}

	dryRunFlag := cmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Error("expected --dry-run flag to exist")
	}

	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Error("expected --yes flag to exist")
	}

	// Declarative version flags replace the removed opt-in upgrade flags.
	if cmd.Flags().Lookup("kubernetes-version") == nil {
		t.Error("expected --kubernetes-version flag to exist")
	}

	if cmd.Flags().Lookup("distribution-version") == nil {
		t.Error("expected --distribution-version flag to exist")
	}

	// The opt-in upgrade flags were removed in favour of declarative version
	// reconciliation (unset = follow latest, set = pin).
	if cmd.Flags().Lookup("update-kubernetes") != nil {
		t.Error("expected --update-kubernetes flag to be removed")
	}

	if cmd.Flags().Lookup("update-distribution") != nil {
		t.Error("expected --update-distribution flag to be removed")
	}
}

func TestResolveConsent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		forceValue bool
		yesValue   string
		expected   bool
	}{
		{
			name:       "deprecated --force resolves to true",
			forceValue: true,
			yesValue:   "",
			expected:   true,
		},
		{name: "--yes resolves to true", forceValue: false, yesValue: "true", expected: true},
		{
			name:       "--yes=false resolves to false",
			forceValue: false,
			yesValue:   "false",
			expected:   false,
		},
		{name: "both flags resolve to true", forceValue: true, yesValue: "true", expected: true},
		{name: "neither flag resolves to false", forceValue: false, yesValue: "", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := cluster.NewUpdateCmd()

			if testCase.yesValue != "" {
				_ = cmd.Flags().Set("yes", testCase.yesValue)
			}

			result := cluster.ExportResolveConsent(testCase.forceValue, cmd.Flags().Lookup("yes"))
			if result != testCase.expected {
				t.Errorf("expected resolveConsent(%v, yes=%q) = %v, got %v",
					testCase.forceValue, testCase.yesValue, testCase.expected, result)
			}
		})
	}
}

//nolint:paralleltest // subtests override global stdin reader
func TestUpdateConfirmation_UsesConfirmPackage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "user confirms with 'yes'",
			input:    "yes\n",
			expected: true,
		},
		{
			name:     "user confirms with 'YES'",
			input:    "YES\n",
			expected: true,
		},
		{
			name:     "user rejects with 'no'",
			input:    "no\n",
			expected: false,
		},
		{
			name:     "user rejects with empty input",
			input:    "\n",
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: SetStdinReaderForTests overrides a process-wide global
			restore := confirm.SetStdinReaderForTests(strings.NewReader(testCase.input))
			defer restore()

			result := confirm.PromptForConfirmation(nil)

			if result != testCase.expected {
				t.Errorf("expected %v, got %v", testCase.expected, result)
			}
		})
	}
}

//nolint:paralleltest // subtests override global TTY checker
func TestUpdateConfirmation_ShouldSkipPrompt(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		isTTY    bool
		expected bool
	}{
		{name: "force skips prompt", force: true, isTTY: true, expected: true},
		{name: "force skips even non-TTY", force: true, isTTY: false, expected: true},
		{name: "non-TTY skips prompt", force: false, isTTY: false, expected: true},
		{name: "TTY without force shows prompt", force: false, isTTY: true, expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: SetTTYCheckerForTests overrides a process-wide global
			restore := confirm.SetTTYCheckerForTests(func() bool {
				return testCase.isTTY
			})
			defer restore()

			result := confirm.ShouldSkipPrompt(testCase.force)
			if result != testCase.expected {
				t.Errorf("expected ShouldSkipPrompt(%v) = %v, got %v",
					testCase.force, testCase.expected, result)
			}
		})
	}
}

func TestDisplayChangesSummary_NoChanges(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportDisplayChangesSummary(cmd, diff)

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty diff, got %q", buf.String())
	}
}

func TestDisplayChangesSummary_TableFormat(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI can be switched via Helm",
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change requires recreation",
	})

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()

	expected := []struct {
		label string
		text  string
	}{
		{"Component header", "Component"},
		{"Before header", "Before"},
		{"After header", "After"},
		{"Impact header", "Impact"},
		{"in-place icon", "🟢"},
		{"recreate-required icon", "🔴"},
		{"in-place label", "in-place"},
		{"recreate-required label", "recreate-required"},
		{"cluster.cni field", "cluster.cni"},
		{"cluster.distribution field", "cluster.distribution"},
		{"Cilium value", "Cilium"},
		{"Talos value", "Talos"},
		{"change count summary", "Detected 2 configuration changes"},
		{"separator line", "─"},
	}

	for _, entry := range expected {
		if !strings.Contains(output, entry.text) {
			t.Errorf("expected output to contain %s (%q)", entry.label, entry.text)
		}
	}
}

func TestDisplayChangesSummary_SeverityOrder(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
	})
	diff.RebootRequired = append(diff.RebootRequired, clusterupdate.Change{
		Field:    "talos.kernel_args",
		OldValue: "",
		NewValue: "console=ttyS0",
		Category: clusterupdate.ChangeCategoryRebootRequired,
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
	})

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()

	// Recreate-required (🔴) should appear before reboot-required (🟡)
	// and reboot-required should appear before in-place (🟢)
	idxRecreate := strings.Index(output, "🔴")
	idxReboot := strings.Index(output, "🟡")
	idxInPlace := strings.Index(output, "🟢")

	if idxRecreate < 0 || idxReboot < 0 || idxInPlace < 0 {
		t.Fatal("expected all three icons to be present")
	}

	if idxRecreate > idxReboot {
		t.Error("recreate-required should appear before reboot-required")
	}

	if idxReboot > idxInPlace {
		t.Error("reboot-required should appear before in-place")
	}
}

func TestNewUpdateCmd_HasOutputFlag(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewUpdateCmd()

	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag to exist")
	}

	if outputFlag.DefValue != "text" {
		t.Errorf("expected --output default to be 'text', got %q", outputFlag.DefValue)
	}
}

func TestDisplayChangesSummary_JSONOutput(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer

	cmd.SetOut(&buf)

	cmd.Flags().String("output", "json", "")

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI can be switched via Helm",
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change requires recreation",
	})

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()

	if !strings.Contains(output, `"totalChanges"`) {
		t.Errorf("expected JSON output to contain totalChanges key, got: %q", output)
	}

	if !strings.Contains(output, `"inPlaceChanges"`) {
		t.Errorf("expected JSON output to contain inPlaceChanges key, got: %q", output)
	}

	if !strings.Contains(output, `"recreateRequired"`) {
		t.Errorf("expected JSON output to contain recreateRequired key, got: %q", output)
	}

	if !strings.Contains(output, `"requiresConfirmation"`) {
		t.Errorf("expected JSON output to contain requiresConfirmation key, got: %q", output)
	}

	if strings.Contains(output, "Component") {
		t.Error("JSON output should not contain table headers like 'Component'")
	}

	if strings.Contains(output, "🟢") || strings.Contains(output, "🔴") {
		t.Error("JSON output should not contain emoji icons")
	}
}

func TestDisplayChangesSummary_JSONOutput_NoChanges(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer

	cmd.SetOut(&buf)

	cmd.Flags().String("output", "json", "")

	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportDisplayChangesSummary(cmd, diff)

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty diff, got %q", buf.String())
	}
}

func TestDiffToJSON_Structure(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI can be switched",
	})
	diff.RebootRequired = append(diff.RebootRequired, clusterupdate.Change{
		Field:    "talos.kernel",
		OldValue: "",
		NewValue: "console=ttyS0",
		Category: clusterupdate.ChangeCategoryRebootRequired,
		Reason:   "kernel arg change needs reboot",
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change",
	})

	out := cluster.ExportDiffToJSON(diff)

	assertDiffCounts(t, out)
	assertInPlaceChange(t, out.InPlaceChanges[0])
	assertCategories(t, out)
}

func assertDiffCounts(t *testing.T, out cluster.DiffJSONOutput) {
	t.Helper()

	if out.TotalChanges != 3 {
		t.Errorf("expected TotalChanges=3, got %d", out.TotalChanges)
	}

	if len(out.InPlaceChanges) != 1 {
		t.Errorf("expected 1 in-place change, got %d", len(out.InPlaceChanges))
	}

	if len(out.RebootRequired) != 1 {
		t.Errorf("expected 1 reboot-required change, got %d", len(out.RebootRequired))
	}

	if len(out.RecreateRequired) != 1 {
		t.Errorf("expected 1 recreate-required change, got %d", len(out.RecreateRequired))
	}

	if !out.RequiresConfirmation {
		t.Error("expected RequiresConfirmation=true when reboot or recreate changes present")
	}
}

func assertInPlaceChange(t *testing.T, inPlace cluster.ChangeJSON) {
	t.Helper()

	if inPlace.Field != "cluster.cni" {
		t.Errorf("expected field=cluster.cni, got %q", inPlace.Field)
	}

	if inPlace.OldValue != "Default" {
		t.Errorf("expected oldValue=Default, got %q", inPlace.OldValue)
	}

	if inPlace.NewValue != "Cilium" {
		t.Errorf("expected newValue=Cilium, got %q", inPlace.NewValue)
	}

	if inPlace.Category != "in-place" {
		t.Errorf("expected category=in-place, got %q", inPlace.Category)
	}
}

func assertCategories(t *testing.T, out cluster.DiffJSONOutput) {
	t.Helper()

	if out.RecreateRequired[0].Category != "recreate-required" {
		t.Errorf("expected category=recreate-required, got %q", out.RecreateRequired[0].Category)
	}

	if out.RebootRequired[0].Category != "reboot-required" {
		t.Errorf("expected category=reboot-required, got %q", out.RebootRequired[0].Category)
	}
}

func TestDiffToJSON_RequiresConfirmation_OnlyInPlace(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		Category: clusterupdate.ChangeCategoryInPlace,
	})

	out := cluster.ExportDiffToJSON(diff)

	if out.RequiresConfirmation {
		t.Error("expected RequiresConfirmation=false for in-place-only changes")
	}
}

func TestDiffToJSON_IncludesWipeRequired(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.WipeRequired = append(diff.WipeRequired, clusterupdate.Change{
		Field:    "machine.systemDiskEncryption.ephemeral",
		OldValue: "none",
		NewValue: "luks2",
		Category: clusterupdate.ChangeCategoryWipeRequired,
		Reason:   "EPHEMERAL partition encryption change requires partition wipe",
	})

	out := cluster.ExportDiffToJSON(diff)

	assert.Equal(t, 1, out.TotalChanges)
	assert.Len(t, out.WipeRequired, 1)
	assert.Equal(t, "machine.systemDiskEncryption.ephemeral", out.WipeRequired[0].Field)
	assert.Equal(t, "wipe-required", out.WipeRequired[0].Category)
	assert.True(t, out.RequiresConfirmation)
}

func TestFormatDiffTable_EmptyDiff(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{}
	got := cluster.ExportFormatDiffTable(diff, 0)
	assert.Contains(t, got, "Component")
	assert.Contains(t, got, "Before")
	assert.Contains(t, got, "After")
	assert.Contains(t, got, "Impact")
}

func TestFormatDiffTable_InPlaceOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "replicas",
				OldValue: "1",
				NewValue: "3",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "replicas")
	assert.Contains(t, got, "1")
	assert.Contains(t, got, "3")
	assert.Contains(t, got, "🟢")
}

func TestFormatDiffTable_RebootOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "kernel",
				OldValue: "5.4",
				NewValue: "5.15",
				Category: clusterupdate.ChangeCategoryRebootRequired,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "kernel")
	assert.Contains(t, got, "🟡")
}

func TestFormatDiffTable_RecreateOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "distribution",
				OldValue: "k3s",
				NewValue: "talos",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "distribution")
	assert.Contains(t, got, "🔴")
}

func TestFormatDiffTable_MixedSeverities(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "dist",
				OldValue: "a",
				NewValue: "b",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		},
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "kern",
				OldValue: "c",
				NewValue: "d",
				Category: clusterupdate.ChangeCategoryRebootRequired,
			},
		},
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "reps",
				OldValue: "e",
				NewValue: "f",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 3)

	// Verify all fields present
	assert.Contains(t, got, "dist")
	assert.Contains(t, got, "kern")
	assert.Contains(t, got, "reps")

	// Verify all icons present
	assert.Contains(t, got, "🔴")
	assert.Contains(t, got, "🟡")
	assert.Contains(t, got, "🟢")

	// Verify header and separator are present
	assert.Contains(t, got, "─")
}

func TestFormatDiffTable_LongFieldValues(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "spec.cluster.metricsServer.config.scrapeInterval",
				OldValue: "a-very-long-before-value-that-tests-column-width",
				NewValue: "another-very-long-after-value-for-testing",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)
	assert.Contains(t, got, "spec.cluster.metricsServer.config.scrapeInterval")
	assert.Contains(t, got, "a-very-long-before-value-that-tests-column-width")
}

func TestFormatDiffTable_MultipleRows(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "a",
				OldValue: "1",
				NewValue: "2",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
			{
				Field:    "b",
				OldValue: "3",
				NewValue: "4",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
			{
				Field:    "c",
				OldValue: "5",
				NewValue: "6",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 3)
	assert.Contains(t, got, "a")
	assert.Contains(t, got, "b")
	assert.Contains(t, got, "c")
}

func TestFormatDiffTable_UnknownBaselineOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		UnknownBaseline: []clusterupdate.Change{
			{
				Field:    "cluster.cni",
				OldValue: clusterupdate.UnknownBaselineValue,
				NewValue: "Cilium",
				Category: clusterupdate.ChangeCategoryUnknown,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 0)

	assert.Contains(t, got, "cluster.cni")
	assert.Contains(t, got, "Unknown")
	assert.Contains(t, got, "Cilium")
	assert.Contains(t, got, "⚪")
	// The summary line must not claim confident configuration changes.
	assert.Contains(t, got, "could not be read")
	assert.NotContains(t, got, "Detected 0 configuration changes")
}

func TestFormatDiffTable_RealAndUnknownTogether(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "cluster.workers",
				OldValue: "1",
				NewValue: "3",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
		UnknownBaseline: []clusterupdate.Change{
			{
				Field:    "cluster.gitOpsEngine",
				OldValue: clusterupdate.UnknownBaselineValue,
				NewValue: "Flux",
				Category: clusterupdate.ChangeCategoryUnknown,
			},
		},
	}
	got := cluster.ExportFormatDiffTable(diff, 1)

	assert.Contains(t, got, "cluster.workers")
	assert.Contains(t, got, "cluster.gitOpsEngine")
	assert.Contains(t, got, "🟢")
	assert.Contains(t, got, "⚪")
	assert.Contains(t, got, "unknown baseline")
}

func TestDisplayChangesSummary_RecreateBeforeRebootBeforeInPlace(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "in-place-field",
				OldValue: "a",
				NewValue: "b",
				Category: clusterupdate.ChangeCategoryInPlace,
			},
		},
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "reboot-field",
				OldValue: "c",
				NewValue: "d",
				Category: clusterupdate.ChangeCategoryRebootRequired,
			},
		},
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "recreate-field",
				OldValue: "e",
				NewValue: "f",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
			},
		},
	}

	got := cluster.ExportFormatDiffTable(diff, 3)

	// Verify order: recreate comes before reboot, which comes before in-place
	recreateIdx := findSubstringIndex(got, "recreate-field")
	rebootIdx := findSubstringIndex(got, "reboot-field")
	inPlaceIdx := findSubstringIndex(got, "in-place-field")

	assert.Less(t, recreateIdx, rebootIdx, "recreate should appear before reboot")
	assert.Less(t, rebootIdx, inPlaceIdx, "reboot should appear before in-place")
}

func findSubstringIndex(s, substr string) int {
	for i := range len(s) - len(substr) + 1 {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}

const (
	// testPinCurrent is the running version used as the baseline in the
	// normalizePinnedVersion cases; testPinNewer is a pin strictly newer than it.
	testPinCurrent = "v1.8.0"
	testPinNewer   = "v1.9.0"
	// testPinSDKRaw is the unprefixed SDK pin (VCluster's ChartVersion()) used in
	// the phantom-upgrade regression cases; testPinSDKNorm is its normalized form.
	testPinSDKRaw  = "0.34.1"
	testPinSDKNorm = "v0.34.1"
)

// normalizePinnedVersionCase is one ExportNormalizePinnedVersion table case.
type normalizePinnedVersionCase struct {
	name        string
	rawPinned   string
	current     string
	wantVersion string
	wantReason  cluster.ExportPinnedVersionSkipReason
}

// normalizePinnedVersionCases returns the happy-path normalization/downgrade
// cases. Kept separate from the test body so the table doesn't trip funlen.
func normalizePinnedVersionCases() []normalizePinnedVersionCase {
	return []normalizePinnedVersionCase{
		{
			name:        "newer pin proceeds with upgrade",
			rawPinned:   testPinNewer,
			current:     testPinCurrent,
			wantVersion: testPinNewer,
			wantReason:  cluster.ExportPinnedVersionProceed,
		},
		{
			name:        "missing v prefix is normalized",
			rawPinned:   "1.9.0",
			current:     testPinCurrent,
			wantVersion: testPinNewer,
			wantReason:  cluster.ExportPinnedVersionProceed,
		},
		{
			name:        "pin equal to current is a no-op",
			rawPinned:   testPinCurrent,
			current:     testPinCurrent,
			wantVersion: testPinCurrent,
			wantReason:  cluster.ExportPinnedVersionAlreadyAtIt,
		},
		{
			// Regression for the VCluster phantom-upgrade bug: an unprefixed SDK pin
			// ("0.34.1", VCluster's ChartVersion()) must still match an unprefixed
			// current of the same version via parsed-semver equality, not raw strings.
			name:        "unprefixed pin equal to unprefixed current is a no-op",
			rawPinned:   testPinSDKRaw,
			current:     testPinSDKRaw,
			wantVersion: testPinSDKNorm,
			wantReason:  cluster.ExportPinnedVersionAlreadyAtIt,
		},
		{
			// Cross-prefix equality: pin normalizes to "v..." while current is raw.
			name:        "unprefixed pin equal to v-prefixed current is a no-op",
			rawPinned:   testPinSDKRaw,
			current:     testPinSDKNorm,
			wantVersion: testPinSDKNorm,
			wantReason:  cluster.ExportPinnedVersionAlreadyAtIt,
		},
		{
			name:        "older pin than current skips the downgrade",
			rawPinned:   "v1.7.0",
			current:     testPinCurrent,
			wantVersion: "v1.7.0",
			wantReason:  cluster.ExportPinnedVersionNewer,
		},
		{
			name:        "unparseable current version falls through to proceed",
			rawPinned:   testPinNewer,
			current:     "",
			wantVersion: testPinNewer,
			wantReason:  cluster.ExportPinnedVersionProceed,
		},
	}
}

func TestNormalizePinnedVersion(t *testing.T) {
	t.Parallel()

	for _, testCase := range normalizePinnedVersionCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotVersion, gotReason, err := cluster.ExportNormalizePinnedVersion(
				testCase.rawPinned, testCase.current,
			)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantVersion, gotVersion)
			assert.Equal(t, testCase.wantReason, gotReason)
		})
	}
}

func TestNormalizePinnedVersion_EmptyPinReturnsError(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   "} {
		gotVersion, gotReason, err := cluster.ExportNormalizePinnedVersion(raw, testPinCurrent)
		require.ErrorIs(t, err, cluster.ErrEmptyPinnedVersion)
		assert.Empty(t, gotVersion)
		assert.Equal(t, cluster.ExportPinnedVersionProceed, gotReason)
	}
}

func TestNormalizePinnedVersion_InvalidPinReturnsError(t *testing.T) {
	t.Parallel()

	gotVersion, gotReason, err := cluster.ExportNormalizePinnedVersion(
		"not-a-version", testPinCurrent,
	)
	require.Error(t, err)
	require.NotErrorIs(t, err, cluster.ErrEmptyPinnedVersion)
	assert.Contains(t, err.Error(), "invalid pinned Talos version")
	assert.Empty(t, gotVersion)
	assert.Equal(t, cluster.ExportPinnedVersionProceed, gotReason)
}

func TestCategoryIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category clusterupdate.ChangeCategory
		want     string
	}{
		{"recreate", clusterupdate.ChangeCategoryRecreateRequired, "🔴"},
		{"rolling-recreate", clusterupdate.ChangeCategoryRollingRecreate, "🟠"},
		{"reboot", clusterupdate.ChangeCategoryRebootRequired, "🟡"},
		{"in-place", clusterupdate.ChangeCategoryInPlace, "🟢"},
		{"unknown", clusterupdate.ChangeCategory(99), "⚪"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, cluster.ExportCategoryIcon(testCase.category))
		})
	}
}

func TestGetOutputFormat(t *testing.T) {
	t.Parallel()

	t.Run("default is text", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		cmd.Flags().String("output", "text", "")

		format := cluster.ExportGetOutputFormat(cmd)
		assert.Equal(t, "text", format)
	})

	t.Run("json format", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		cmd.Flags().String("output", "json", "")

		format := cluster.ExportGetOutputFormat(cmd)
		assert.Equal(t, "json", format)
	})

	t.Run("no flag returns text default", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}
		format := cluster.ExportGetOutputFormat(cmd)
		assert.Equal(t, cluster.ExportOutputFormatText, format)
	})
}

func TestDiffToJSON_EmptyDiff(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{}
	out := cluster.ExportDiffToJSON(diff)

	assert.Equal(t, 0, out.TotalChanges)
	assert.Empty(t, out.InPlaceChanges)
	assert.Empty(t, out.RebootRequired)
	assert.Empty(t, out.RecreateRequired)
	assert.False(t, out.RequiresConfirmation)
}

func TestDiffToJSON_AllCategories(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "cni",
				OldValue: "cilium",
				NewValue: "calico",
				Category: clusterupdate.ChangeCategoryInPlace,
				Reason:   "component swap",
			},
		},
		RebootRequired: []clusterupdate.Change{
			{
				Field:    "nodeCount",
				OldValue: "1",
				NewValue: "3",
				Category: clusterupdate.ChangeCategoryRebootRequired,
				Reason:   "scaling",
			},
		},
		RecreateRequired: []clusterupdate.Change{
			{
				Field:    "distribution",
				OldValue: "Vanilla",
				NewValue: "K3s",
				Category: clusterupdate.ChangeCategoryRecreateRequired,
				Reason:   "distribution change",
			},
		},
	}

	out := cluster.ExportDiffToJSON(diff)
	assert.Equal(t, 3, out.TotalChanges)
	assert.Len(t, out.InPlaceChanges, 1)
	assert.Len(t, out.RebootRequired, 1)
	assert.Len(t, out.RecreateRequired, 1)
	assert.True(t, out.RequiresConfirmation)
	assert.Equal(t, "cni", out.InPlaceChanges[0].Field)
	assert.Equal(t, "component swap", out.InPlaceChanges[0].Reason)
}

func TestDiffToJSON_RollingRecreate(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		RollingRecreate: []clusterupdate.Change{
			{
				Field:    testFieldHetznerCPServerType,
				OldValue: "cx23",
				NewValue: "cpx41",
				Category: clusterupdate.ChangeCategoryRollingRecreate,
				Reason:   "rolling node replacement",
			},
		},
	}

	out := cluster.ExportDiffToJSON(diff)
	assert.Equal(t, 1, out.TotalChanges)
	assert.Len(t, out.RollingRecreate, 1)
	assert.True(t, out.RequiresConfirmation)
	assert.Equal(t, "rolling-recreate", out.RollingRecreate[0].Category)
	assert.Equal(t, testFieldHetznerCPServerType, out.RollingRecreate[0].Field)
}

func TestConfirmDisruptiveChanges( //nolint:funlen // Table-driven tests are naturally long.
	t *testing.T,
) {
	t.Parallel()

	rolling := &clusterupdate.UpdateResult{
		RollingRecreate: []clusterupdate.Change{
			{Field: testFieldHetznerCPServerType},
		},
	}
	reboot := &clusterupdate.UpdateResult{
		RebootRequired: []clusterupdate.Change{{Field: "cluster.cdi"}},
	}
	inPlace := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{{Field: testFieldClusterCNI}},
	}

	// Only branches that do not depend on TTY state are asserted: the
	// no-disruptive-change path, and the consent path (ShouldSkipPrompt always
	// skips the prompt when consent is given via --yes/--force). The interactive
	// prompt branch reads a global TTY/stdin state that is not injectable, so it
	// is left to E2E.
	tests := []struct {
		name             string
		diff             *clusterupdate.UpdateResult
		consent          bool
		wantAllowRolling bool
		wantProceed      bool
	}{
		{
			name:             "no disruptive changes proceeds without elevating consent",
			diff:             inPlace,
			consent:          false,
			wantAllowRolling: false,
			wantProceed:      true,
		},
		{
			name:             "rolling-recreate with consent proceeds",
			diff:             rolling,
			consent:          true,
			wantAllowRolling: true,
			wantProceed:      true,
		},
		{
			name:             "reboot-only with consent proceeds",
			diff:             reboot,
			consent:          true,
			wantAllowRolling: true,
			wantProceed:      true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			allowRolling, proceed := cluster.ExportConfirmDisruptiveChanges(
				cmd,
				testCase.diff,
				testCase.consent,
			)

			assert.Equal(t, testCase.wantAllowRolling, allowRolling)
			assert.Equal(t, testCase.wantProceed, proceed)
		})
	}
}

func TestReportFailedChanges(t *testing.T) {
	t.Parallel()

	t.Run("no failures writes nothing", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}

		var buf bytes.Buffer

		// reportFailedChanges writes via cmd.OutOrStderr(), which cobra backs with
		// the out writer when set.
		cmd.SetOut(&buf)

		cluster.ExportReportFailedChanges(cmd, clusterupdate.NewEmptyUpdateResult())
		assert.Empty(t, buf.String())
	})

	t.Run("failures are reported", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}

		var buf bytes.Buffer

		cmd.SetOut(&buf)

		result := clusterupdate.NewEmptyUpdateResult()
		result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
			Field:  "cluster.controlPlanes",
			Reason: "replacement failed",
		})

		cluster.ExportReportFailedChanges(cmd, result)

		output := buf.String()
		assert.Contains(t, output, "cluster.controlPlanes")
		assert.Contains(t, output, "replacement failed")
	})
}

func TestDisplayChangesSummary_EmptyChanges(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{}
	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()
	assert.Empty(t, output, "empty changes should produce no output")
}

func TestDiffToJSON_UnknownBaseline(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		UnknownBaseline: []clusterupdate.Change{
			{
				Field:    "cluster.cni",
				OldValue: clusterupdate.UnknownBaselineValue,
				NewValue: "Cilium",
				Category: clusterupdate.ChangeCategoryUnknown,
				Reason:   "current cluster state could not be read; baseline is unknown",
			},
		},
	}

	out := cluster.ExportDiffToJSON(diff)

	// Unknown-baseline entries are surfaced separately and never counted as
	// applicable changes.
	assert.Equal(t, 0, out.TotalChanges)
	assert.Len(t, out.UnknownBaseline, 1)
	assert.Equal(t, "cluster.cni", out.UnknownBaseline[0].Field)
	assert.Equal(t, "Unknown", out.UnknownBaseline[0].OldValue)
	assert.Equal(t, "unknown", out.UnknownBaseline[0].Category)
	assert.False(t, out.RequiresConfirmation)
}

func TestDisplayChangesSummary_UnknownBaselineOnly(t *testing.T) {
	t.Parallel()

	diff := &clusterupdate.UpdateResult{
		UnknownBaseline: []clusterupdate.Change{
			{
				Field:    "cluster.gitOpsEngine",
				OldValue: clusterupdate.UnknownBaselineValue,
				NewValue: "Flux",
				Category: clusterupdate.ChangeCategoryUnknown,
			},
		},
	}
	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()
	assert.Contains(t, output, "Change summary")
	assert.Contains(t, output, "cluster.gitOpsEngine")
	assert.Contains(t, output, "Unknown")
	assert.Contains(t, output, "could not be read")
}

func TestReportNoApplicableChanges_UnknownVsClean(t *testing.T) {
	t.Parallel()

	t.Run("clean cluster", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{}

		var out bytes.Buffer

		cmd.SetOut(&out)

		cluster.ExportReportNoApplicableChanges(cmd, clusterupdate.NewEmptyUpdateResult())
		assert.Contains(t, out.String(), "No changes detected")
	})

	t.Run("unknown baseline", func(t *testing.T) {
		t.Parallel()

		diff := clusterupdate.NewEmptyUpdateResult()
		diff.UnknownBaseline = append(diff.UnknownBaseline, clusterupdate.Change{
			Field:    "cluster.cni",
			OldValue: clusterupdate.UnknownBaselineValue,
			NewValue: "Cilium",
			Category: clusterupdate.ChangeCategoryUnknown,
		})

		cmd := &cobra.Command{}

		var errOut bytes.Buffer

		cmd.SetErr(&errOut)

		cluster.ExportReportNoApplicableChanges(cmd, diff)

		got := errOut.String()
		assert.Contains(t, got, "could not be read")
		assert.NotContains(t, got, "No changes detected")
	})
}
