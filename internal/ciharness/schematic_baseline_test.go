package ciharness_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	talosconfig "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestHetznerSchematicTrialPinsItsBaselineBeforeCreate(t *testing.T) {
	t.Parallel()

	var action compositeAction
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/actions/ksail-system-test/action.yaml"), &action,
	))
	resolve := findHarnessStep(t, action.Runs.Steps, "🔧 Resolve GHCR credentials in args")
	assert.Equal(t, "${{ inputs.test-talos-schematic-rollout }}", resolve.Env["SCHEMATIC_TRIAL"])
	assert.Less(t,
		harnessStepIndex(t, action.Runs.Steps, resolve.Name),
		harnessStepIndex(t, action.Runs.Steps, "🧪 ksail cluster create"),
	)

	for _, selected := range []string{"true", "false"} {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			fixture := newSchematicRolloutFixture(t)
			outputPath := filepath.Join(t.TempDir(), "output")

			commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			command := exec.CommandContext( //nolint:gosec // Executes the reviewed repository-owned action body.
				commandContext,
				"bash",
				"-c",
				resolve.Run,
			)
			command.Dir = fixture.project

			command.Env = append(os.Environ(),
				"ARGS=--name schematic-trial --image-verification cosign",
				"DISTRIBUTION=Talos", "PROVIDER=Hetzner", "GHCR_USER=", "GHCR_TOKEN=",
				"UPGRADE_FROM=", "UPGRADE_TO=", "INIT=true", "SCHEMATIC_TRIAL="+selected,
				"GITHUB_OUTPUT="+outputPath,
			)
			output, err := command.CombinedOutput()
			require.NoErrorf(t, err, "baseline resolution failed:\n%s", output)
			args, err := os.ReadFile(outputPath) //nolint:gosec // Test-owned temporary output path.
			require.NoError(t, err)

			if selected == "true" {
				assert.Contains(t, string(args), "--distribution-version v1.12.4")
			} else {
				assert.NotContains(t, string(args), "--distribution-version")
			}
		})
	}
}

func TestHetznerSchematicTrialFixtureIdentity(t *testing.T) {
	t.Parallel()

	var schematic talosconfig.Schematic
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/fixtures/talos-schematic-trial.yaml"), &schematic),
	)
	id, err := schematic.ID()
	require.NoError(t, err)
	assert.Equal(t, "c9078f9419961640c712a8bf2bb9174933dfcf1da383fd8ea2b7dc21493f8bac", id)
	assert.Equal(t, *talosconfig.NewSchematic([]string{"siderolabs/iscsi-tools"}, nil), schematic)
}

func TestHetznerSchematicTrialRejectsInvalidBaseline(t *testing.T) {
	t.Parallel()

	var action compositeAction
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/actions/ksail-system-test/action.yaml"), &action),
	)

	resolve := findHarnessStep(t, action.Runs.Steps, "🔧 Resolve GHCR credentials in args")
	for _, invalid := range []string{"provider", "init", "override", "missing-default", "invalid-default"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			fixture := newSchematicRolloutFixture(t)
			outputPath := filepath.Join(t.TempDir(), "output")
			provider, init, args := invalidSchematicBaseline(t, fixture.project, invalid)

			commandContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			command := exec.CommandContext( //nolint:gosec // Executes the reviewed repository-owned action body.
				commandContext,
				"bash",
				"-c",
				resolve.Run,
			)
			command.Dir = fixture.project

			command.Env = append(
				os.Environ(),
				"ARGS="+args,
				"DISTRIBUTION=Talos",
				"PROVIDER="+provider,
				"GHCR_USER=",
				"GHCR_TOKEN=",
				"UPGRADE_FROM=",
				"UPGRADE_TO=",
				"INIT="+init,
				"SCHEMATIC_TRIAL=true",
				"GITHUB_OUTPUT="+outputPath,
			)
			output, err := command.CombinedOutput()
			require.Errorf(t, err, "accepted invalid baseline: %s", output)
			_, err = os.Stat(outputPath)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func invalidSchematicBaseline(t *testing.T, project, invalid string) (string, string, string) {
	t.Helper()

	provider, init, args := "Hetzner", "true", "--name schematic-trial"
	defaultPath := filepath.Join(project, "pkg", "apis", "cluster", "v1alpha1", "defaults.go")

	switch invalid {
	case "provider":
		provider = "Docker"
	case "init":
		init = "false"
	case "override":
		args += " --distribution-version=v1.14.2"
	case "missing-default":
		require.NoError(t, os.Remove(defaultPath))
	case "invalid-default":
		require.NoError(
			t,
			os.WriteFile(defaultPath, []byte("DefaultHetznerTalosVersion = \"latest\"\n"), 0o600),
		)
	}

	return provider, init, args
}

func TestHetznerSchematicExtensionWouldChangeMachineConfig(t *testing.T) {
	t.Parallel()
	fixture := newSchematicRolloutFixture(t)
	writeSchematicBaseline(t, fixture, hetznerSchematicScenario{}, "v1.12.4")
	before := loadRenderedSchematicConfigs(t, fixture.project)
	path := filepath.Join(fixture.project, "ksail.yaml")
	config, err := os.ReadFile(path) //nolint:gosec // Test-owned temporary configuration path.
	require.NoError(t, err)

	config = append(config, []byte("      extensions:\n        - siderolabs/iscsi-tools\n")...)
	require.NoError(
		t,
		os.WriteFile( //nolint:gosec // Test-owned temporary configuration path.
			path,
			config,
			0o600,
		),
	)
	after := loadRenderedSchematicConfigs(t, fixture.project)
	assert.Contains(t, after.ControlPlane().Machine().Install().Image(), "c9078f")
	assert.NotEqual(
		t,
		before.ControlPlane().Machine().Install().Image(),
		after.ControlPlane().Machine().Install().Image(),
		"positive control: extensions must expose real installer configuration drift",
	)
}
