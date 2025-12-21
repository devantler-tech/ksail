package cmd_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/cmd"
	pkgcmd "github.com/devantler-tech/ksail/v5/pkg/cmd"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/ui/timer"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
)

var errRootTest = errors.New("boom")

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestNewRootCmdVersionFormatting(t *testing.T) {
	t.Parallel()

	version := "1.2.3"
	commit := "abc123"
	date := "2025-08-17"
	cmd := cmd.NewRootCmd(version, commit, date)

	expectedVersion := version + " (Built on " + date + " from Git SHA " + commit + ")"
	if cmd.Version != expectedVersion {
		t.Fatalf("unexpected version string. want %q, got %q", expectedVersion, cmd.Version)
	}
}

func TestExecuteShowsHelp(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := cmd.NewRootCmd("", "", "")
	root.SetOut(&out)

	_ = root.Execute()

	snaps.MatchSnapshot(t, out.String())
}

func TestExecuteShowsHelpFlag(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := cmd.NewRootCmd("", "", "")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})

	_ = root.Execute()

	snaps.MatchSnapshot(t, out.String())
}

func TestExecuteShowsVersion(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := cmd.NewRootCmd("1.2.3", "abc123", "2025-08-17")
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})

	_ = root.Execute()

	snaps.MatchSnapshot(t, out.String())
}

func TestNewRootCmdTimingFlagDefaultFalse(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test", "test", "test")

	flag := root.PersistentFlags().Lookup(pkgcmd.TimingFlagName)
	if flag == nil {
		t.Fatalf("expected persistent flag %q to exist", pkgcmd.TimingFlagName)
	}

	got, err := root.PersistentFlags().GetBool(pkgcmd.TimingFlagName)
	if err != nil {
		t.Fatalf("expected to read %q flag: %v", pkgcmd.TimingFlagName, err)
	}

	if got {
		t.Fatalf("expected %q to default to false", pkgcmd.TimingFlagName)
	}
}

func TestDefaultRunDoesNotPrintTimingOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := setupRootWithBuffer(&out)

	probe := &cobra.Command{
		Use:  "timing-probe",
		RunE: timingProbeRunE(notify.SuccessType, "probe complete"),
	}

	root.AddCommand(probe)
	root.SetArgs([]string{"timing-probe"})

	_ = root.Execute()

	got := out.String()
	if strings.Contains(got, "⏲") {
		t.Fatalf("expected no timing glyph in default output, got %q", got)
	}

	if strings.Contains(got, "[stage:") {
		t.Fatalf("expected no timing bracket output in default output, got %q", got)
	}
}

func TestTimingFlagEnablesTimingOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := setupRootWithBuffer(&out)

	probe := &cobra.Command{
		Use:          "timing-probe",
		SilenceUsage: true,
		RunE:         timingProbeRunE(notify.SuccessType, "probe complete"),
	}

	root.AddCommand(probe)
	root.SetArgs([]string{"--timing", "timing-probe"})

	_ = root.Execute()

	got := out.String()
	if !strings.Contains(got, "⏲ current:") {
		t.Fatalf("expected timing block when --timing enabled, got %q", got)
	}

	if !strings.Contains(got, "total:") {
		t.Fatalf("expected total timing line when --timing enabled, got %q", got)
	}
}

func TestTimingDoesNotPrintOnError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := setupRootWithBuffer(&out)

	failing := &cobra.Command{
		Use:          "timing-fail",
		SilenceUsage: true,
		RunE:         timingProbeRunE(notify.ErrorType, "boom"),
	}

	root.AddCommand(failing)
	root.SetArgs([]string{"--timing", "timing-fail"})

	_ = root.Execute()

	got := out.String()
	if strings.Contains(got, "⏲") {
		t.Fatalf("expected no timing output on errors, got %q", got)
	}
}

// newTestCommand creates a cobra.Command for testing with exhaustive field initialization.
func newTestCommand(use string, runE func(*cobra.Command, []string) error) *cobra.Command {
	return &cobra.Command{
		Use:  use,
		RunE: runE,
	}
}

// setupRootWithBuffer creates a root command configured with the provided buffer for output.
func setupRootWithBuffer(out *bytes.Buffer) *cobra.Command {
	root := cmd.NewRootCmd("test", "test", "test")
	root.SetOut(out)
	root.SetErr(out)

	return root
}

// timingProbeRunE creates a RunE function that simulates timing operations for testing.
// It takes a message type and content, and returns a function that can be used as a Cobra RunE.
// When msgType is notify.ErrorType, the returned function will return errRootTest.
func timingProbeRunE(
	msgType notify.MessageType,
	content string,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		tmr := timer.New()
		tmr.Start()

		outputTimer := pkgcmd.MaybeTimer(cmd, tmr)

		notify.WriteMessage(notify.Message{
			Type:    msgType,
			Content: content,
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		if msgType == notify.ErrorType {
			return errRootTest
		}

		return nil
	}
}

func TestExecuteReturnsError(t *testing.T) {
	t.Parallel()

	failing := newTestCommand("fail", func(_ *cobra.Command, _ []string) error {
		return errRootTest
	})

	actual := cmd.NewRootCmd("test", "test", "test")
	actual.SetArgs([]string{"fail"})
	actual.AddCommand(failing)

	err := actual.Execute()
	if err == nil {
		t.Fatal("Expected error but got none")
	}

	if !errors.Is(err, errRootTest) {
		t.Fatalf("Expected error to be %v, got %v", errRootTest, err)
	}
}

func TestExecuteWithNonexistentCommand(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	root := cmd.NewRootCmd("test", "test", "test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"nonexistent"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Expected error but got none")
	}

	snaps.MatchSnapshot(t, out.String())
}

func TestExecuteSuccess(t *testing.T) {
	t.Parallel()

	succeeding := newTestCommand("ok", func(_ *cobra.Command, _ []string) error {
		return nil
	})

	actual := cmd.NewRootCmd("test", "test", "test")
	actual.SetArgs([]string{"ok"})
	actual.AddCommand(succeeding)

	err := actual.Execute()
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}
}

func TestExecuteWrapperSuccess(t *testing.T) {
	t.Parallel()

	succeeding := newTestCommand("ok", func(_ *cobra.Command, _ []string) error {
		return nil
	})

	rootCmd := cmd.NewRootCmd("test", "test", "test")
	rootCmd.SetArgs([]string{"ok"})
	rootCmd.AddCommand(succeeding)

	err := cmd.Execute(rootCmd)
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}
}

func TestExecuteWrapperError(t *testing.T) {
	t.Parallel()

	failing := newTestCommand("fail", func(_ *cobra.Command, _ []string) error {
		return errRootTest
	})

	rootCmd := cmd.NewRootCmd("test", "test", "test")
	rootCmd.SetArgs([]string{"fail"})
	rootCmd.AddCommand(failing)

	err := cmd.Execute(rootCmd)
	if err == nil {
		t.Fatal("Expected error but got none")
	}

	if !errors.Is(err, errRootTest) {
		t.Fatalf("Expected error to wrap %v, got %v", errRootTest, err)
	}
}
