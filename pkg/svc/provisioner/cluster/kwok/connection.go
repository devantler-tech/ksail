package kwokprovisioner

import (
	"fmt"

	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
)

// Connection coordinates for a nested KWOK cluster on a host Kubernetes cluster. These are the
// single source of truth for the naming contract between the DinD exposure resources ksail creates
// (namespace + API-server exposure) and the kubeconfig Secret it publishes at create time. The
// operator imports these (rather than re-deriving them) so a naming change here can never silently
// break operator component installs or status observation.
//
// Unlike the operator-published distributions (k3k, vCluster), a KWOK cluster runs inside a DinD pod
// and writes its kubeconfig to a file; no nested controller publishes a Secret. So ksail publishes
// it itself at create time (see KubernetesProvisioner.publishConnectorKubeconfig) and Kubeconfig()
// serves it back to the operator as-published — mirroring the nested-Kind and nested-Talos
// Connectors.
const (
	// kwokKubeconfigSecretSuffix is appended to the cluster name for the kubeconfig Secret
	// ("kwok-<name>-kubeconfig", mirroring kind/talos/k3k's "<distribution>-<name>-kubeconfig").
	kwokKubeconfigSecretSuffix = "kubeconfig"
	// kwokKubeconfigKey is the Secret data key holding the kubeconfig ("kubeconfig.yaml", the same
	// key the other nested Connectors publish under).
	kwokKubeconfigKey = "kubeconfig.yaml"
	// kwokContextPrefix is the kubeconfig context (and cluster) name kwokctl writes for a created
	// cluster ("kwok-<name>"). The create-time publish minifies the shared host kubeconfig down to
	// this single context before storing it in the Secret.
	kwokContextPrefix = "kwok-"
	// kwokDistributionPrefix is the distribution prefix in the kubeconfig Secret name
	// ("kwok-<name>-kubeconfig", mirroring kind/talos/k3k's "<distribution>-<name>-kubeconfig"). It
	// is kept distinct from kwokContextPrefix — the two happen to share a value today but name
	// separate concepts (kwokctl's context prefix vs. the Secret naming scheme), so one can change
	// without silently shifting the other.
	kwokDistributionPrefix = "kwok-"
)

// Connection holds the in-hub coordinates for a named nested KWOK cluster: the namespace and
// kubeconfig Secret ksail publishes, and the context name to minify the published kubeconfig to.
type Connection struct {
	// Namespace is the host-cluster namespace holding the DinD pod and the published kubeconfig
	// Secret ("ksail-<name>" — the shared DinD exposure namespace, so the Secret's lifecycle is
	// tied to the cluster's and namespace deletion cleans it up).
	Namespace string
	// SecretName is the kubeconfig Secret name in Namespace ("kwok-<name>-kubeconfig").
	SecretName string
	// ContextName is the kubeconfig context kwokctl writes ("kwok-<name>"); the create-time publish
	// minifies the shared host kubeconfig to exactly this context so the operator's current-context
	// points at the nested cluster.
	ContextName string
}

// ConnectionFor returns the in-hub connection coordinates for the named nested KWOK cluster. It is
// the single source of truth shared by the provisioner's publishConnectorKubeconfig() and
// Kubeconfig(), so the namespace/secret/context contract is derived in exactly one place.
func ConnectionFor(name string) Connection {
	return Connection{
		Namespace: kubernetesprovider.NamespaceName(name),
		SecretName: fmt.Sprintf(
			"%s%s-%s",
			kwokDistributionPrefix,
			name,
			kwokKubeconfigSecretSuffix,
		),
		ContextName: kwokContextPrefix + name,
	}
}
