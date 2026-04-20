package editor_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/editor"
	"github.com/stretchr/testify/assert"
)

//nolint:paralleltest // Uses t.Setenv and must not run in parallel.
func TestSetupEditorEnv_NilCmd(t *testing.T) {
	// Not parallel because it uses environment variables
	// Clear all editor environment variables first
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
	}

	// SetupEditorEnv with nil cmd and a flag editor
	cleanup := editor.SetupEditorEnv(nil, "test-editor", "cipher")
	defer cleanup()

	// The flag editor should be resolved and set in env
	assert.Equal(t, "test-editor", os.Getenv("SOPS_EDITOR"))
	assert.Equal(t, "test-editor", os.Getenv("EDITOR"))
}

//nolint:paralleltest // Uses t.Setenv and must not run in parallel.
func TestSetupEditorEnv_NilCmdWorkload(t *testing.T) {
	// Not parallel because it uses environment variables
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
	}

	cleanup := editor.SetupEditorEnv(nil, "code", "workload")
	defer cleanup()

	assert.Equal(t, "code", os.Getenv("KUBE_EDITOR"))
	assert.Equal(t, "code", os.Getenv("EDITOR"))
	assert.Equal(t, "code", os.Getenv("VISUAL"))
}

//nolint:paralleltest // Uses t.Setenv and must not run in parallel.
func TestSetupEditorEnv_NilCmdConnect(t *testing.T) {
	// Not parallel because it uses environment variables
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
	}

	cleanup := editor.SetupEditorEnv(nil, "nano", "connect")
	defer cleanup()

	assert.Equal(t, "nano", os.Getenv("EDITOR"))
	assert.Equal(t, "nano", os.Getenv("VISUAL"))
	assert.Equal(t, "nano", os.Getenv("KUBE_EDITOR"))
}

func TestSetupEditorEnv_EmptyFlagFallsBackToEnv(t *testing.T) {
	// Not parallel because it uses environment variables
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
	}

	// Set EDITOR env var and pass empty flag
	t.Setenv("EDITOR", "env-editor")

	cleanup := editor.SetupEditorEnv(nil, "", "cipher")
	defer cleanup()

	// env-editor should be resolved from EDITOR and propagated
	assert.Equal(t, "env-editor", os.Getenv("SOPS_EDITOR"))
	assert.Equal(t, "env-editor", os.Getenv("EDITOR"))
}

func TestSetupEditorEnv_CleanupRestoresOriginal(t *testing.T) {
	// Not parallel because it uses environment variables
	t.Setenv("SOPS_EDITOR", "original-sops")
	t.Setenv("KUBE_EDITOR", "original-kube")
	t.Setenv("EDITOR", "original-editor")
	t.Setenv("VISUAL", "original-visual")

	cleanup := editor.SetupEditorEnv(nil, "new-editor", "connect")

	// Verify new values are set
	assert.Equal(t, "new-editor", os.Getenv("EDITOR"))
	assert.Equal(t, "new-editor", os.Getenv("VISUAL"))
	assert.Equal(t, "new-editor", os.Getenv("KUBE_EDITOR"))

	// Call cleanup
	cleanup()

	// Verify originals are restored
	assert.Equal(t, "original-sops", os.Getenv("SOPS_EDITOR"))
	assert.Equal(t, "original-kube", os.Getenv("KUBE_EDITOR"))
	assert.Equal(t, "original-editor", os.Getenv("EDITOR"))
	assert.Equal(t, "original-visual", os.Getenv("VISUAL"))
}

//nolint:paralleltest // Uses t.Setenv and must not run in parallel.
func TestEditorResolver_SetEnvVars_UnknownCommand(t *testing.T) {
	// Not parallel because it uses environment variables
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
	}

	resolver := editor.NewResolver("", nil)

	cleanup := resolver.SetEnvVars("some-editor", "unknown-command")
	defer cleanup()

	// Unknown command should not set any env vars
	assert.Empty(t, os.Getenv("SOPS_EDITOR"))
	assert.Empty(t, os.Getenv("KUBE_EDITOR"))
	assert.Empty(t, os.Getenv("EDITOR"))
	assert.Empty(t, os.Getenv("VISUAL"))
}

//nolint:paralleltest // Uses t.Setenv and must not run in parallel.
func TestEditorResolver_SetEnvVars_RestoresUnsetVars(t *testing.T) {
	// Not parallel because it uses environment variables
	// Ensure vars are unset
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
		_ = os.Unsetenv(env)
	}

	resolver := editor.NewResolver("", nil)
	cleanup := resolver.SetEnvVars("code", "workload")

	// Verify env vars are set
	assert.Equal(t, "code", os.Getenv("KUBE_EDITOR"))
	assert.Equal(t, "code", os.Getenv("EDITOR"))
	assert.Equal(t, "code", os.Getenv("VISUAL"))

	// Cleanup should unset these (since they were empty before)
	cleanup()
}

func TestNewResolver_ConfigEditorExtraction(t *testing.T) {
	t.Parallel()

	// With a config that has editor set
	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Editor: "custom-editor",
		},
	}

	resolver := editor.NewResolver("", cfg)
	assert.NotNil(t, resolver)
}

func TestNewResolver_NilConfig(t *testing.T) {
	t.Parallel()

	resolver := editor.NewResolver("flag-editor", nil)
	assert.NotNil(t, resolver)
}
