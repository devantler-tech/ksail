package workload_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/cmd/workload"
)

func TestNewValidateCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	if cmd.Use != "validate [PATH]" {
		t.Fatalf("expected Use to be 'validate [PATH]', got %q", cmd.Use)
	}

	if cmd.Short != "Validate Kubernetes manifests and kustomizations" {
		t.Fatalf(
			"expected Short description to be 'Validate Kubernetes manifests and kustomizations', got %q",
			cmd.Short,
		)
	}

	// Check default flag values
	skipSecrets, _ := cmd.Flags().GetBool("skip-secrets")
	if !skipSecrets {
		t.Fatal("expected skip-secrets to default to true")
	}

	strict, _ := cmd.Flags().GetBool("strict")
	if !strict {
		t.Fatal("expected strict to default to true")
	}

	ignoreMissingSchemas, _ := cmd.Flags().GetBool("ignore-missing-schemas")
	if !ignoreMissingSchemas {
		t.Fatal("expected ignore-missing-schemas to default to true")
	}
}

func TestValidateCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	helpText := output.String()
	if !strings.Contains(helpText, "Validate Kubernetes manifest") {
		t.Fatal("expected help text to contain 'Validate Kubernetes manifest'")
	}

	if !strings.Contains(helpText, "kubeconform") {
		t.Fatal("expected help text to mention kubeconform")
	}

	if !strings.Contains(helpText, "--skip-secrets") {
		t.Fatal("expected help text to include --skip-secrets flag")
	}

	if !strings.Contains(helpText, "--strict") {
		t.Fatal("expected help text to include --strict flag")
	}
}

func TestValidateCmdRejectsMultiplePaths(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	// This test validates that the command rejects multiple path arguments
	cmd.SetArgs([]string{
		"/some/path1",
		"/some/path2",
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	// We expect an error because multiple paths are not allowed
	if err == nil {
		t.Fatal("expected error for multiple paths")
	}

	// The error should be about too many arguments
	if !strings.Contains(err.Error(), "accepts at most 1 arg(s)") {
		t.Fatalf("expected error about too many arguments, got %v", err)
	}
}

func TestValidateCmdAcceptsSinglePath(t *testing.T) {
	t.Parallel()

	cmd := workload.NewValidateCmd()

	// This test validates that the command accepts a single path argument
	// It will fail during execution because the path doesn't exist, but that's expected
	cmd.SetArgs([]string{
		"/nonexistent/path",
	})

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	// We expect an error because the path doesn't exist
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}

	// The error should be about path access, not argument parsing
	if !strings.Contains(err.Error(), "access path") {
		t.Fatalf("expected error about path access, got %v", err)
	}
}
