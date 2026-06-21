package cluster

import (
	"fmt"
	"os"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

// configureOIDCKubeconfig adds OIDC exec credential plugin entries to the kubeconfig
// after cluster creation when OIDC authentication is configured.
func configureOIDCKubeconfig(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	displayName := lifecycle.ExtractClusterNameFromContext(
		clusterCfg.Spec.Cluster.Connection.Context,
		clusterCfg.Spec.Cluster.Distribution,
	)

	// Resolve the actual cluster entry name from the kubeconfig by looking up
	// the context. This is necessary because the context name and cluster entry
	// name differ for some distributions (e.g. Talos uses context "admin@<name>"
	// but cluster entry "<name>").
	contextName := clusterCfg.Spec.Cluster.Connection.Context

	clusterEntryName, resolveErr := resolveClusterEntryName(kubeconfigPath, contextName)
	if resolveErr != nil {
		// Fall back to using the context name directly (works for Kind, K3d, VCluster)
		clusterEntryName = contextName
	}

	oidcCfg := &clusterCfg.Spec.Cluster.OIDC

	err = k8s.AddOIDCKubeconfigEntries(&k8s.OIDCExecConfig{
		KubeconfigPath:   kubeconfigPath,
		ClusterEntryName: clusterEntryName,
		DisplayName:      displayName,
		IssuerURL:        oidcCfg.IssuerURL,
		ClientID:         oidcCfg.ClientID,
		ExtraScopes:      oidcCfg.ExtraScopes,
		CAFile:           oidcCfg.CAFile,
	}, cmd.OutOrStdout())
	if err != nil {
		return fmt.Errorf("failed to add OIDC kubeconfig entries: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "OIDC context 'oidc@%s' added to kubeconfig (use 'kubectl config use-context oidc@%s' to switch)",
		Args:    []any{displayName, displayName},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// resolveClusterEntryName reads the kubeconfig and returns the cluster entry
// name that the given context references. This handles distributions where
// the context name differs from the cluster entry name (e.g. Talos).
func resolveClusterEntryName(kubeconfigPath, contextName string) (string, error) {
	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	kubeconfigBytes, err := os.ReadFile(canonicalPath) //nolint:gosec // canonicalized above
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	ctxEntry, ok := kubeConfig.Contexts[contextName]
	if !ok || ctxEntry == nil {
		return "", fmt.Errorf("%w: %s", errContextNotFound, contextName)
	}

	return ctxEntry.Cluster, nil
}
