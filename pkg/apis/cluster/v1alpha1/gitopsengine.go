package v1alpha1

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

// ValidGitOpsEngines enumerates supported GitOps engine values.
func ValidGitOpsEngines() []GitOpsEngine {
	return []GitOpsEngine{
		GitOpsEngineNone,
		GitOpsEngineFlux,
		GitOpsEngineArgoCD,
	}
}

// Set for GitOpsEngine (pflag.Value interface).
func (g *GitOpsEngine) Set(value string) error {
	return setEnum(g, value, ValidGitOpsEngines(), ErrInvalidGitOpsEngine)
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
	return validValueStrings(ValidGitOpsEngines())
}
