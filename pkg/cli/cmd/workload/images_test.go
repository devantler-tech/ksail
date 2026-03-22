package workload_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/gkampitakis/go-snaps/snaps"
)

func TestNewImagesCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewImagesCmd()

	if cmd.Use != "images" {
		t.Fatalf("expected Use to be %q, got %q", "images", cmd.Use)
	}

	if cmd.Short != "List container images required by cluster components" {
		t.Fatalf("expected Short description %q, got %q",
			"List container images required by cluster components", cmd.Short)
	}

	if !cmd.SilenceUsage {
		t.Fatal("expected SilenceUsage to be true")
	}

	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag to exist")
	}

	if outputFlag.DefValue != "plain" {
		t.Fatalf("expected --output flag default value to be %q, got %q",
			"plain", outputFlag.DefValue)
	}

	if outputFlag.Shorthand != "o" {
		t.Fatalf("expected --output shorthand to be %q, got %q", "o", outputFlag.Shorthand)
	}
}

func TestImagesCmdShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewImagesCmd()

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error executing images --help, got %v", err)
	}

	snaps.MatchSnapshot(t, normalizeHomePaths(output.String()))
}

func TestImagesCmdAcceptsValidOutputFormats(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "default is plain",
			args:     []string{},
			expected: "plain",
		},
		{
			name:     "explicit plain",
			args:     []string{"--output=plain"},
			expected: "plain",
		},
		{
			name:     "json format",
			args:     []string{"--output=json"},
			expected: "json",
		},
		{
			name:     "shorthand -o json",
			args:     []string{"-o", "json"},
			expected: "json",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewImagesCmd()

			err := cmd.ParseFlags(testCase.args)
			if err != nil {
				t.Fatalf("expected no error parsing flags %v, got %v", testCase.args, err)
			}

			got, err := cmd.Flags().GetString("output")
			if err != nil {
				t.Fatalf("expected no error getting output flag, got %v", err)
			}

			if got != testCase.expected {
				t.Fatalf("expected output flag %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestErrUnknownOutputFormatIsSentinelError(t *testing.T) {
	t.Parallel()

	if workload.ErrUnknownOutputFormat == nil {
		t.Fatal("expected ErrUnknownOutputFormat to be a non-nil sentinel error")
	}

	if workload.ErrUnknownOutputFormat.Error() == "" {
		t.Fatal("expected ErrUnknownOutputFormat.Error() to return a non-empty string")
	}

	wrapped := fmt.Errorf("wrapping: %w", workload.ErrUnknownOutputFormat)
	if !errors.Is(wrapped, workload.ErrUnknownOutputFormat) {
		t.Fatal("expected errors.Is to identify ErrUnknownOutputFormat through wrapping")
	}
}
