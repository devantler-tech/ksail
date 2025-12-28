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
		CNI:                "",
		CSI:                "",
		LocalRegistry:      LocalRegistryDisabled,
		GitOpsEngine:       GitOpsEngineNone,
		// Flattened options (previously nested under Options)
		Kind:              NewClusterOptionsKind(),
		K3d:               NewClusterOptionsK3d(),
		Talos:             NewClusterOptionsTalos(),
		Cilium:            NewClusterOptionsCilium(),
		Calico:            OptionsCalico{},
		Flux:              NewClusterOptionsFlux(),
		ArgoCD:            NewClusterOptionsArgoCD(),
		LocalRegistryOpts: NewClusterOptionsLocalRegistry(),
		Helm:              NewClusterOptionsHelm(),
		Kustomize:         NewClusterOptionsKustomize(),
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
	return OptionsTalos{
		Provider: TalosProviderDocker,
	}
}

// NewClusterOptionsCilium creates a new OptionsCilium with default values.
func NewClusterOptionsCilium() OptionsCilium {
	return OptionsCilium{}
}

// NewClusterOptionsFlux creates a new OptionsFlux with default values.
func NewClusterOptionsFlux() OptionsFlux {
	return OptionsFlux{}
}

// NewClusterOptionsArgoCD creates a new OptionsArgoCD with default values.
func NewClusterOptionsArgoCD() OptionsArgoCD {
	return OptionsArgoCD{}
}

// NewClusterOptionsLocalRegistry creates a new OptionsLocalRegistry with default values.
func NewClusterOptionsLocalRegistry() OptionsLocalRegistry {
	return OptionsLocalRegistry{}
}

// NewClusterOptionsHelm creates a new OptionsHelm with default values.
func NewClusterOptionsHelm() OptionsHelm {
	return OptionsHelm{}
}

// NewClusterOptionsKustomize creates a new OptionsKustomize with default values.
func NewClusterOptionsKustomize() OptionsKustomize {
	return OptionsKustomize{}
}

// NewOCIRegistry creates a new OCIRegistry with default lifecycle state.
func NewOCIRegistry() OCIRegistry {
	return OCIRegistry{
		Status: OCIRegistryStatusNotProvisioned,
	}
}
