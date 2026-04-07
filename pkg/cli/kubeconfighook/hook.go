package kubeconfighook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	omniprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// expiryBuffer is subtracted from the token's exp claim so that refresh
	// happens a short while before the token actually expires.
	expiryBuffer = 5 * time.Minute

	// kubeconfigFileMode is the file mode for kubeconfig files.
	kubeconfigFileMode = 0o600

	// jwtParts is the expected number of dot-separated segments in a JWT
	// (header.payload.signature). Requiring exactly 3 prevents non-JWT
	// bearer tokens that happen to contain a dot from being parsed.
	jwtParts = 3
)

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
	cfg, distCfg, canonicalPath := resolveOmniKubeconfigPath(cmd)
	if cfg == nil {
		return
	}

	if !IsTokenExpired(canonicalPath) {
		return
	}

	clusterName := resolveClusterName(distCfg, canonicalPath)
	if clusterName == "" {
		return
	}

	refreshErr := refreshKubeconfig(
		cmd.Context(),
		cfg.Spec.Cluster.Omni,
		clusterName,
		canonicalPath,
	)
	if refreshErr != nil {
		notify.Warningf(cmd.OutOrStderr(), "failed to refresh Omni kubeconfig: %v", refreshErr)
	}
}

// resolveOmniKubeconfigPath loads the KSail config, verifies the provider is
// Omni, and returns the canonicalized kubeconfig path together with the
// cluster config and distribution config. Returns nils when a refresh is not
// applicable (non-Omni provider, missing config, missing kubeconfig file).
func resolveOmniKubeconfigPath(
	cmd *cobra.Command,
) (*v1alpha1.Cluster, *clusterprovisioner.DistributionConfig, string) {
	cfgManager := kubeconfig.NewSilentConfigManager(cmd)

	cfg, loadErr := cfgManager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
	if loadErr != nil || cfg == nil || !cfgManager.IsConfigFileFound() {
		return nil, nil, ""
	}

	if cfg.Spec.Cluster.Provider != v1alpha1.ProviderOmni {
		return nil, nil, ""
	}

	kubeconfigPath, pathErr := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	if pathErr != nil {
		return nil, nil, ""
	}

	canonicalPath, canonErr := fsutil.EvalCanonicalPath(kubeconfigPath)
	if canonErr != nil {
		return nil, nil, ""
	}

	_, statErr := os.Stat(canonicalPath)
	if os.IsNotExist(statErr) {
		return nil, nil, ""
	}

	return cfg, cfgManager.DistributionConfig, canonicalPath
}

// resolveClusterName determines the Omni cluster name from available sources.
// Priority: distribution config → kubeconfig current context.
func resolveClusterName(
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
// The write is atomic (temp file + rename) to prevent corruption if the
// process is interrupted.
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

	writeErr := atomicWriteFile(kubeconfigPath, data, kubeconfigFileMode)
	if writeErr != nil {
		return fmt.Errorf("write kubeconfig: %w", writeErr)
	}

	return nil
}

// atomicWriteFile writes data to a temp file in the same directory and
// renames it to the target path, ensuring an all-or-nothing write.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".kubeconfig-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() {
		// Clean up temp file on any failure path.
		_ = os.Remove(tmpPath)
	}()

	chmodErr := os.Chmod(tmpPath, perm)
	if chmodErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("set permissions: %w", chmodErr)
	}

	bytesWritten, writeErr := tmp.Write(data)
	if writeErr != nil {
		_ = tmp.Close()

		return fmt.Errorf("write data: %w", writeErr)
	}

	if bytesWritten != len(data) {
		_ = tmp.Close()

		return fmt.Errorf(
			"write data: %w: wrote %d of %d bytes",
			errShortWrite,
			bytesWritten,
			len(data),
		)
	}

	closeErr := tmp.Close()
	if closeErr != nil {
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	renameErr := os.Rename(tmpPath, path)
	if renameErr != nil {
		// On Windows, os.Rename may fail when the destination exists.
		// Mirror the pattern in pkg/cli/cmd/cluster/cluster.go: remove
		// the target and retry.
		_, statErr := os.Stat(path)
		if statErr == nil {
			_ = os.Remove(path)

			renameErr = os.Rename(tmpPath, path)
		}
	}

	if renameErr != nil {
		return fmt.Errorf("rename temp file: %w", renameErr)
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

	currentCtx, contextExists := cfg.Contexts[cfg.CurrentContext]
	if !contextExists {
		return false
	}

	authInfo, authExists := cfg.AuthInfos[currentCtx.AuthInfo]
	if !authExists || authInfo.Token == "" {
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
	parts := strings.Split(token, ".")
	if len(parts) != jwtParts {
		return time.Time{}, errNotJWT
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}

	unmarshalErr := json.Unmarshal(payload, &claims)
	if unmarshalErr != nil {
		return time.Time{}, fmt.Errorf("unmarshal JWT claims: %w", unmarshalErr)
	}

	if claims.Exp == 0 {
		return time.Time{}, errNoExpClaim
	}

	return time.Unix(claims.Exp, 0), nil
}
