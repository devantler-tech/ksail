package k8s

import (
	"fmt"
	"io"
	"os"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// kubeconfigFileMode is the file mode for kubeconfig files.
const kubeconfigFileMode = 0o600

// CleanupKubeconfig removes the cluster, context, and user entries for a cluster
// from the kubeconfig file. This only removes entries matching the provided names,
// leaving other cluster configurations intact.
//
// Parameters:
//   - kubeconfigPath: absolute path to the kubeconfig file
//   - clusterName: the cluster entry name to remove
//   - contextName: the context entry name to remove
//   - userName: the user/authinfo entry name to remove
//   - logWriter: writer for log output (can be io.Discard)
func CleanupKubeconfig(
	kubeconfigPath string,
	clusterName string,
	contextName string,
	userName string,
	logWriter io.Writer,
) error {
	// Check if kubeconfig file exists
	_, statErr := os.Stat(kubeconfigPath)
	if os.IsNotExist(statErr) {
		// No kubeconfig to clean up
		return nil
	}

	return removeEntriesFromKubeconfig(
		kubeconfigPath,
		clusterName,
		contextName,
		userName,
		logWriter,
	)
}

// removeEntriesFromKubeconfig loads the kubeconfig, removes the specified entries, and saves it.
//
//nolint:gosec // G304: kubeconfigPath is validated by caller
func removeEntriesFromKubeconfig(
	kubeconfigPath string,
	clusterName string,
	contextName string,
	userName string,
	logWriter io.Writer,
) error {
	kubeconfigBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Check if any entries exist to remove
	if !hasKubeconfigEntriesToCleanup(kubeConfig, contextName, clusterName, userName) {
		return nil
	}

	delete(kubeConfig.Contexts, contextName)
	delete(kubeConfig.Clusters, clusterName)
	delete(kubeConfig.AuthInfos, userName)

	if kubeConfig.CurrentContext == contextName {
		kubeConfig.CurrentContext = ""
	}

	_, _ = fmt.Fprintf(logWriter, "Cleaned up kubeconfig entries for cluster %q\n", clusterName)

	// Serialize and write back
	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = os.WriteFile(kubeconfigPath, result, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return nil
}

// RenameKubeconfigContext renames the current context (and its associated cluster and
// user entries) in raw kubeconfig bytes to desiredContext.
//
// If desiredContext is empty or already matches the current context, the kubeconfig is
// returned as-is. Returns an error when CurrentContext is empty and no single context
// entry can be unambiguously selected.
func RenameKubeconfigContext(kubeconfigData []byte, desiredContext string) ([]byte, error) {
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	oldContext := config.CurrentContext

	// When CurrentContext is empty, try to pick the sole context entry.
	if oldContext == "" {
		switch len(config.Contexts) {
		case 0:
			// No contexts at all — just set CurrentContext and return.
			if desiredContext != "" {
				config.CurrentContext = desiredContext
			}

			return clientcmd.Write(*config)
		case 1:
			for name := range config.Contexts {
				oldContext = name
			}
		default:
			return nil, fmt.Errorf(
				"kubeconfig has no current context and %d context entries; cannot determine which to rename",
				len(config.Contexts),
			)
		}
	}

	if oldContext == desiredContext {
		return clientcmd.Write(*config)
	}

	ctxEntry, exists := config.Contexts[oldContext]
	if !exists {
		return nil, fmt.Errorf("current context %q not found in kubeconfig", oldContext)
	}

	// Rename context entry
	delete(config.Contexts, oldContext)
	config.Contexts[desiredContext] = ctxEntry

	// Rename cluster reference only when its name matches the old context name
	// and the desired key does not already exist (avoids clobbering shared entries).
	renameKubeconfigClusterRef(config, ctxEntry, oldContext, desiredContext)

	// Rename user/authinfo reference under the same conditions.
	renameKubeconfigAuthInfoRef(config, ctxEntry, oldContext, desiredContext)

	config.CurrentContext = desiredContext

	result, err := clientcmd.Write(*config)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	return result, nil
}

// renameKubeconfigClusterRef renames the cluster entry referenced by the context
// when its name matches oldContext and desiredContext is not already taken.
func renameKubeconfigClusterRef(
	config *api.Config,
	ctxEntry *api.Context,
	oldContext, desiredContext string,
) {
	oldCluster := ctxEntry.Cluster
	if oldCluster == "" || oldCluster != oldContext {
		return
	}

	if _, collision := config.Clusters[desiredContext]; collision {
		return
	}

	if clusterEntry, ok := config.Clusters[oldCluster]; ok {
		delete(config.Clusters, oldCluster)
		config.Clusters[desiredContext] = clusterEntry
		ctxEntry.Cluster = desiredContext
	}
}

// renameKubeconfigAuthInfoRef renames the authinfo/user entry referenced by the
// context when its name matches oldContext and desiredContext is not already taken.
func renameKubeconfigAuthInfoRef(
	config *api.Config,
	ctxEntry *api.Context,
	oldContext, desiredContext string,
) {
	oldUser := ctxEntry.AuthInfo
	if oldUser == "" || oldUser != oldContext {
		return
	}

	if _, collision := config.AuthInfos[desiredContext]; collision {
		return
	}

	if authEntry, ok := config.AuthInfos[oldUser]; ok {
		delete(config.AuthInfos, oldUser)
		config.AuthInfos[desiredContext] = authEntry
		ctxEntry.AuthInfo = desiredContext
	}
}

// hasKubeconfigEntriesToCleanup checks if any kubeconfig entries exist for cleanup.
// Returns true if at least one of: context, cluster, user, or current-context needs removal.
func hasKubeconfigEntriesToCleanup(
	kubeConfig *api.Config,
	contextName string,
	clusterName string,
	userName string,
) bool {
	_, hasContext := kubeConfig.Contexts[contextName]
	_, hasCluster := kubeConfig.Clusters[clusterName]
	_, hasUser := kubeConfig.AuthInfos[userName]
	isCurrentContext := kubeConfig.CurrentContext == contextName

	return hasContext || hasCluster || hasUser || isCurrentContext
}
