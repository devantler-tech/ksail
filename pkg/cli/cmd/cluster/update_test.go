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
