package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewCluster creates a new Cluster instance with minimal required structure.
// All default values are now handled by the configuration system via field selectors.
func NewCluster() *Cluster {
	return &Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       Kind,
			APIVersion: APIVersion,
		},
		Spec: NewClusterSpec(),
	}
}

// NewClusterSpec creates a new Spec with default values.
func NewClusterSpec() Spec {
	return Spec{
		Editor:   "",
		Cluster:  NewClusterSubSpec(),
		Workload: NewWorkloadSubSpec(),
	}
}

// NewClusterSubSpec creates a new ClusterSpec with default values.
func NewClusterSubSpec() ClusterSpec {
	return ClusterSpec{
		DistributionConfig: "",
		Connection:         NewClusterConnection(),
		Distribution:       "",
		Provider:           ProviderDocker,
		CNI:                "",
		CSI:                "",
		LocalRegistry:      LocalRegistryDisabled,
		GitOpsEngine:       GitOpsEngineNone,
		// Distribution-specific options
		Kind:              NewClusterOptionsKind(),
		K3d:               NewClusterOptionsK3d(),
		Talos:             NewClusterOptionsTalos(),
		EKS:               NewClusterOptionsEKS(),
		LocalRegistryOpts: NewClusterOptionsLocalRegistry(),
	}
}

// NewWorkloadSubSpec creates a new WorkloadSpec with default values.
func NewWorkloadSubSpec() WorkloadSpec {
	return WorkloadSpec{
		SourceDirectory: "",
	}
}

// NewClusterConnection creates a new Connection with default values.
func NewClusterConnection() Connection {
	return Connection{
		Kubeconfig: "",
		Context:    "",
		Timeout:    metav1.Duration{Duration: 0},
	}
}

// NewClusterOptionsKind creates a new OptionsKind with default values.
func NewClusterOptionsKind() OptionsKind {
	return OptionsKind{}
}

// NewClusterOptionsK3d creates a new OptionsK3d with default values.
func NewClusterOptionsK3d() OptionsK3d {
	return OptionsK3d{}
}

// NewClusterOptionsTalos creates a new OptionsTalos with default values.
func NewClusterOptionsTalos() OptionsTalos {
	return OptionsTalos{}
}

// NewClusterOptionsEKS creates a new OptionsEKS with default values.
func NewClusterOptionsEKS() OptionsEKS {
	return OptionsEKS{}
}

// NewClusterOptionsLocalRegistry creates a new OptionsLocalRegistry with default values.
func NewClusterOptionsLocalRegistry() OptionsLocalRegistry {
	return OptionsLocalRegistry{}
}

// NewOCIRegistry creates a new OCIRegistry with default lifecycle state.
func NewOCIRegistry() OCIRegistry {
	return OCIRegistry{
		Status: OCIRegistryStatusNotProvisioned,
	}
}
