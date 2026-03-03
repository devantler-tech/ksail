package v1alpha1

import (
	"fmt"
	"strings"
)

// GitOpsEngine defines the GitOps Engine options for a KSail cluster.
type GitOpsEngine string

const (
	// GitOpsEngineNone is the default and disables managed GitOps integration.
	// It means "no GitOps engine" is configured for the cluster.
	GitOpsEngineNone GitOpsEngine = "None"
	// GitOpsEngineFlux installs and manages Flux controllers.
	GitOpsEngineFlux GitOpsEngine = "Flux"
	// GitOpsEngineArgoCD installs and manages Argo CD.
	GitOpsEngineArgoCD GitOpsEngine = "ArgoCD"
)

// Set for GitOpsEngine (pflag.Value interface).
func (g *GitOpsEngine) Set(value string) error {
	for _, tool := range ValidGitOpsEngines() {
		if strings.EqualFold(value, string(tool)) {
			*g = tool

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidGitOpsEngine,
		value,
		GitOpsEngineNone,
		GitOpsEngineFlux,
		GitOpsEngineArgoCD,
	)
}

// String returns the string representation of the GitOpsEngine.
func (g *GitOpsEngine) String() string {
	return string(*g)
}

// Type returns the type of the GitOpsEngine.
func (g *GitOpsEngine) Type() string {
	return "GitOpsEngine"
}

// Default returns the default value for GitOpsEngine (None).
func (g *GitOpsEngine) Default() any {
	return GitOpsEngineNone
}

// ValidValues returns all valid GitOpsEngine values as strings.
func (g *GitOpsEngine) ValidValues() []string {
	return []string{string(GitOpsEngineNone), string(GitOpsEngineFlux), string(GitOpsEngineArgoCD)}
}
