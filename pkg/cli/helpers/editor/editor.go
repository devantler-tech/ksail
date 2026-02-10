package editor

import (
	"os"
	"os/exec"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/ksail"
)

// Resolver handles editor configuration resolution with proper precedence.
type Resolver struct {
	flagEditor   string
	configEditor string
}

// NewResolver creates a new editor resolver.
func NewResolver(flagEditor string, cfg *v1alpha1.Cluster) *Resolver {
	configEditor := ""
	if cfg != nil {
		configEditor = cfg.Spec.Editor
	}

	return &Resolver{
		flagEditor:   flagEditor,
		configEditor: configEditor,
	}
}

// Resolve resolves the editor command based on precedence:
// 1. --editor flag
// 2. spec.editor from config
// 3. Environment variables (SOPS_EDITOR, KUBE_EDITOR, EDITOR, VISUAL)
// 4. Fallback to vim, nano, vi.
func (r *Resolver) Resolve() string {
	// Priority 1: --editor flag
	if r.flagEditor != "" {
		return r.flagEditor
	}

	// Priority 2: spec.editor from config
	if r.configEditor != "" {
		return r.configEditor
	}

	// Priority 3: Environment variables
	// Check SOPS_EDITOR first (for cipher edit compatibility)
	if editorEnv := os.Getenv("SOPS_EDITOR"); editorEnv != "" {
		return editorEnv
	}

	// Check KUBE_EDITOR (for workload edit compatibility)
	if editorEnv := os.Getenv("KUBE_EDITOR"); editorEnv != "" {
		return editorEnv
	}

	// Check EDITOR
	if editorEnv := os.Getenv("EDITOR"); editorEnv != "" {
		return editorEnv
	}

	// Check VISUAL
	if editorEnv := os.Getenv("VISUAL"); editorEnv != "" {
		return editorEnv
	}

	// Priority 4: Fallback to common editors
	for _, editorName := range []string{"vim", "nano", "vi"} {
		editorPath, err := exec.LookPath(editorName)
		if err == nil {
			return editorPath
		}
	}

	return ""
}

// SetEnvVars sets the appropriate environment variables for the resolved editor.
// It returns a cleanup function that restores the original environment.
func (r *Resolver) SetEnvVars(editorCmd string, forCommand string) func() {
	if editorCmd == "" {
		return func() {}
	}

	// Store original values
	originalSOPSEditor := os.Getenv("SOPS_EDITOR")
	originalKubeEditor := os.Getenv("KUBE_EDITOR")
	originalEditor := os.Getenv("EDITOR")
	originalVisual := os.Getenv("VISUAL")

	// Set environment variables based on the command
	switch forCommand {
	case "cipher":
		// For cipher edit, set SOPS_EDITOR and EDITOR
		_ = os.Setenv("SOPS_EDITOR", editorCmd)
		_ = os.Setenv("EDITOR", editorCmd)
	case "workload":
		// For workload edit, set KUBE_EDITOR, EDITOR, and VISUAL
		_ = os.Setenv("KUBE_EDITOR", editorCmd)
		_ = os.Setenv("EDITOR", editorCmd)
		_ = os.Setenv("VISUAL", editorCmd)
	case "connect":
		// For cluster connect, set EDITOR, VISUAL, and optionally KUBE_EDITOR
		_ = os.Setenv("EDITOR", editorCmd)
		_ = os.Setenv("VISUAL", editorCmd)
		_ = os.Setenv("KUBE_EDITOR", editorCmd)
	}

	// Return cleanup function
	return func() {
		// Restore original values
		if originalSOPSEditor != "" {
			_ = os.Setenv("SOPS_EDITOR", originalSOPSEditor)
		} else {
			_ = os.Unsetenv("SOPS_EDITOR")
		}

		if originalKubeEditor != "" {
			_ = os.Setenv("KUBE_EDITOR", originalKubeEditor)
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
	}
}

// SetupEditorEnv sets up the editor environment variables based on flag and config.
// It returns a cleanup function that should be called to restore the original environment.
func SetupEditorEnv(editorFlag, forCommand string) func() {
	// Try to load config silently (don't error if it fails)
	var cfg *v1alpha1.Cluster

	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	cfgManager := ksailconfigmanager.NewConfigManager(nil, fieldSelectors...)

	loadedCfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
	if err == nil {
		cfg = loadedCfg
	}

	// Create editor resolver
	resolver := NewResolver(editorFlag, cfg)

	// Resolve the editor
	editorCmd := resolver.Resolve()

	// Set environment variables for the command
	return resolver.SetEnvVars(editorCmd, forCommand)
}
