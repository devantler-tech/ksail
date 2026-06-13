package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/spf13/cobra"
)

// resolveTargetKubeconfig resolves the kubeconfig path for backup/restore.
//
//   - When nameFlag is empty (the existing behavior), it returns the config-derived
//     kubeconfig path via GetKubeconfigPathSilently — backup/restore then operate on
//     the current kubeconfig context.
//   - When nameFlag is set, it resolves the targeted cluster through the shared
//     ResolveClusterInfo path (flag > config > kubeconfig context) and returns that
//     cluster's resolved kubeconfig path, so a user can target a specific cluster
//     without switching their current context.
//
// Either way an empty result is reported as ErrKubeconfigNotFound so callers
// fail fast rather than backing up the wrong (or no) cluster.
func resolveTargetKubeconfig(cmd *cobra.Command, nameFlag string) (string, error) {
	if nameFlag == "" {
		kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)
		if kubeconfigPath == "" {
			return "", ErrKubeconfigNotFound
		}

		return kubeconfigPath, nil
	}

	resolved, err := lifecycle.ResolveClusterInfo(cmd, nameFlag, "", "")
	if err != nil {
		return "", fmt.Errorf("resolve cluster info: %w", err)
	}

	if resolved.KubeconfigPath == "" {
		return "", ErrKubeconfigNotFound
	}

	return resolved.KubeconfigPath, nil
}
