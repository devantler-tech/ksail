package fluxinstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	"k8s.io/client-go/rest"
)

// SopsAgeSecretName is the name of the Kubernetes secret used for SOPS Age decryption.
const SopsAgeSecretName = sopsutil.SopsAgeSecretName

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

	secret := sopsutil.BuildSopsAgeSecret(fluxclient.DefaultNamespace, ageKey)

	k8sClient, err := newCoreV1Client(restConfig)
	if err != nil {
		return err
	}

	return upsertSecret(ctx, k8sClient, secret)
}
