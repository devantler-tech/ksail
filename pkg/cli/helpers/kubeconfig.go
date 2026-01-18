package helpers

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetDefaultKubeconfigPath returns the default kubeconfig path for the current user.
// The path is constructed as ~/.kube/config using the user's home directory.
func GetDefaultKubeconfigPath() string {
	homeDir, _ := os.UserHomeDir()

	return filepath.Join(homeDir, ".kube", "config")
}

// GetKubeconfigRESTConfig loads the kubeconfig and returns a REST config for Kubernetes clients.
// This is used by both kubernetes.Clientset and dynamic.Client creation.
func GetKubeconfigRESTConfig() (*rest.Config, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	return config, nil
}

// GetKubeconfigPathFromConfig extracts and expands the kubeconfig path from a loaded cluster config.
// If the config doesn't specify a kubeconfig path, it returns the default path from GetDefaultKubeconfigPath.
//
// The function always expands tilde (~) characters in the path to the user's home directory,
// regardless of whether the path came from the config or is the default.
//
// Returns an error if path expansion fails.
func GetKubeconfigPathFromConfig(cfg *v1alpha1.Cluster) (string, error) {
	kubeconfigPath := cfg.Spec.Cluster.Connection.Kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = GetDefaultKubeconfigPath()
	}

	// Always expand tilde in kubeconfig path, regardless of source
	expandedPath, err := iopath.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to expand home path: %w", err)
	}

	return expandedPath, nil
}

// GetKubeconfigPathSilently attempts to load the KSail config and extract the kubeconfig path
// without producing any output. All config loading output is suppressed using io.Discard.
//
// If config loading fails for any reason, this function returns the default kubeconfig path
// rather than propagating the error. This makes it suitable for scenarios where a best-effort
// path is acceptable.
func GetKubeconfigPathSilently() string {
	// Use io.Discard to suppress all output
	cfgManager := ksailconfigmanager.NewConfigManager(io.Discard)

	kubeconfigPath, err := getKubeconfigPath(cfgManager)
	if err != nil {
		// If we can't load config, use default kubeconfig
		return GetDefaultKubeconfigPath()
	}

	return kubeconfigPath
}

// getKubeconfigPath loads the KSail configuration using the provided manager
// and extracts the kubeconfig path from the loaded cluster configuration.
//
// This is an internal helper function used by GetKubeconfigPathSilently.
// It creates a minimal timer for config loading and delegates to GetKubeconfigPathFromConfig
// for path extraction and expansion.
func getKubeconfigPath(cfgManager *ksailconfigmanager.ConfigManager) (string, error) {
	// Create a minimal timer for config loading
	tmr := timer.New()
	tmr.Start()

	clusterCfg, err := cfgManager.Load(configmanager.LoadOptions{Timer: tmr})
	if err != nil {
		return "", fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	return GetKubeconfigPathFromConfig(clusterCfg)
}
