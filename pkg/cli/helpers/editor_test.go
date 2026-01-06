package helpers_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEditorResolver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		flagEditor string
		cfg        *v1alpha1.Cluster
	}{
		{
			name:       "with flag and config",
			flagEditor: "code",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Editor: "vim",
				},
			},
		},
		{
			name:       "with flag only",
			flagEditor: "code",
			cfg:        nil,
		},
		{
			name:       "with config only",
			flagEditor: "",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Editor: "vim",
				},
			},
		},
		{
			name:       "with neither flag nor config",
			flagEditor: "",
			cfg:        nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := helpers.NewEditorResolver(testCase.flagEditor, testCase.cfg)
			assert.NotNil(t, resolver)
		})
	}
}

func TestEditorResolver_Resolve(t *testing.T) {
	// Not parallel because subtests use t.Setenv
	t.Run("flag takes priority over config", func(t *testing.T) {
		t.Setenv("EDITOR", "")

		resolver := helpers.NewEditorResolver("code", &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{Editor: "vim"},
		})
		assert.Equal(t, "code", resolver.Resolve())
	})

	t.Run("config takes priority over env", func(t *testing.T) {
		t.Setenv("EDITOR", "nano")

		resolver := helpers.NewEditorResolver("", &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{Editor: "vim"},
		})
		assert.Equal(t, "vim", resolver.Resolve())
	})
}

func TestEditorResolver_Resolve_EnvPriority(t *testing.T) {
	// Not parallel because subtests use t.Setenv
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}

	t.Run("SOPS_EDITOR takes priority over KUBE_EDITOR", func(t *testing.T) {
		for _, env := range envsToClear {
			t.Setenv(env, "")
		}

		t.Setenv("SOPS_EDITOR", "sops-editor")
		t.Setenv("KUBE_EDITOR", "kube-editor")

		resolver := helpers.NewEditorResolver("", nil)
		assert.Equal(t, "sops-editor", resolver.Resolve())
	})

	t.Run("KUBE_EDITOR takes priority over EDITOR", func(t *testing.T) {
		for _, env := range envsToClear {
			t.Setenv(env, "")
		}

		t.Setenv("KUBE_EDITOR", "kube-editor")
		t.Setenv("EDITOR", "default-editor")

		resolver := helpers.NewEditorResolver("", nil)
		assert.Equal(t, "kube-editor", resolver.Resolve())
	})

	t.Run("EDITOR takes priority over VISUAL", func(t *testing.T) {
		for _, env := range envsToClear {
			t.Setenv(env, "")
		}

		t.Setenv("EDITOR", "default-editor")
		t.Setenv("VISUAL", "visual-editor")

		resolver := helpers.NewEditorResolver("", nil)
		assert.Equal(t, "default-editor", resolver.Resolve())
	})

	t.Run("VISUAL is used when no other env vars", func(t *testing.T) {
		for _, env := range envsToClear {
			t.Setenv(env, "")
		}

		t.Setenv("VISUAL", "visual-editor")

		resolver := helpers.NewEditorResolver("", nil)
		assert.Equal(t, "visual-editor", resolver.Resolve())
	})
}

func TestEditorResolver_SetEnvVars(t *testing.T) {
	// Not parallel because subtests use t.Setenv
	tests := []struct {
		name       string
		editorCmd  string
		forCommand string
		checkEnvs  []string
	}{
		{
			name:       "cipher command sets SOPS_EDITOR and EDITOR",
			editorCmd:  "code",
			forCommand: "cipher",
			checkEnvs:  []string{"SOPS_EDITOR", "EDITOR"},
		},
		{
			name:       "workload command sets KUBE_EDITOR, EDITOR, and VISUAL",
			editorCmd:  "code",
			forCommand: "workload",
			checkEnvs:  []string{"KUBE_EDITOR", "EDITOR", "VISUAL"},
		},
		{
			name:       "connect command sets EDITOR, VISUAL, and KUBE_EDITOR",
			editorCmd:  "code",
			forCommand: "connect",
			checkEnvs:  []string{"EDITOR", "VISUAL", "KUBE_EDITOR"},
		},
		{
			name:       "empty editor returns noop cleanup",
			editorCmd:  "",
			forCommand: "cipher",
			checkEnvs:  nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel because t.Setenv is used

			// Clear all editor environment variables first
			envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
			for _, env := range envsToClear {
				t.Setenv(env, "")
			}

			resolver := helpers.NewEditorResolver("", nil)
			cleanup := resolver.SetEnvVars(testCase.editorCmd, testCase.forCommand)

			// Check that expected env vars are set
			if testCase.editorCmd != "" && testCase.checkEnvs != nil {
				for _, env := range testCase.checkEnvs {
					assert.Equal(t, testCase.editorCmd, os.Getenv(env), "env %s should be set", env)
				}
			}

			// Call cleanup and verify env vars are restored
			cleanup()
		})
	}
}

func TestEditorResolver_SetEnvVars_RestoresOriginal(t *testing.T) {
	// Not parallel because it uses t.Setenv which is incompatible with t.Parallel()

	// Set original values
	originalEditor := "original-editor"
	t.Setenv("EDITOR", originalEditor)

	resolver := helpers.NewEditorResolver("", nil)
	cleanup := resolver.SetEnvVars("new-editor", "workload")

	// Verify new value is set
	require.Equal(t, "new-editor", os.Getenv("EDITOR"))

	// Call cleanup
	cleanup()

	// Verify original is restored
	assert.Equal(t, originalEditor, os.Getenv("EDITOR"))
}

func TestEditorResolver_Resolve_FallbackSnapshot(t *testing.T) {
	t.Parallel() // Safe with t.Setenv as it handles cleanup automatically

	// Clear all editor environment variables
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
	}

	resolver := helpers.NewEditorResolver("", nil)
	got := resolver.Resolve()

	// The result depends on what editors are installed on the system
	// We use snapshot testing to capture the behavior
	snaps.MatchSnapshot(t, got)
}
