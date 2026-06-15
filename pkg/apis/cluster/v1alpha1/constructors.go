package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewCluster creates a new Cluster instance with minimal required structure.
// All other default values are handled by the configuration system via field
// selectors.
func NewCluster() *Cluster {
	return &Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       Kind,
			APIVersion: APIVersion,
		},
		Spec: Spec{
			Cluster: ClusterSpec{
				GitOpsEngine: GitOpsEngineNone,
			},
		},
	}
}

// NewOCIRegistry creates a new OCIRegistry with default lifecycle state.
func NewOCIRegistry() OCIRegistry {
	return OCIRegistry{
		Status: OCIRegistryStatusNotProvisioned,
	}
}
