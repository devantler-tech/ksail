package project_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// These tests drive the env verbs against workspaces scaffolded by the real
// `project init`, in both init modes, so the environment the scaffold declares
// and the environments the env verbs see can never drift apart again (issue
// #6905).

// initWorkspace scaffolds a project into a fresh directory — in multi-cluster
// mode when multiCluster is non-empty — and returns its root.
func initWorkspace(t *testing.T, multiCluster string) string {
	t.Helper()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, false, &buffer)
	if multiCluster != "" {
		setFlags(t, cmd, map[string]string{"multi-cluster": multiCluster})
	}

	require.NoError(t, project.HandleInitRunE(cmd, cfgManager, newInitDeps(t)))

	return outDir
}

// runEnvCmd executes an env verb standalone with args (the experimental gate
// satisfied for the gated verbs) and returns its combined output and error.
func runEnvCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()

	if cmd.Flags().Lookup(flags.ExperimentalFlagName) == nil {
		cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")
	}

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

type listedEnvironment struct {
	Name         string `json:"name"`
	Distribution string `json:"distribution"`
	Provider     string `json:"provider"`
	Config       string `json:"config"`
}

// scaffolded builds the listing expected for an environment scaffolded with the
// default distribution and provider, which the scaffold leaves implicit.
func scaffolded(name, config string) listedEnvironment {
	return listedEnvironment{
		Name:         name,
		Distribution: "Vanilla",
		Provider:     "Docker",
		Config:       config,
	}
}

// listEnvironments runs `env list --output json` and returns the declared
// environments.
func listEnvironments(t *testing.T) []listedEnvironment {
	t.Helper()

	out, err := runEnvCmd(t, env.NewListCmd(), "--output", "json")
	require.NoError(t, err, out)

	var listed []listedEnvironment

	require.NoError(t, json.Unmarshal([]byte(out), &listed), out)

	return listed
}

func readWorkspaceFile(t *testing.T, root, rel string) string {
	t.Helper()

	//nolint:gosec // G304: reads a file under the test's own t.TempDir().
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)

	return string(data)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestEnvVerbsSeeTheMultiClusterScaffoldsInitialEnvironment(t *testing.T) {
	root := initWorkspace(t, "prod")
	t.Chdir(root)

	baseConfig := readWorkspaceFile(t, root, "ksail.yaml")

	// env list reports the scaffolded environment, declared by the base config.
	assert.Equal(t, []listedEnvironment{scaffolded("prod", "ksail.yaml")}, listEnvironments(t))

	// env add can clone it: the new environment gets its own root config and
	// overlay, and the base config it was cloned from is untouched.
	out, err := runEnvCmd(t, env.NewAddCmd(), "staging", "--from", "prod")
	require.NoError(t, err, out)

	stagingConfig := readWorkspaceFile(t, root, "ksail.staging.yaml")
	assert.Contains(t, stagingConfig, "kustomizationFile: clusters/staging")
	assert.NotContains(t, stagingConfig, "clusters/prod")
	// The base config the scaffold wrote names no cluster, so the clone must name its
	// own: otherwise both environments resolve to the default cluster and context.
	assert.Contains(t, stagingConfig, "name: staging")
	assert.Contains(t, stagingConfig, "context: kind-staging")
	assert.Contains(
		t,
		readWorkspaceFile(t, root, "k8s/clusters/staging/kustomization.yaml"),
		"base",
	)
	assert.Equal(t, baseConfig, readWorkspaceFile(t, root, "ksail.yaml"))

	assert.Equal(t, []listedEnvironment{
		scaffolded("prod", "ksail.yaml"),
		scaffolded("staging", "ksail.staging.yaml"),
	}, listEnvironments(t))

	// env reconcile sees the same two environments, both already complete.
	out, err = runEnvCmd(t, env.NewReconcileCmd())
	require.NoError(t, err, out)
	assert.Contains(t, out, "prod         clusters/prod     Present")
	assert.Contains(t, out, "staging      clusters/staging  Present")
	assert.Contains(t, out, "nothing to generate")
	assert.NotContains(t, out, "orphan")

	// env rm refuses the base-synced environment instead of deleting the
	// workspace base config, and names the fix.
	out, err = runEnvCmd(t, env.NewRmCmd(), "prod")
	require.ErrorIs(t, err, env.ErrBaseSyncedEnvironment, out)
	require.ErrorContains(t, err, "spec.workload.kustomizationFile")
	assert.Equal(t, baseConfig, readWorkspaceFile(t, root, "ksail.yaml"))

	// ...while the cloned environment is removable as usual.
	out, err = runEnvCmd(t, env.NewRmCmd(), "staging")
	require.NoError(t, err, out)
	assert.Equal(t, []listedEnvironment{scaffolded("prod", "ksail.yaml")}, listEnvironments(t))

	// A --provider override is validated against the defaulted distribution the
	// scaffold leaves implicit, not against an empty one.
	out, err = runEnvCmd(t, env.NewAddCmd(), "dev", "--from", "prod", "--provider", "Docker")
	require.NoError(t, err, out)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestEnvVerbsNoEnvironmentsHintIsFollowableAfterPlainInit(t *testing.T) {
	root := initWorkspace(t, "")
	t.Chdir(root)

	// A plain init declares no environment, and the hint must name steps that
	// work from exactly this state — not `env add --from`, which has nothing to
	// clone.
	out, err := runEnvCmd(t, env.NewListCmd())
	require.NoError(t, err, out)
	assert.Contains(t, out, "no environments declared")
	assert.Contains(t, out, "spec.workload.kustomizationFile to clusters/<name>")
	assert.NotContains(t, out, "--from")

	// Follow the hint: point the base config at clusters/dev...
	setBaseKustomizationFile(t, root, "clusters/dev")

	assert.Equal(t, []listedEnvironment{scaffolded("dev", "ksail.yaml")}, listEnvironments(t))

	// ...scaffold its overlay with reconcile...
	out, err = runEnvCmd(t, env.NewReconcileCmd())
	require.NoError(t, err, out)
	assert.Contains(t, out, "reconciled 1 missing environment overlay(s)")

	// ...and the new environment can now be cloned.
	out, err = runEnvCmd(t, env.NewAddCmd(), "prod", "--from", "dev")
	require.NoError(t, err, out)
	assert.Contains(
		t,
		readWorkspaceFile(t, root, "ksail.prod.yaml"),
		"kustomizationFile: clusters/prod",
	)
}

// setBaseKustomizationFile rewrites the workspace base config's
// spec.workload.kustomizationFile, as a user following the hint would.
func setBaseKustomizationFile(t *testing.T, root, kustomizationFile string) {
	t.Helper()

	var config map[string]any

	require.NoError(t, yaml.Unmarshal([]byte(readWorkspaceFile(t, root, "ksail.yaml")), &config))

	// The default scaffold writes only non-default fields, so spec and
	// spec.workload may be absent and are created as a user would.
	spec, _ := config["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
		config["spec"] = spec
	}

	workload, _ := spec["workload"].(map[string]any)
	if workload == nil {
		workload = map[string]any{}
	}

	workload["kustomizationFile"] = kustomizationFile
	spec["workload"] = workload

	data, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.yaml"), data, 0o600))
}
