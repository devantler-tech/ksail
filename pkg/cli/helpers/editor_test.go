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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver := helpers.NewEditorResolver(tt.flagEditor, tt.cfg)
			assert.NotNil(t, resolver)
		})
	}
}

func TestEditorResolver_Resolve(t *testing.T) {
	// Not parallel due to environment variable manipulation
	tests := []struct {
		name       string
		flagEditor string
		cfg        *v1alpha1.Cluster
		envVars    map[string]string
		want       string
	}{
		{
			name:       "flag takes priority over config",
			flagEditor: "code",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Editor: "vim",
				},
			},
			want: "code",
		},
		{
			name:       "config takes priority over env",
			flagEditor: "",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Editor: "vim",
				},
			},
			envVars: map[string]string{"EDITOR": "nano"},
			want:    "vim",
		},
		{
			name:       "SOPS_EDITOR takes priority over KUBE_EDITOR",
			flagEditor: "",
			cfg:        nil,
			envVars: map[string]string{
				"SOPS_EDITOR": "sops-editor",
				"KUBE_EDITOR": "kube-editor",
			},
			want: "sops-editor",
		},
		{
			name:       "KUBE_EDITOR takes priority over EDITOR",
			flagEditor: "",
			cfg:        nil,
			envVars: map[string]string{
				"KUBE_EDITOR": "kube-editor",
				"EDITOR":      "default-editor",
			},
			want: "kube-editor",
		},
		{
			name:       "EDITOR takes priority over VISUAL",
			flagEditor: "",
			cfg:        nil,
			envVars: map[string]string{
				"EDITOR": "default-editor",
				"VISUAL": "visual-editor",
			},
			want: "default-editor",
		},
		{
			name:       "VISUAL is used when no other env vars",
			flagEditor: "",
			cfg:        nil,
			envVars: map[string]string{
				"VISUAL": "visual-editor",
			},
			want: "visual-editor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all editor environment variables first
			envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
			origEnvs := make(map[string]string)

			for _, env := range envsToClear {
				origEnvs[env] = os.Getenv(env)
				_ = os.Unsetenv(env)
			}

			// Set test-specific env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			resolver := helpers.NewEditorResolver(tt.flagEditor, tt.cfg)
			got := resolver.Resolve()

			// Restore original env vars
			for env, val := range origEnvs {
				if val != "" {
					_ = os.Setenv(env, val)
				}
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEditorResolver_SetEnvVars(t *testing.T) {
	// Not parallel due to environment variable manipulation
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all editor environment variables first
			envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
			origEnvs := make(map[string]string)

			for _, env := range envsToClear {
				origEnvs[env] = os.Getenv(env)
				_ = os.Unsetenv(env)
			}

			resolver := helpers.NewEditorResolver("", nil)
			cleanup := resolver.SetEnvVars(tt.editorCmd, tt.forCommand)

			// Check that expected env vars are set
			if tt.editorCmd != "" && tt.checkEnvs != nil {
				for _, env := range tt.checkEnvs {
					assert.Equal(t, tt.editorCmd, os.Getenv(env), "env %s should be set", env)
				}
			}

			// Call cleanup and verify env vars are restored
			cleanup()

			// Restore original env vars
			for env, val := range origEnvs {
				if val != "" {
					_ = os.Setenv(env, val)
				}
			}
		})
	}
}

func TestEditorResolver_SetEnvVars_RestoresOriginal(t *testing.T) {
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
	// Not parallel due to environment variable manipulation
	// Clear all editor environment variables
	envsToClear := []string{"SOPS_EDITOR", "KUBE_EDITOR", "EDITOR", "VISUAL"}
	for _, env := range envsToClear {
		t.Setenv(env, "")
		_ = os.Unsetenv(env)
	}

	resolver := helpers.NewEditorResolver("", nil)
	got := resolver.Resolve()

	// The result depends on what editors are installed on the system
	// We use snapshot testing to capture the behavior
	snaps.MatchSnapshot(t, got)
}
