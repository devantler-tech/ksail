package runner_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cmd/runner"
	"github.com/spf13/cobra"
)

var errCommandFailed = errors.New("boom")

func TestCobraCommandRunner_RunPropagatesStdout(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	runner := runner.NewCobraCommandRunner(&stdout, &stderr)

	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("hello world")
		},
	}

	res, err := runner.Run(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(res.Stdout, "hello world") {
		t.Fatalf("expected stdout to contain greeting, got %q", res.Stdout)
	}

	if !strings.Contains(stdout.String(), "hello world") {
		t.Fatalf("expected console output to contain greeting, got %q", stdout.String())
	}
}

func TestCobraCommandRunner_RunReturnsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	runner := runner.NewCobraCommandRunner(&stdout, &stderr)

	cmd := &cobra.Command{
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println("info output")
			cmd.PrintErrln("stderr detail")

			return errCommandFailed
		},
	}

	res, err := runner.Run(context.Background(), cmd, nil)
	if err == nil {
		t.Fatal("expected error when command fails")
	}

	if !strings.Contains(err.Error(), "command execution failed") {
		t.Fatalf("expected wrapped error message, got %q", err.Error())
	}

	if !strings.Contains(res.Stdout, "info output") {
		t.Fatalf("expected stdout capture, got %q", res.Stdout)
	}

	if !strings.Contains(res.Stderr, "stderr detail") {
		t.Fatalf("expected stderr capture, got %q", res.Stderr)
	}
}

func TestCobraCommandRunner_DefaultsToOsStdout(t *testing.T) {
	t.Parallel()

	// Test that nil defaults work
	runner := runner.NewCobraCommandRunner(nil, nil)

	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("test")
		},
	}

	res, err := runner.Run(context.Background(), cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(res.Stdout, "test") {
		t.Fatalf("expected stdout capture, got %q", res.Stdout)
	}
}
