package workload_test

// cspell:words cmdtestutils

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/cmd"
	"github.com/devantler-tech/ksail/v5/cmd/workload"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/timer"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/samber/do/v2"
	"github.com/stretchr/testify/require"
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

func writeValidKsailConfig(t *testing.T, dir string) {
	t.Helper()

	workloadDir := filepath.Join(dir, "k8s")
	require.NoError(t, os.MkdirAll(workloadDir, 0o750))

	ksailConfigContent := "apiVersion: ksail.dev/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  distribution: Kind\n" +
		"  distributionConfig: kind.yaml\n" +
		"  sourceDirectory: k8s\n"

	configPath := filepath.Join(dir, "ksail.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(ksailConfigContent), 0o600))

	kindConfigContent := "kind: Cluster\n" +
		"apiVersion: kind.x-k8s.io/v1alpha4\n" +
		"name: kind\n"

	kindConfigPath := filepath.Join(dir, "kind.yaml")
	require.NoError(t, os.WriteFile(kindConfigPath, []byte(kindConfigContent), 0o600))
}

func TestWorkloadHelpSnapshots(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
	}{
		{name: "namespace", args: []string{"workload", "--help"}},
		{name: "reconcile", args: []string{"workload", "reconcile", "--help"}},
		{name: "push", args: []string{"workload", "push", "--help"}},
		{name: "apply", args: []string{"workload", "apply", "--help"}},
		{name: "create", args: []string{"workload", "create", "--help"}},
		{name: "create_source", args: []string{"workload", "create", "source", "--help"}},
		{
			name: "create_kustomization",
			args: []string{"workload", "create", "kustomization", "--help"},
		},
		// NOTE: create_helmrelease snapshot test temporarily disabled due to snapshot system issue
		// {name: "create_helmrelease", args: []string{"workload", "create", "helmrelease", "--help"}},
		{name: "delete", args: []string{"workload", "delete", "--help"}},
		{name: "describe", args: []string{"workload", "describe", "--help"}},
		{name: "edit", args: []string{"workload", "edit", "--help"}},
		{name: "exec", args: []string{"workload", "exec", "--help"}},
		{name: "explain", args: []string{"workload", "explain", "--help"}},
		{name: "expose", args: []string{"workload", "expose", "--help"}},
		{name: "get", args: []string{"workload", "get", "--help"}},
		{name: "install", args: []string{"workload", "install", "--help"}},
		{name: "logs", args: []string{"workload", "logs", "--help"}},
		{name: "rollout", args: []string{"workload", "rollout", "--help"}},
		{name: "scale", args: []string{"workload", "scale", "--help"}},
		{name: "wait", args: []string{"workload", "wait", "--help"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			root := cmd.NewRootCmd("test", "test", "test")
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(testCase.args)

			err := root.Execute()
			require.NoErrorf(
				t,
				err,
				"expected no error executing %s help",
				strings.Join(testCase.args, " "),
			)

			snaps.MatchSnapshot(t, out.String())
		})
	}
}

//nolint:paralleltest // Uses t.Chdir which is incompatible with parallel tests.
func TestWorkloadCommandsLoadConfigOnly(t *testing.T) {
	// Note: "apply" and "install" are excluded as they are full implementations with kubectl/helm wrappers
	commands := []string{"reconcile", "push"}

	for _, commandName := range commands {
		t.Run(commandName, func(t *testing.T) {
			var out bytes.Buffer

			tempDir := t.TempDir()
			writeValidKsailConfig(t, tempDir)

			t.Chdir(tempDir)

			root := cmd.NewRootCmd("test", "test", "test")
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"workload", commandName})

			err := root.Execute()
			require.ErrorContains(
				t,
				err,
				"GitOps engine must be enabled",
				"expected workload %s handler to require GitOps engine (push also requires local registry)",
				commandName,
			)

			actual := out.String()
			require.Contains(t, actual, "config loaded")
			require.NotContains(t, actual, "coming soon")
			require.NotContains(t, actual, "ℹ")
		})
	}
}

func TestNewWorkloadCmdRunETriggersHelp(t *testing.T) {
	t.Parallel()

	runtimeContainer := runtime.New(func(injector do.Injector) error {
		do.Provide(injector, func(do.Injector) (timer.Timer, error) {
			return timer.New(), nil
		})

		return nil
	})

	var out bytes.Buffer

	command := workload.NewWorkloadCmd(runtimeContainer)
	command.SetOut(&out)
	command.SetErr(&out)

	err := command.Execute()
	require.NoError(t, err)

	snaps.MatchSnapshot(t, out.String())
}
