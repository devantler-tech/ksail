package workload_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail-go/cmd/workload"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
)

func TestNewInstallCmdRequiresMinimumArgs(t *testing.T) {
	t.Parallel()

	cmd := workload.NewInstallCmd(runtime.NewRuntime())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected argument validation error")
	}
}

func TestInstallCommandUsesDefaultNamespace(t *testing.T) {
	t.Parallel()

	err := runInstallCmd(t, "release", "./missing-chart")
	if err == nil {
		t.Fatalf("expected installation error due to missing chart")
	}

	if !strings.Contains(err.Error(), "install chart \"./missing-chart\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallCommandHonorsFlags(t *testing.T) {
	t.Parallel()

	err := runInstallCmd(
		t,
		"release",
		"./still-missing",
		"--namespace",
		"team",
		"--create-namespace",
		"--wait",
		"--atomic",
	)
	if err == nil {
		t.Fatalf("expected installation error due to missing chart")
	}

	if !strings.Contains(err.Error(), "install chart \"./still-missing\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runInstallCmd(t *testing.T, args ...string) error {
	t.Helper()

	cmd := workload.NewInstallCmd(runtime.NewRuntime())

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	t.Cleanup(cancel)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)

	err := cmd.Execute()
	if err != nil {
		return fmt.Errorf("execute install command: %w", err)
	}

	return nil
}
