package k8s

import (
	"fmt"
	"io"
	"os"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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

// hasKubeconfigEntriesToCleanup checks if any kubeconfig entries exist for cleanup.
// Returns true if at least one of: context, cluster, user, or current-context needs removal.
func hasKubeconfigEntriesToCleanup(
	kubeConfig *clientcmdapi.Config,
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
