package kubeconfighook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	omniprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

// expiryBuffer is subtracted from the token's exp claim so that refresh
// happens a short while before the token actually expires. This avoids
// race conditions where the token expires between the check and the
// actual API call.
const expiryBuffer = 5 * time.Minute

// MaybeRefreshOmniKubeconfig checks whether the current kubeconfig's service-account
// token is expired for Omni-managed clusters and transparently refreshes it.
//
// This function is designed to be called from Cobra PersistentPreRunE hooks.
// It is a fast no-op (~1ms) when:
//   - No KSail config is found or the provider is not Omni
//   - The kubeconfig file does not exist yet (e.g., before cluster create)
//   - The token is still valid
//
// On refresh failure, a warning is logged but the error is not propagated —
// the command proceeds with the existing kubeconfig.
func MaybeRefreshOmniKubeconfig(cmd *cobra.Command) {
	cfg, distCfg := loadConfigSilently(cmd)
	if cfg == nil || cfg.Spec.Cluster.Provider != v1alpha1.ProviderOmni {
		return
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	if err != nil {
		return
	}

	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return
	}

	if !IsTokenExpired(kubeconfigPath) {
		return
	}

	clusterName := resolveClusterName(cfg, distCfg, kubeconfigPath)
	if clusterName == "" {
		return
	}

	err = refreshKubeconfig(cmd.Context(), cfg.Spec.Cluster.Omni, clusterName, kubeconfigPath)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(), "failed to refresh Omni kubeconfig: %v", err)
	}
}

// loadConfigSilently loads the KSail config without producing output.
// Returns nil for both values if no config is found or loading fails.
func loadConfigSilently(cmd *cobra.Command) (*v1alpha1.Cluster, *clusterprovisioner.DistributionConfig) {
	var configFile string

	if cmd != nil {
		cfgPath, err := flags.GetConfigPath(cmd)
		if err == nil {
			configFile = cfgPath
		}
	}

	cfgManager := ksailconfigmanager.NewConfigManager(io.Discard, configFile)

	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
	if err != nil || cfg == nil || !cfgManager.IsConfigFileFound() {
		return nil, nil
	}

	return cfg, cfgManager.DistributionConfig
}

// resolveClusterName determines the Omni cluster name from available sources.
// Priority: distribution config → kubeconfig current context.
func resolveClusterName(
	_ *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
	kubeconfigPath string,
) string {
	if distCfg != nil {
		name := clusterNameFromDistConfig(distCfg)
		if name != "" {
			return name
		}
	}

	// Omni kubeconfig uses the cluster name as the context name.
	return clusterNameFromKubeconfig(kubeconfigPath)
}

// clusterNameFromDistConfig extracts the cluster name from distribution-specific config.
func clusterNameFromDistConfig(distCfg *clusterprovisioner.DistributionConfig) string {
	if distCfg == nil {
		return ""
	}

	if distCfg.Talos != nil {
		return distCfg.Talos.GetClusterName()
	}

	return ""
}

// clusterNameFromKubeconfig extracts the current context name from the kubeconfig file.
// For Omni, the context name matches the Omni cluster name.
func clusterNameFromKubeconfig(kubeconfigPath string) string {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return ""
	}

	return cfg.CurrentContext
}

// refreshKubeconfig creates an Omni client and fetches a fresh kubeconfig.
func refreshKubeconfig(
	ctx context.Context,
	omniOpts v1alpha1.OptionsOmni,
	clusterName string,
	kubeconfigPath string,
) error {
	prov, err := omniprovider.NewProviderFromOptions(omniOpts)
	if err != nil {
		return fmt.Errorf("create Omni provider: %w", err)
	}

	defer func() {
		if c := prov.Client(); c != nil {
			_ = c.Close()
		}
	}()

	data, err := prov.GetKubeconfig(ctx, clusterName, omniprovider.DefaultKubeconfigTTL)
	if err != nil {
		return fmt.Errorf("fetch kubeconfig: %w", err)
	}

	// kubeconfigPath is already expanded by GetKubeconfigPathFromConfig.
	//nolint:gosec // kubeconfig must be user-readable
	if err := os.WriteFile(kubeconfigPath, data, 0o600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}

	return nil
}

// IsTokenExpired checks whether the bearer token in the kubeconfig's current
// context has expired (or will expire within the expiryBuffer).
//
// Returns false (not expired) when the kubeconfig cannot be parsed, has no
// token, or the token is not a JWT — erring on the side of not refreshing
// unnecessarily.
func IsTokenExpired(kubeconfigPath string) bool {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return false
	}

	ctx, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return false
	}

	authInfo, ok := cfg.AuthInfos[ctx.AuthInfo]
	if !ok || authInfo.Token == "" {
		return false
	}

	expiry, err := jwtExpiry(authInfo.Token)
	if err != nil {
		return false
	}

	return time.Now().After(expiry.Add(-expiryBuffer))
}

// jwtExpiry extracts the "exp" claim from a JWT token without verifying the
// signature. This is safe because we only need the expiry time for a
// locally-stored kubeconfig — the token's authenticity is verified by the
// API server, not by this function.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return time.Time{}, errNotJWT
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("unmarshal JWT claims: %w", err)
	}

	if claims.Exp == 0 {
		return time.Time{}, errNoExpClaim
	}

	return time.Unix(claims.Exp, 0), nil
}
