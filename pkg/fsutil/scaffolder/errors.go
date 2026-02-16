package scaffolder

import "errors"

// Scaffolding errors.
var (
	// ErrUnknownDistribution indicates an unsupported distribution was requested.
	ErrUnknownDistribution = errors.New("unknown distribution")

	// ErrKSailConfigGeneration wraps failures when creating ksail.yaml.
	ErrKSailConfigGeneration = errors.New("failed to generate ksail configuration")

	// ErrKindConfigGeneration wraps failures when creating Kind configuration.
	ErrKindConfigGeneration = errors.New("failed to generate kind configuration")

	// ErrK3dConfigGeneration wraps failures when creating K3d configuration.
	ErrK3dConfigGeneration = errors.New("failed to generate k3d configuration")

	// ErrTalosConfigGeneration wraps failures when creating Talos configuration.
	ErrTalosConfigGeneration = errors.New("failed to generate talos configuration")

	// ErrVClusterConfigGeneration wraps failures when creating vCluster configuration.
	ErrVClusterConfigGeneration = errors.New("failed to generate vcluster configuration")

	// ErrKustomizationGeneration wraps failures when creating kustomization.yaml.
	ErrKustomizationGeneration = errors.New("failed to generate kustomization configuration")

	// ErrGitOpsConfigGeneration wraps failures when creating GitOps CR manifests.
	ErrGitOpsConfigGeneration = errors.New("failed to generate gitops configuration")
)
