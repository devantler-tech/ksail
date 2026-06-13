package vclusterprovisioner

import "fmt"

// Connection coordinates for a vCluster instance provisioned on a host Kubernetes cluster. These
// are the single source of truth for the naming contract the KubernetesProvisioner establishes —
// the namespace and Secret it publishes, the in-cluster API endpoint, and the TLS server name the
// served certificate is verified against. The operator imports these (rather than re-deriving them)
// so a naming change here can never silently break operator status observation or resource browsing.
const (
	// NamespacePrefix is the namespace prefix for vCluster instances on Kubernetes
	// ("vcluster-<name>").
	NamespacePrefix = "vcluster-"
	// SecretPrefix is the prefix for vCluster kubeconfig secrets ("vc-<name>").
	SecretPrefix = "vc-"
	// KubeconfigSecretKey is the key within the kubeconfig Secret that holds the kubeconfig YAML.
	KubeconfigSecretKey = "config"
	// ServiceAPIPort is the port the vCluster API Service exposes inside the host cluster.
	ServiceAPIPort = 443
	// InClusterServerName is the TLS server name on the vCluster API server certificate. The
	// in-cluster Service DNS name is not a SAN, so the served certificate is verified against this
	// name (with the kubeconfig's CA) while connecting to the Service address.
	InClusterServerName = "kubernetes"
)

// Connection holds the in-hub coordinates for a named vCluster: the namespace and kubeconfig Secret
// the provisioner publishes and the in-cluster API endpoint reachable from where the operator runs.
type Connection struct {
	// Namespace is the host-cluster namespace holding the vCluster ("vcluster-<name>").
	Namespace string
	// SecretName is the kubeconfig Secret name in Namespace ("vc-<name>").
	SecretName string
	// Endpoint is the in-cluster API server URL (https://<name>.<namespace>.svc:443).
	Endpoint string
}

// ConnectionFor returns the in-hub connection coordinates for the named vCluster. It is the single
// source of truth shared by the provisioner's Kubeconfig() and the operator's status observation and
// resource browsing, so the namespace/secret/endpoint contract is derived in exactly one place.
func ConnectionFor(name string) Connection {
	namespace := NamespacePrefix + name

	return Connection{
		Namespace:  namespace,
		SecretName: SecretPrefix + name,
		Endpoint:   fmt.Sprintf("https://%s.%s.svc:%d", name, namespace, ServiceAPIPort),
	}
}
