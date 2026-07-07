package hetznerbase

import (
	"context"
	"fmt"
	"time"

	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

const (
	// connectorKubeconfigKey is the Secret data key holding the kubeconfig
	// ("kubeconfig.yaml", the same key the nested Connectors publish under).
	connectorKubeconfigKey = "kubeconfig.yaml"
	// connectorSecretSuffix is appended to the cluster name for the kubeconfig
	// Secret ("<prefix>-<name>-kubeconfig", mirroring the nested distributions'
	// "<distribution>-<name>-kubeconfig" convention).
	connectorSecretSuffix = "kubeconfig"
	// connectorPublishSteps / connectorPublishBaseDelay / connectorPublishFactor
	// bound the retry around the hub Secret upsert. The publish runs at the very
	// end of a paid bring-up whose failure path tears the cluster down, so a
	// transient hub API blip must be absorbed here; only a persistent failure —
	// the operator could never reach the child anyway — is worth the teardown.
	connectorPublishSteps     = 4
	connectorPublishBaseDelay = 250 * time.Millisecond
	connectorPublishFactor    = 2.0
)

// connectorSecretName is the single source of truth for the Connector kubeconfig
// Secret's name, shared by the create-time publish, the Connector read, and the
// delete-time cleanup.
func (b *Base) connectorSecretName(clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", b.ConnectorSecretPrefix, clusterName, connectorSecretSuffix)
}

// Kubeconfig implements the clusterprovisioner.Connector capability for a Hetzner
// cluster: it returns the kubeconfig published at create time (see
// publishConnectorKubeconfig), whose API server already points at the node's
// public IPv4 — reachable from where the operator runs. It returns
// clustererr.ErrKubeconfigNotReady (via nested.ConnectorKubeconfig) while the
// Secret is not yet published, so the caller requeues. Hetzner is not a managed
// service and the bootstrap SSH keypair is discarded after create, so the
// published Secret is the only operator-reachable credential source.
func (b *Base) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	target := b.ResolveName(name)

	raw, err := nested.ConnectorKubeconfig(
		ctx, b.Hub, target,
		b.HubNamespace, b.connectorSecretName(target), connectorKubeconfigKey,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.ConnectorSecretPrefix, err)
	}

	return raw, nil
}

// publishConnectorKubeconfig upserts the endpoint-rewritten admin kubeconfig as a
// hub Secret under the Connector naming contract so Kubeconfig() can serve it back
// to the operator. Without a configured hub clientset (the CLI flow) it is a
// no-op — there is no hub to publish to and nothing consumes the Connector.
func (b *Base) publishConnectorKubeconfig(
	ctx context.Context,
	clusterName string,
	kubeconfig []byte,
) error {
	if b.Hub == nil {
		return nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.connectorSecretName(clusterName),
			Namespace: b.HubNamespace,
			Labels:    kubernetesprovider.CommonLabels(clusterName),
		},
		Data: map[string][]byte{connectorKubeconfigKey: kubeconfig},
	}

	// Retry any error with backoff: UpsertSecret itself only retries update
	// conflicts, and the caller's failure path destroys the just-provisioned
	// servers — too drastic for a transient hub API failure.
	backoff := wait.Backoff{
		Steps:    connectorPublishSteps,
		Duration: connectorPublishBaseDelay,
		Factor:   connectorPublishFactor,
	}

	err := retry.OnError(backoff, func(error) bool { return true }, func() error {
		return nested.UpsertSecret(ctx, b.Hub, secret)
	})
	if err != nil {
		return fmt.Errorf("publish connector kubeconfig: %w", err)
	}

	return nil
}

// deleteConnectorKubeconfig removes the published Connector kubeconfig Secret when
// the cluster is deleted. Unlike the nested distributions — whose Secret lives in a
// per-cluster hub namespace that is deleted with the cluster — the Hetzner Secret
// lives in the operator's namespace, so it must be cleaned up explicitly. An
// absent Secret (never published, or already cleaned) is not an error.
func (b *Base) deleteConnectorKubeconfig(ctx context.Context, clusterName string) error {
	if b.Hub == nil {
		return nil
	}

	name := b.connectorSecretName(clusterName)

	err := b.Hub.CoreV1().Secrets(b.HubNamespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete connector kubeconfig secret %s/%s: %w", b.HubNamespace, name, err)
	}

	return nil
}
