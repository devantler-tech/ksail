package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

// resolveTarget resolves the kubeconfig path AND context for backup/restore.
//
//   - When nameFlag is empty (the existing behavior), it returns the config-derived
//     kubeconfig path via GetKubeconfigPathSilently and an empty context, so
//     backup/restore operate on the kubeconfig's current-context.
//   - When nameFlag is set, it resolves the targeted cluster's kubeconfig path AND
//     its actual kubeconfig context (via the same prefix-matching resolver `cluster
//     switch` uses), so the operation targets that specific cluster rather than
//     silently acting on whatever current-context happens to be — critical for
//     restore, which would otherwise write into the wrong cluster.
//
// A missing path is reported as ErrKubeconfigNotFound, and a name that matches no
// (or multiple) contexts as ErrContextNotFound/ErrAmbiguousCluster, so callers
// fail fast rather than backing up or restoring the wrong (or no) cluster.
func resolveTarget(cmd *cobra.Command, nameFlag string) (string, string, error) {
	if nameFlag == "" {
		path := kubeconfig.GetKubeconfigPathSilently(cmd)
		if path == "" {
			return "", "", ErrKubeconfigNotFound
		}

		return path, "", nil
	}

	resolved, err := lifecycle.ResolveClusterInfo(cmd, nameFlag, "", "")
	if err != nil {
		return "", "", fmt.Errorf("resolve cluster info: %w", err)
	}

	if resolved.KubeconfigPath == "" {
		return "", "", ErrKubeconfigNotFound
	}

	config, err := clientcmd.LoadFromFile(resolved.KubeconfigPath)
	if err != nil {
		return "", "", fmt.Errorf("load kubeconfig %q: %w", resolved.KubeconfigPath, err)
	}

	kubeContext, err := resolveContextName(config, resolved.ClusterName)
	if err != nil {
		return "", "", err
	}

	return resolved.KubeconfigPath, kubeContext, nil
}
