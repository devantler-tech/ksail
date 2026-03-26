package cluster_test

import (
	"bytes"
	"strings"
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
)

func TestNewUpdateCmd(t *testing.T) {
	t.Parallel()

	runtimeContainer := &di.Runtime{}
	cmd := clusterpkg.NewUpdateCmd(runtimeContainer)

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

	// Verify flags
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("expected --force flag to exist")
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
}

func TestResolveForce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		forceValue bool
		yesValue   string
		expected   bool
	}{
		{name: "--force resolves to true", forceValue: true, yesValue: "", expected: true},
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

			runtimeContainer := &di.Runtime{}
			cmd := clusterpkg.NewUpdateCmd(runtimeContainer)

			if testCase.yesValue != "" {
				_ = cmd.Flags().Set("yes", testCase.yesValue)
			}

			result := clusterpkg.ExportResolveForce(testCase.forceValue, cmd.Flags().Lookup("yes"))
			if result != testCase.expected {
				t.Errorf("expected resolveForce(%v, yes=%q) = %v, got %v",
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
	clusterpkg.ExportDisplayChangesSummary(cmd, diff)

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

	clusterpkg.ExportDisplayChangesSummary(cmd, diff)

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

	clusterpkg.ExportDisplayChangesSummary(cmd, diff)

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

	runtimeContainer := &di.Runtime{}
	cmd := clusterpkg.NewUpdateCmd(runtimeContainer)

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

	clusterpkg.ExportDisplayChangesSummary(cmd, diff)

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
	clusterpkg.ExportDisplayChangesSummary(cmd, diff)

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

	out := clusterpkg.ExportDiffToJSON(diff)

	assertDiffCounts(t, out)
	assertInPlaceChange(t, out.InPlaceChanges[0])
	assertCategories(t, out)
}

func assertDiffCounts(t *testing.T, out clusterpkg.DiffJSONOutput) {
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

func assertInPlaceChange(t *testing.T, inPlace clusterpkg.ChangeJSON) {
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

func assertCategories(t *testing.T, out clusterpkg.DiffJSONOutput) {
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

	out := clusterpkg.ExportDiffToJSON(diff)

	if out.RequiresConfirmation {
		t.Error("expected RequiresConfirmation=false for in-place-only changes")
	}
}
