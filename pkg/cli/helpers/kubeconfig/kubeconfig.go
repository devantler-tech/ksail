package kubeconfig

import (
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
)

// GetKubeconfigPathFromConfig extracts and expands the kubeconfig path from a loaded cluster config.
// If the config doesn't specify a kubeconfig path, it returns the default path.
//
// The function always expands tilde (~) characters in the path to the user's home directory,
// regardless of whether the path came from the config or is the default.
//
// Returns an error if path expansion fails.
func GetKubeconfigPathFromConfig(cfg *v1alpha1.Cluster) (string, error) {
	kubeconfigPath := cfg.Spec.Cluster.Connection.Kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = k8s.DefaultKubeconfigPath()
	}

	// Always expand tilde in kubeconfig path, regardless of source
	expandedPath, err := fsutil.ExpandHomePath(kubeconfigPath)
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
		return k8s.DefaultKubeconfigPath()
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
