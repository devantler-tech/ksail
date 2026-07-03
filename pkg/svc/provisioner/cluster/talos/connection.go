package talosprovisioner

import (
	"fmt"

	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
)

// Connection coordinates for a nested Talos cluster on a host Kubernetes cluster. These are the
// single source of truth for the naming contract between the DinD exposure resources ksail
// creates (namespace + apiserver Service) and the kubeconfig Secret it publishes at create time.
// The operator imports these (rather than re-deriving them) so a naming change here can never
// silently break operator component installs or status observation.
const (
	// talosKubeconfigSecretSuffix is appended to the prefixed cluster name for the kubeconfig
	// Secret ("talos-<name>-kubeconfig", mirroring k3k's "k3k-<name>-kubeconfig").
	talosKubeconfigSecretSuffix = "kubeconfig"
	// talosKubeconfigKey is the Secret data key holding the kubeconfig ("kubeconfig.yaml",
	// the same key the k3k operator publishes under).
	talosKubeconfigKey = "kubeconfig.yaml"
)

// Connection holds the in-hub coordinates for a named nested Talos cluster: the namespace and
// kubeconfig Secret ksail publishes and the in-cluster API endpoint reachable from where the
// ksail operator runs.
type Connection struct {
	// Namespace is the host-cluster namespace holding the DinD pod, the apiserver Service, and
	// the published kubeconfig Secret ("ksail-<name>" — the shared DinD exposure namespace, so
	// the Secret's lifecycle is tied to the cluster's and namespace deletion cleans it up).
	Namespace string
	// SecretName is the kubeconfig Secret name in Namespace ("talos-<name>-kubeconfig").
	SecretName string
	// Endpoint is the in-cluster API server URL ("https://apiserver.ksail-<name>:6443").
	// The bare "<service>.<namespace>" form (no ".svc" suffix) mirrors the k3k Connection
	// contract; CertSANs() covers both forms so either resolves and verifies.
	Endpoint string
}

// ConnectionFor returns the in-hub connection coordinates for the named nested Talos cluster. It
// is the single source of truth shared by prepareExposure (cert SANs), the create-time Secret
// publish, the provisioner's Kubeconfig(), and any operator-side consumer, so the
// namespace/secret/endpoint contract is derived in exactly one place.
func ConnectionFor(name string) Connection {
	namespace := kubernetesprovider.NamespaceName(name)

	return Connection{
		Namespace:  namespace,
		SecretName: fmt.Sprintf("talos-%s-%s", name, talosKubeconfigSecretSuffix),
		Endpoint: fmt.Sprintf(
			"https://%s.%s:%d",
			kubernetesprovider.APIServiceName, namespace, kubernetesprovider.DinDAPIServerPort,
		),
	}
}

// CertSANs returns the in-cluster DNS names the nested API server certificate must cover so the
// published kubeconfig's Endpoint verifies against the served cert with the kubeconfig's own CA
// (no tls-server-name override needed). Unlike k3k — whose operator inserts the Service DNS SAN
// itself — the Talos exposure flow only bakes in the host-reachable address, so ksail adds these
// during prepareExposure before the cluster PKI is generated.
func (c Connection) CertSANs() []string {
	host := kubernetesprovider.APIServiceName + "." + c.Namespace

	return []string{host, host + ".svc"}
}
