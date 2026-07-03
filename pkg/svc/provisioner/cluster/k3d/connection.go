package k3dprovisioner

import "fmt"

// Connection coordinates for a k3k-provisioned K3s cluster on a host Kubernetes cluster. These are
// the single source of truth for the naming contract between ksail and the rancher/k3k operator —
// the namespace ksail creates, the kubeconfig Secret k3k publishes, and the in-cluster API endpoint
// the k3k server Service exposes. The operator imports these (rather than re-deriving them) so a
// naming change here can never silently break operator component installs or status observation.
const (
	// k3kServiceSuffix is appended to the prefixed cluster name for the k3k server Service
	// ("k3k-<name>-service", rancher/k3k's server.ServiceName contract).
	k3kServiceSuffix = "service"
	// k3kServiceAPIPort is the port the k3k server Service exposes the nested K3s API on inside
	// the host cluster (targeting the k3s server pods' 6443).
	k3kServiceAPIPort = 443
)

// Connection holds the in-hub coordinates for a named k3k cluster: the namespace and kubeconfig
// Secret the k3k operator publishes and the in-cluster API endpoint reachable from where the ksail
// operator runs.
type Connection struct {
	// Namespace is the host-cluster namespace holding the k3k cluster ("k3k-<name>").
	Namespace string
	// SecretName is the kubeconfig Secret name in Namespace ("k3k-<name>-kubeconfig").
	SecretName string
	// Endpoint is the in-cluster API server URL ("https://k3k-<name>-service.<namespace>:443").
	// The bare "<service>.<namespace>" form (no ".svc" suffix) is deliberate: it is the exact DNS
	// SAN k3k inserts into the nested k3s server certificate (rancher/k3k
	// pkg/controller/cluster/server/config.go Config()), so the endpoint verifies against the
	// served cert with the kubeconfig's own CA and needs no tls-server-name override.
	Endpoint string
}

// ConnectionFor returns the in-hub connection coordinates for the named k3k cluster. It is the
// single source of truth shared by the provisioner's Kubeconfig() and any operator-side consumer,
// so the namespace/secret/endpoint contract is derived in exactly one place.
func ConnectionFor(name string) Connection {
	namespace := k3kNamespacePrefix + name

	return Connection{
		Namespace:  namespace,
		SecretName: fmt.Sprintf("k3k-%s-%s", name, k3kKubeconfigSecretSuffix),
		Endpoint: fmt.Sprintf(
			"https://k3k-%s-%s.%s:%d",
			name, k3kServiceSuffix, namespace, k3kServiceAPIPort,
		),
	}
}
