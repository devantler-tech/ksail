package models

// KSailMetadata represents metadata for KSail objects
type KSailMetadata struct {
	Name string `yaml:"name" json:"name"`
}

// KSailProject represents the project configuration
type KSailProject struct {
	// ConfigPath is the path to the ksail configuration file
	ConfigPath string `yaml:"configPath" json:"configPath"`

	// DistributionConfigPath is the path to the distribution configuration file
	DistributionConfigPath string `yaml:"distributionConfigPath" json:"distributionConfigPath"`

	// KustomizationPath is the path to the root kustomization directory
	KustomizationPath string `yaml:"kustomizationPath" json:"kustomizationPath"`

	// ContainerEngine is the provider to use for running the KSail cluster
	ContainerEngine KSailContainerEngineType `yaml:"containerEngine" json:"containerEngine"`

	// Distribution is the Kubernetes distribution to use
	Distribution KSailDistributionType `yaml:"distribution" json:"distribution"`

	// DeploymentTool is the deployment tool to use
	DeploymentTool KSailDeploymentToolType `yaml:"deploymentTool" json:"deploymentTool"`

	// CNI is the CNI to use
	CNI KSailCNIType `yaml:"cni" json:"cni"`

	// CSI is the CSI to use
	CSI KSailCSIType `yaml:"csi" json:"csi"`

	// IngressController is the Ingress Controller to use
	IngressController KSailIngressControllerType `yaml:"ingressController" json:"ingressController"`

	// GatewayController is the Gateway Controller to use
	GatewayController KSailGatewayControllerType `yaml:"gatewayController" json:"gatewayController"`

	// MetricsServer indicates whether to install Metrics Server
	MetricsServer bool `yaml:"metricsServer" json:"metricsServer"`

	// SecretManager indicates whether to use a secret manager
	SecretManager KSailSecretManagerType `yaml:"secretManager" json:"secretManager"`

	// Editor is the editor to use for viewing files while debugging
	Editor KSailEditorType `yaml:"editor" json:"editor"`

	// MirrorRegistries indicates whether to set up mirror registries for the project
	MirrorRegistries bool `yaml:"mirrorRegistries" json:"mirrorRegistries"`
}

// NewDefaultProject creates a KSailProject with default values
func NewDefaultProject() *KSailProject {
	return &KSailProject{
		ConfigPath:             "ksail.yaml",
		DistributionConfigPath: "kind.yaml",
		KustomizationPath:      "k8s",
		ContainerEngine:        ContainerEngineDocker,
		Distribution:           DistributionKind,
		DeploymentTool:         DeploymentToolKubectl,
		CNI:                    CNIDefault,
		CSI:                    CSIDefault,
		IngressController:      IngressControllerDefault,
		GatewayController:      GatewayControllerDefault,
		MetricsServer:          true,
		SecretManager:          SecretManagerNone,
		Editor:                 EditorNano,
		MirrorRegistries:       true,
	}
}

// KSailClusterSpec represents the specification for a KSail cluster
type KSailClusterSpec struct {
	Name string `yaml:"name" json:"name"`
	KSailProject `yaml:",inline" json:",inline"`
}

// KSailCluster represents a KSail cluster configuration
type KSailCluster struct {
	ApiVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   KSailMetadata    `yaml:"metadata" json:"metadata"`
	Spec       KSailClusterSpec `yaml:"spec" json:"spec"`
}

// NewDefaultCluster creates a KSailCluster with default values
func NewDefaultCluster(name string) *KSailCluster {
	if name == "" {
		name = "ksail-default"
	}

	project := NewDefaultProject()
	
	return &KSailCluster{
		ApiVersion: "ksail.io/v1alpha1",
		Kind:       "Cluster",
		Metadata: KSailMetadata{
			Name: name,
		},
		Spec: KSailClusterSpec{
			Name:         name,
			KSailProject: *project,
		},
	}
}