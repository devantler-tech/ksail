package cmd_test

import (
	"os"
	"testing"

	pkgcmd "github.com/devantler-tech/ksail/pkg/cmd"

	"github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
)

func TestEditorResolver_ResolveEditor(t *testing.T) {
	tests := []struct {
		name         string
		flagEditor   string
		configEditor string
		envVars      map[string]string
		expected     string
	}{
		{
			name:       "flag takes precedence over config",
			flagEditor: "code --wait",
			configEditor: "vim",
			expected:   "code --wait",
		},
		{
			name:         "config takes precedence over env vars",
			flagEditor:   "",
			configEditor: "nano",
			envVars: map[string]string{
				"EDITOR": "vim",
			},
			expected: "nano",
		},
		{
			name:         "SOPS_EDITOR env var is used when no flag or config",
			flagEditor:   "",
			configEditor: "",
			envVars: map[string]string{
				"SOPS_EDITOR": "code --wait",
			},
			expected: "code --wait",
		},
		{
			name:         "KUBE_EDITOR env var is used when SOPS_EDITOR not set",
			flagEditor:   "",
			configEditor: "",
			envVars: map[string]string{
				"KUBE_EDITOR": "vim",
			},
			expected: "vim",
		},
		{
			name:         "EDITOR env var is used when SOPS_EDITOR and KUBE_EDITOR not set",
			flagEditor:   "",
			configEditor: "",
			envVars: map[string]string{
				"EDITOR": "nano",
			},
			expected: "nano",
		},
		{
			name:         "VISUAL env var is used when other env vars not set",
			flagEditor:   "",
			configEditor: "",
			envVars: map[string]string{
				"VISUAL": "emacs",
			},
			expected: "emacs",
		},
		{
			name:         "SOPS_EDITOR takes precedence over KUBE_EDITOR",
			flagEditor:   "",
			configEditor: "",
			envVars: map[string]string{
				"SOPS_EDITOR": "code",
				"KUBE_EDITOR": "vim",
			},
			expected: "code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Create config if configEditor is set
			var cfg *v1alpha1.Cluster
			if tt.configEditor != "" {
				cfg = &v1alpha1.Cluster{
					Spec: v1alpha1.Spec{
						Editor: tt.configEditor,
					},
				}
			}

			resolver := pkgcmd.NewEditorResolver(tt.flagEditor, cfg)
			result := resolver.ResolveEditor()

			if result != tt.expected {
				t.Errorf("ResolveEditor() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestEditorResolver_SetEditorEnvVars(t *testing.T) {
	tests := []struct {
		name           string
		editor         string
		forCommand     string
		expectedVars   map[string]string
		unexpectedVars []string
	}{
		{
			name:       "cipher command sets SOPS_EDITOR and EDITOR",
			editor:     "vim",
			forCommand: "cipher",
			expectedVars: map[string]string{
				"SOPS_EDITOR": "vim",
				"EDITOR":      "vim",
			},
		},
		{
			name:       "workload command sets KUBE_EDITOR, EDITOR, and VISUAL",
			editor:     "nano",
			forCommand: "workload",
			expectedVars: map[string]string{
				"KUBE_EDITOR": "nano",
				"EDITOR":      "nano",
				"VISUAL":      "nano",
			},
		},
		{
			name:       "connect command sets EDITOR, VISUAL, and KUBE_EDITOR",
			editor:     "code --wait",
			forCommand: "connect",
			expectedVars: map[string]string{
				"EDITOR":      "code --wait",
				"VISUAL":      "code --wait",
				"KUBE_EDITOR": "code --wait",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original environment variables before test
			originalSOPS := os.Getenv("SOPS_EDITOR")
			originalKube := os.Getenv("KUBE_EDITOR")
			originalEditor := os.Getenv("EDITOR")
			originalVisual := os.Getenv("VISUAL")

			// Clean up after test - restore originals
			defer func() {
				if originalSOPS != "" {
					_ = os.Setenv("SOPS_EDITOR", originalSOPS)
				} else {
					_ = os.Unsetenv("SOPS_EDITOR")
				}
				if originalKube != "" {
					_ = os.Setenv("KUBE_EDITOR", originalKube)
				} else {
					_ = os.Unsetenv("KUBE_EDITOR")
				}
				if originalEditor != "" {
					_ = os.Setenv("EDITOR", originalEditor)
				} else {
					_ = os.Unsetenv("EDITOR")
				}
				if originalVisual != "" {
					_ = os.Setenv("VISUAL", originalVisual)
				} else {
					_ = os.Unsetenv("VISUAL")
				}
			}()

			resolver := pkgcmd.NewEditorResolver("", nil)
			cleanup := resolver.SetEditorEnvVars(tt.editor, tt.forCommand)

			// Verify expected environment variables are set
			for k, v := range tt.expectedVars {
				if got := os.Getenv(k); got != v {
					t.Errorf("environment variable %s = %q, want %q", k, got, v)
				}
			}

			// Clean up
			cleanup()

			// Verify cleanup restored original values
			if got := os.Getenv("SOPS_EDITOR"); got != originalSOPS {
				t.Errorf("SOPS_EDITOR not restored: got %q, want %q", got, originalSOPS)
			}
			if got := os.Getenv("KUBE_EDITOR"); got != originalKube {
				t.Errorf("KUBE_EDITOR not restored: got %q, want %q", got, originalKube)
			}
			if got := os.Getenv("EDITOR"); got != originalEditor {
				t.Errorf("EDITOR not restored: got %q, want %q", got, originalEditor)
			}
			if got := os.Getenv("VISUAL"); got != originalVisual {
				t.Errorf("VISUAL not restored: got %q, want %q", got, originalVisual)
			}
		})
	}
}

func TestParseEditorCommand(t *testing.T) {
	tests := []struct {
		name     string
		editor   string
		expected []string
	}{
		{
			name:     "simple editor",
			editor:   "vim",
			expected: []string{"vim"},
		},
		{
			name:     "editor with single flag",
			editor:   "code --wait",
			expected: []string{"code", "--wait"},
		},
		{
			name:     "editor with multiple flags",
			editor:   "emacs -nw --no-desktop",
			expected: []string{"emacs", "-nw", "--no-desktop"},
		},
		{
			name:     "empty string",
			editor:   "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pkgcmd.ParseEditorCommand(tt.editor)

			if len(result) != len(tt.expected) {
				t.Errorf("ParseEditorCommand() length = %d, want %d", len(result), len(tt.expected))

				return
			}

			for i, part := range result {
				if part != tt.expected[i] {
					t.Errorf("ParseEditorCommand()[%d] = %q, want %q", i, part, tt.expected[i])
				}
			}
		})
	}
}
