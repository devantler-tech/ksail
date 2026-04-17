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

	// ErrK3dContainerdConfigGeneration wraps failures when creating K3d containerd config template.
	ErrK3dContainerdConfigGeneration = errors.New(
		"failed to generate k3d containerd config template",
	)

	// ErrTalosConfigGeneration wraps failures when creating Talos configuration.
	ErrTalosConfigGeneration = errors.New("failed to generate talos configuration")

	// ErrVClusterConfigGeneration wraps failures when creating vCluster configuration.
	ErrVClusterConfigGeneration = errors.New("failed to generate vcluster configuration")

	// ErrKWOKConfigGeneration wraps failures when creating KWOK configuration.
	ErrKWOKConfigGeneration = errors.New("failed to generate kwok configuration")

	// ErrEKSConfigGeneration wraps failures when creating EKS (eksctl) configuration.
	ErrEKSConfigGeneration = errors.New("failed to generate eksctl configuration")

	// ErrKustomizationGeneration wraps failures when creating kustomization.yaml.
	ErrKustomizationGeneration = errors.New("failed to generate kustomization configuration")

	// ErrInvalidKustomizationFilePath indicates an absolute or traversing kustomizationFile path.
	ErrInvalidKustomizationFilePath = errors.New(
		"kustomizationFile must be a relative path within the source tree",
	)

	// ErrGitOpsConfigGeneration wraps failures when creating GitOps CR manifests.
	ErrGitOpsConfigGeneration = errors.New("failed to generate gitops configuration")
)
