package runner_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/utils/runner"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

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

	snaps.MatchSnapshot(t, res.Stdout)
	snaps.MatchSnapshot(t, stdout.String())
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

	snaps.MatchSnapshot(t, err.Error())
	snaps.MatchSnapshot(t, res.Stdout)
	snaps.MatchSnapshot(t, res.Stderr)
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

	snaps.MatchSnapshot(t, res.Stdout)
}
