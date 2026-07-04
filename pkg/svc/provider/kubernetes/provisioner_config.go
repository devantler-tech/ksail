package kubernetes

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// DinDProvisionerConfig holds the host-cluster wiring shared by the DinD-based nested provisioners
// (Kind, KWOK): both run their nested cluster inside a Docker-in-Docker pod on the same host
// cluster, so they take the same host clients, Kubernetes provider, and nested-cluster identity.
// It is built once (see the factory's buildDinDProvisionerConfig) and embedded in each
// distribution's provisioner config so the two never drift and the assembly lives in one place.
type DinDProvisionerConfig struct {
	// KubeconfigPath is the path to the nested cluster's kubeconfig.
	KubeconfigPath string
	// HostClientset is the Kubernetes clientset for the host cluster, used to publish and read the
	// nested cluster's kubeconfig Secret for the operator Connector contract.
	HostClientset kubernetes.Interface
	// K8sProvider is the Kubernetes infrastructure provider.
	K8sProvider *Provider
	// DynamicClient is the dynamic client for Gateway API resources.
	DynamicClient dynamic.Interface
	// RestConfig is the REST config for port-forwarding to the DinD pod.
	RestConfig *rest.Config
	// ClusterName is the nested cluster name (used for namespace, labels).
	ClusterName string
	// Distribution is the distribution name (for labels).
	Distribution string
	// GatewayClassName is the Gateway class for API exposure (empty = no gateway).
	GatewayClassName string
	// Persistence holds PVC configuration for the DinD Docker data directory.
	Persistence v1alpha1.KubernetesPersistence
}
