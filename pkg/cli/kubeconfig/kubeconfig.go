package kubeconfig

import (
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
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
// When cmd is non-nil, the --config persistent flag is honored so that an explicit
// config file is used instead of auto-discovery.
//
// This function intentionally avoids NewCommandConfigManager (which calls AddFlagsFromFields)
// to prevent "flag redefined" panics on commands that already define flags like --kubeconfig.
// Instead, it resolves the --config flag value directly and passes it to NewConfigManager.
//
// If config loading fails for any reason, this function returns the default kubeconfig path
// rather than propagating the error. This makes it suitable for scenarios where a best-effort
// path is acceptable.
func GetKubeconfigPathSilently(cmd *cobra.Command) string {
	cfgManager := NewSilentConfigManager(cmd)

	kubeconfigPath, err := getKubeconfigPath(cfgManager)
	if err != nil {
		// If we can't load config, use default kubeconfig
		return k8s.DefaultKubeconfigPath()
	}

	return kubeconfigPath
}

// NewSilentConfigManager creates a config manager from the command's --config
// flag with all output suppressed via [io.Discard].
//
// This is a shared helper used by both [GetKubeconfigPathSilently] and the
// kubeconfighook package to avoid duplicating flag-resolution boilerplate.
func NewSilentConfigManager(cmd *cobra.Command) *ksailconfigmanager.ConfigManager {
	var configFile string

	if cmd != nil {
		cfgPath, err := flags.GetConfigPath(cmd)
		if err == nil {
			configFile = cfgPath
		}
	}

	return ksailconfigmanager.NewConfigManager(io.Discard, configFile)
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
