package cmd_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	pkgcmd "github.com/devantler-tech/ksail/pkg/cmd"
)

//nolint:paralleltest // Cannot parallelize tests that modify environment variables
func TestEditorResolver_ResolveEditor(t *testing.T) {
	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("flag takes precedence over config", func(t *testing.T) {
		testEditorPrecedence(t, "code --wait", "vim", nil, "code --wait")
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("config takes precedence over env vars", func(t *testing.T) {
		testEditorPrecedence(t, "", "nano", map[string]string{"EDITOR": "vim"}, "nano")
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("SOPS_EDITOR env var is used when no flag or config", func(t *testing.T) {
		testEditorPrecedence(
			t,
			"",
			"",
			map[string]string{"SOPS_EDITOR": "code --wait"},
			"code --wait",
		)
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("KUBE_EDITOR env var is used when SOPS_EDITOR not set", func(t *testing.T) {
		testEditorPrecedence(t, "", "", map[string]string{"KUBE_EDITOR": "vim"}, "vim")
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("EDITOR env var is used when SOPS_EDITOR and KUBE_EDITOR not set", func(t *testing.T) {
		testEditorPrecedence(t, "", "", map[string]string{"EDITOR": "nano"}, "nano")
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("VISUAL env var is used when other env vars not set", func(t *testing.T) {
		testEditorPrecedence(t, "", "", map[string]string{"VISUAL": "emacs"}, "emacs")
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("SOPS_EDITOR takes precedence over KUBE_EDITOR", func(t *testing.T) {
		testEditorPrecedence(t, "", "", map[string]string{
			"SOPS_EDITOR": "code",
			"KUBE_EDITOR": "vim",
		}, "code")
	})
}

func testEditorPrecedence(
	t *testing.T,
	flagEditor, configEditor string,
	envVars map[string]string,
	expected string,
) {
	t.Helper()

	// Set up environment variables
	for key, value := range envVars {
		t.Setenv(key, value)
	}

	// Create config if configEditor is set
	var cfg *v1alpha1.Cluster
	if configEditor != "" {
		cfg = &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Editor: configEditor,
			},
		}
	}

	resolver := pkgcmd.NewEditorResolver(flagEditor, cfg)
	result := resolver.ResolveEditor()

	if result != expected {
		t.Errorf("ResolveEditor() = %q, want %q", result, expected)
	}
}

//nolint:paralleltest // Cannot parallelize tests that modify environment variables
func TestEditorResolver_SetEditorEnvVars(t *testing.T) {
	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("cipher command sets SOPS_EDITOR and EDITOR", func(t *testing.T) {
		testEditorEnvVars(t, "vim", "cipher", map[string]string{
			"SOPS_EDITOR": "vim",
			"EDITOR":      "vim",
		})
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("workload command sets KUBE_EDITOR, EDITOR, and VISUAL", func(t *testing.T) {
		testEditorEnvVars(t, "nano", "workload", map[string]string{
			"KUBE_EDITOR": "nano",
			"EDITOR":      "nano",
			"VISUAL":      "nano",
		})
	})

	//nolint:paralleltest // Cannot parallelize tests that modify environment variables
	t.Run("connect command sets EDITOR, VISUAL, and KUBE_EDITOR", func(t *testing.T) {
		testEditorEnvVars(t, "code --wait", "connect", map[string]string{
			"EDITOR":      "code --wait",
			"VISUAL":      "code --wait",
			"KUBE_EDITOR": "code --wait",
		})
	})
}

func testEditorEnvVars(t *testing.T, editor, forCommand string, expectedVars map[string]string) {
	t.Helper()

	// Store original environment variables before test
	originals := saveEnvVars()

	// Clean up after test - restore originals
	t.Cleanup(func() {
		restoreEnvVars(originals)
	})

	resolver := pkgcmd.NewEditorResolver("", nil)
	cleanup := resolver.SetEditorEnvVars(editor, forCommand)

	// Verify expected environment variables are set
	for key, value := range expectedVars {
		if got := os.Getenv(key); got != value {
			t.Errorf("environment variable %s = %q, want %q", key, got, value)
		}
	}

	// Clean up
	cleanup()

	// Verify cleanup restored original values
	verifyEnvVarsRestored(t, originals)
}

func saveEnvVars() map[string]string {
	return map[string]string{
		"SOPS_EDITOR": os.Getenv("SOPS_EDITOR"),
		"KUBE_EDITOR": os.Getenv("KUBE_EDITOR"),
		"EDITOR":      os.Getenv("EDITOR"),
		"VISUAL":      os.Getenv("VISUAL"),
	}
}

func restoreEnvVars(originals map[string]string) {
	for key, value := range originals {
		if value != "" {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func verifyEnvVarsRestored(t *testing.T, originals map[string]string) {
	t.Helper()

	for key, originalValue := range originals {
		if got := os.Getenv(key); got != originalValue {
			t.Errorf("%s not restored: got %q, want %q", key, got, originalValue)
		}
	}
}
