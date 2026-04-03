package fluxinstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v5/pkg/client/flux"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/sopsutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const (
	// SopsAgeSecretName is the name of the Kubernetes secret used for SOPS Age decryption.
	SopsAgeSecretName = "sops-age"
	// sopsAgeKeyField is the data key within the secret that holds the Age private key.
	sopsAgeKeyField = "sops.agekey"
)

// ensureSopsAgeSecret creates or updates the sops-age secret in flux-system namespace
// if SOPS is enabled and an Age key is available.
func ensureSopsAgeSecret(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	ageKey, err := sopsutil.ResolveEnabledAgeKey(
		clusterCfg.Spec.Cluster.SOPS,
	)
	if err != nil {
		return fmt.Errorf("resolve SOPS Age key: %w", err)
	}

	if ageKey == "" {
		return nil
	}

	secret := buildSopsAgeSecret(ageKey)

	k8sClient, err := newCoreV1Client(restConfig)
	if err != nil {
		return err
	}

	return upsertSecret(ctx, k8sClient, secret)
}

// buildSopsAgeSecret creates the Secret object for SOPS Age decryption.
func buildSopsAgeSecret(ageKey string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SopsAgeSecretName,
			Namespace: fluxclient.DefaultNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "ksail",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			sopsAgeKeyField: []byte(ageKey),
		},
	}
}
