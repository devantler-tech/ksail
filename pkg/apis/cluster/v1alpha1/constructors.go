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
		Provider:           "",
		CNI:                "",
		CSI:                "",
		LocalRegistry:      NewLocalRegistry(),
		GitOpsEngine:       GitOpsEngineNone,
		Vanilla:            NewClusterOptionsVanilla(),
		Talos:              NewClusterOptionsTalos(),
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

// NewClusterOptionsVanilla creates a new OptionsVanilla with default values.
func NewClusterOptionsVanilla() OptionsVanilla {
	return OptionsVanilla{}
}

// NewClusterOptionsTalos creates a new OptionsTalos with default values.
func NewClusterOptionsTalos() OptionsTalos {
	return OptionsTalos{}
}

// NewLocalRegistry creates a new LocalRegistry with default values.
func NewLocalRegistry() LocalRegistry {
	return LocalRegistry{
		Enabled:  false,
		HostPort: DefaultLocalRegistryPort,
	}
}

// NewOCIRegistry creates a new OCIRegistry with default lifecycle state.
func NewOCIRegistry() OCIRegistry {
	return OCIRegistry{
		Status: OCIRegistryStatusNotProvisioned,
	}
}
