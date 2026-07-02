package project_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// mirrorRegistryHelp is the help text used when the test config-manager helper
// registers a stand-in mirror-registry flag (the init command registers the real
// flag via clusterflags.RegisterMirrorRegistryFlag; the text is not asserted).
const mirrorRegistryHelp = "Configure mirror registries with format 'host=upstream' " +
	"(e.g., docker.io=https://registry-1.docker.io)."

// setFlags sets the given flag values on cmd, failing the test on any error.
func setFlags(t *testing.T, cmd *cobra.Command, values map[string]string) {
	t.Helper()

	for k, v := range values {
		err := cmd.Flags().Set(k, v)
		if err != nil {
			t.Fatalf("failed to set flag %s: %v", k, err)
		}
	}
}

func newInitCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "init"}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	return cmd
}

func newConfigManager(
	t *testing.T,
	cmd *cobra.Command,
	writer io.Writer,
) *ksailconfigmanager.ConfigManager {
	t.Helper()
	cmd.SetOut(writer)
	cmd.SetErr(writer)
	manager := ksailconfigmanager.NewCommandConfigManager(cmd, project.InitFieldSelectors())
	// bind init-local flags like production code
	cmd.Flags().StringP("output", "o", "", "Output directory for the project")
	_ = manager.Viper.BindPFlag("output", cmd.Flags().Lookup("output"))
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	_ = manager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))
	cmd.Flags().String("multi-cluster", "", "Scaffold a multi-cluster source layout")
	_ = manager.Viper.BindPFlag("multi-cluster", cmd.Flags().Lookup("multi-cluster"))
	cmd.Flags().
		StringSlice("mirror-registry", []string{}, mirrorRegistryHelp)
	// NOTE: mirror-registry is NOT bound to Viper to allow custom merge logic in production
	// Tests that need to check mirror values should call getMirrorRegistriesWithDefaults()

	return manager
}

// writeKsailConfig creates a ksail.yaml config file in the specified directory.
func writeKsailConfig(t *testing.T, outDir string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "ksail.yaml"), []byte(content), 0o600))
}

// setupInitTest sets up a test command with configuration manager and common flags.
func setupInitTest(
	t *testing.T,
	outDir string,
	force bool,
	buffer *bytes.Buffer,
) (*cobra.Command, *ksailconfigmanager.ConfigManager) {
	t.Helper()
	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, buffer)

	forceStr := strconv.FormatBool(force)

	setFlags(t, cmd, map[string]string{
		"output": outDir,
		"force":  forceStr,
	})

	return cmd, cfgManager
}

func TestHandleInitRunE_SuccessWithOutputFlag(t *testing.T) {
	t.Parallel()

	// Using mockery-generated Timer (pkg/ui/timer/mocks.go) so we can set deterministic
	// expectations on timing calls without maintaining a bespoke RecordingTimer helper.

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, true, &buffer)

	deps := newInitDeps(t)

	var err error

	err = project.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	// Expectations asserted via mock cleanup

	snaps.MatchSnapshot(t, buffer.String())

	_, err = os.Stat(filepath.Join(outDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}
}

func TestHandleInitRunE_RespectsDistributionFlag(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, &buffer)

	setFlags(t, cmd, map[string]string{
		"output":              outDir,
		"distribution":        "K3s",
		"distribution-config": "k3d.yaml",
		"force":               "true",
	})

	deps := newInitDeps(t)

	err := project.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	_, err = os.Stat(filepath.Join(outDir, "k3d.yaml"))
	if err != nil {
		t.Fatalf("expected k3d.yaml to be scaffolded: %v", err)
	}
}

//nolint:funlen // Test function includes comprehensive assertions for Talos scaffolding
func TestHandleInitRunE_RespectsDistributionFlagTalos(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, &buffer)

	setFlags(t, cmd, map[string]string{
		"output":       outDir,
		"distribution": "Talos",
		"force":        "true",
	})

	deps := newInitDeps(t)

	err := project.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	// Verify the talos patches directory structure was created
	// Note: .gitkeep is NOT created in cluster/ because allow-scheduling patch is generated there
	expectedPaths := []string{
		filepath.Join(outDir, "talos", "control-planes", ".gitkeep"),
		filepath.Join(outDir, "talos", "workers", ".gitkeep"),
		filepath.Join(outDir, "talos", "cluster", "allow-scheduling-on-control-planes.yaml"),
	}

	for _, path := range expectedPaths {
		_, err = os.Stat(path)
		if err != nil {
			t.Fatalf("expected path to be scaffolded: %s, error: %v", path, err)
		}
	}

	// Verify allow-scheduling-on-control-planes.yaml content
	allowSchedulingPath := filepath.Join(
		outDir,
		"talos",
		"cluster",
		"allow-scheduling-on-control-planes.yaml",
	)

	//nolint:gosec // Test file path is safe
	allowSchedulingContent, err := os.ReadFile(allowSchedulingPath)
	if err != nil {
		t.Fatalf("expected allow-scheduling-on-control-planes.yaml to be scaffolded: %v", err)
	}

	if !strings.Contains(string(allowSchedulingContent), "allowSchedulingOnControlPlanes: true") {
		t.Fatalf(
			"expected allow-scheduling-on-control-planes.yaml to contain correct config\n%s",
			allowSchedulingContent,
		)
	}

	// Verify ksail.yaml contains Talos distribution
	ksailPath := filepath.Join(outDir, "ksail.yaml")

	content, err := os.ReadFile(ksailPath) //nolint:gosec // Test file path is safe
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}

	if !strings.Contains(string(content), "distribution: Talos") {
		t.Fatalf("expected ksail.yaml to contain Talos distribution\n%s", content)
	}

	// Verify output contains created files
	// Note: cluster/.gitkeep is NOT in output because allow-scheduling patch replaces it
	output := buffer.String()
	if !strings.Contains(output, "talos/control-planes/.gitkeep") {
		t.Fatalf("expected output to mention created talos directory structure\n%s", output)
	}
}

//nolint:paralleltest // Uses t.Chdir for snapshot setup.
func TestHandleInitRunE_UsesWorkingDirectoryWhenOutputUnset(t *testing.T) {
	workingDir := t.TempDir()

	var buffer bytes.Buffer

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, &buffer)

	t.Chdir(workingDir)

	setFlags(t, cmd, map[string]string{
		"force": "true",
	})

	deps := newInitDeps(t)

	var err error

	err = project.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	snaps.MatchSnapshot(t, buffer.String())

	_, err = os.Stat(filepath.Join(workingDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml in working directory: %v", err)
	}
}

func TestHandleInitRunE_DefaultsLocalRegistryWithFlux(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, io.Discard)

	setFlags(t, cmd, map[string]string{
		"output":        outDir,
		"force":         "true",
		"gitops-engine": "Flux",
	})

	deps := newInitDeps(t)

	err := project.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	//nolint:gosec // test file path is safe
	content, err := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}

	// With the new single-field design, local registry is enabled when the registry field is populated
	if !strings.Contains(string(content), "localRegistry:") ||
		!strings.Contains(string(content), "registry: localhost:5050") {
		t.Fatalf("expected ksail.yaml to enable local registry when Flux is selected\n%s", content)
	}
}

func TestHandleInitRunE_RespectsCertManagerFlag(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, io.Discard)

	setFlags(t, cmd, map[string]string{
		"output":       outDir,
		"force":        "true",
		"cert-manager": "Enabled",
	})

	deps := newInitDeps(t)

	err := project.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	//nolint:gosec // test file path is safe
	content, err := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}

	if !strings.Contains(string(content), "certManager: Enabled") {
		t.Fatalf("expected ksail.yaml to enable cert-manager when flag is set\n%s", content)
	}
}

func TestHandleInitRunE_IgnoresExistingConfigFile(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()
	existing := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  distribution: K3s\n" +
		"  distributionConfig: custom-k3d.yaml\n" +
		"  sourceDirectory: legacy\n"

	writeKsailConfig(t, outDir, existing)

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, true, &buffer)

	deps := newInitDeps(t)

	err := project.HandleInitRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	//nolint:gosec // test file path is safe
	content, readErr := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	require.NoError(t, readErr)

	// Ensure defaults are applied instead of values from the existing file.
	if strings.Contains(string(content), "distribution: K3s") {
		t.Fatalf("unexpected prior distribution carried over\n%s", string(content))
	}

	if strings.Contains(string(content), "distributionConfig: custom-k3d.yaml") {
		t.Fatalf("unexpected prior distributionConfig carried over\n%s", string(content))
	}

	if strings.Contains(string(content), "sourceDirectory: legacy") {
		t.Fatalf("unexpected prior sourceDirectory carried over\n%s", string(content))
	}
}

func newInitDeps(t *testing.T) project.InitDeps {
	t.Helper()
	tmr := timer.NewMockTimer(t)
	tmr.EXPECT().Start().Return()
	tmr.EXPECT().NewStage().Return()

	return project.InitDeps{Timer: tmr}
}

func TestHandleInitRunE_MultiClusterFlagScaffoldsLayout(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, false, &buffer)
	setFlags(t, cmd, map[string]string{"multi-cluster": "local"})

	err := project.HandleInitRunE(cmd, cfgManager, newInitDeps(t))
	require.NoError(t, err)

	for _, relPath := range []string{
		filepath.Join("k8s", "clusters", "base", "kustomization.yaml"),
		filepath.Join("k8s", "clusters", "local", "kustomization.yaml"),
	} {
		_, statErr := os.Stat(filepath.Join(outDir, relPath))
		require.NoError(t, statErr, "expected %s to be scaffolded", relPath)
	}

	//nolint:gosec // test reads from t.TempDir()
	ksailContent, err := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(ksailContent), "kustomizationFile: clusters/local")

	// The overlay replaces the flat single-cluster kustomization.
	_, err = os.Stat(filepath.Join(outDir, "k8s", "kustomization.yaml"))
	require.True(t, os.IsNotExist(err))
}

func TestHandleInitRunE_MultiClusterFlagRejectsReservedName(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, false, &buffer)
	setFlags(t, cmd, map[string]string{"multi-cluster": "base"})

	tmr := timer.NewMockTimer(t)
	tmr.EXPECT().Start().Return()
	tmr.EXPECT().NewStage().Return()

	err := project.HandleInitRunE(cmd, cfgManager, project.InitDeps{Timer: tmr})
	require.Error(t, err)
	require.ErrorContains(t, err, "reserved")

	// Fail-fast: nothing was scaffolded.
	_, statErr := os.Stat(filepath.Join(outDir, "ksail.yaml"))
	require.True(t, os.IsNotExist(statErr))
}
