package models

// KSailDistributionType represents the supported Kubernetes distributions
type KSailDistributionType string

const (
	DistributionKind KSailDistributionType = "Kind"
	DistributionK3d  KSailDistributionType = "K3d"
)

// KSailContainerEngineType represents the supported container engines
type KSailContainerEngineType string

const (
	ContainerEngineDocker KSailContainerEngineType = "Docker"
	ContainerEnginePodman KSailContainerEngineType = "Podman"
)

// KSailDeploymentToolType represents the supported deployment tools
type KSailDeploymentToolType string

const (
	DeploymentToolKubectl KSailDeploymentToolType = "Kubectl"
	DeploymentToolFlux    KSailDeploymentToolType = "Flux"
)

// KSailCNIType represents the supported CNI options
type KSailCNIType string

const (
	CNIDefault KSailCNIType = "Default"
	CNICilium  KSailCNIType = "Cilium"
	CNINone    KSailCNIType = "None"
)

// KSailCSIType represents the supported CSI options
type KSailCSIType string

const (
	CSIDefault             KSailCSIType = "Default"
	CSILocalPathProvisioner KSailCSIType = "LocalPathProvisioner"
	CSINone                KSailCSIType = "None"
)

// KSailIngressControllerType represents the supported ingress controllers
type KSailIngressControllerType string

const (
	IngressControllerDefault KSailIngressControllerType = "Default"
	IngressControllerTraefik KSailIngressControllerType = "Traefik"
	IngressControllerNone    KSailIngressControllerType = "None"
)

// KSailGatewayControllerType represents the supported gateway controllers
type KSailGatewayControllerType string

const (
	GatewayControllerDefault KSailGatewayControllerType = "Default"
	GatewayControllerNone    KSailGatewayControllerType = "None"
)

// KSailSecretManagerType represents the supported secret managers
type KSailSecretManagerType string

const (
	SecretManagerNone KSailSecretManagerType = "None"
	SecretManagerSOPS KSailSecretManagerType = "SOPS"
)

// KSailEditorType represents the supported editors
type KSailEditorType string

const (
	EditorNano KSailEditorType = "Nano"
	EditorVim  KSailEditorType = "Vim"
)