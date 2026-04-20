package kubeconfighook

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	omniprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// expiryBuffer is subtracted from the token's exp claim so that refresh
	// happens a short while before the token actually expires.
	expiryBuffer = 5 * time.Minute

	// kubeconfigFileMode is the file mode for kubeconfig files.
	kubeconfigFileMode = 0o600

	// kubeconfigDirMode is the directory mode used when creating the parent
	// directory for the kubeconfig on initial fetch (e.g. fresh CI runners).
	kubeconfigDirMode = 0o700

	// jwtParts is the expected number of dot-separated segments in a JWT
	// (header.payload.signature). Requiring exactly 3 prevents non-JWT
	// bearer tokens that happen to contain a dot from being parsed.
	jwtParts = 3

	// staleCheckTimeout is the timeout for the lightweight API health check
	// used to detect stale kubeconfigs (e.g., after cluster recreation).
	staleCheckTimeout = 3 * time.Second
)

// MaybeRefreshOmniKubeconfig checks whether the current kubeconfig's service-account
// token is expired or stale for Omni-managed clusters and transparently refreshes it.
// It also performs an initial fetch when the kubeconfig file does not yet exist
// (e.g. on a fresh CI runner) so downstream Helm/Flux/GitOps operations have a
// kubeconfig to work against.
//
// This function is designed to be called from Cobra PersistentPreRunE hooks.
// It is a fast no-op when:
//   - No KSail config is found or the provider is not Omni
//   - The user passed --kubeconfig explicitly (they manage it themselves)
//
// When the kubeconfig file exists, this function first checks JWT token expiry
// (~1ms, local-only). If the token is still valid, a lightweight API server
// probe (up to staleCheckTimeout) detects credentials that are structurally
// valid but rejected by a recreated cluster.
//
// Refresh is triggered when:
//   - The kubeconfig file does not yet exist (initial fetch)
//   - The JWT token in the kubeconfig is expired or about to expire
//   - The kubeconfig credentials are rejected by the API server (e.g., after
//     cluster recreation with the same name)
//
// On refresh failure, a warning is logged but the error is not propagated —
// the command proceeds with whatever (if any) kubeconfig is on disk.
func MaybeRefreshOmniKubeconfig(cmd *cobra.Command) {
	cfg, distCfg, canonicalPath := resolveOmniKubeconfigPath(cmd)
	if cfg == nil {
		return
	}

	fileExists, shouldRefresh := evaluateKubeconfigRefresh(
		canonicalPath,
		cfg.Spec.Cluster.Connection.Context,
	)
	if !shouldRefresh {
		return
	}

	performOmniKubeconfigRefresh(cmd, cfg, distCfg, canonicalPath, fileExists)
}

// evaluateKubeconfigRefresh determines whether the Omni kubeconfig at
// canonicalPath needs to be (re)fetched. It returns whether the file currently
// exists and whether a refresh should be triggered.
func evaluateKubeconfigRefresh(canonicalPath, kubeconfigContext string) (bool, bool) {
	_, statErr := os.Stat(canonicalPath)

	switch {
	case statErr == nil:
		if IsTokenExpired(canonicalPath, kubeconfigContext) {
			return true, true
		}

		// When the token is not expired, check if the credentials are still
		// accepted by the API server. After cluster recreation the token may
		// be structurally valid (not expired) but rejected by the new cluster.
		return true, IsKubeconfigStale(canonicalPath, kubeconfigContext)
	case os.IsNotExist(statErr):
		// Fresh runner: no kubeconfig yet. Fetch it from Omni so downstream
		// operations (component detection, Helm, Flux) can connect.
		return false, true
	default:
		// Some other stat error (e.g. permission). Leave it alone — a later
		// step will surface the real error.
		return false, false
	}
}

// performOmniKubeconfigRefresh runs the actual Omni refresh/fetch and reports
// progress via cmd's output. It is only invoked after evaluateKubeconfigRefresh
// has decided a refresh is warranted.
func performOmniKubeconfigRefresh(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
	canonicalPath string,
	fileExists bool,
) {
	clusterName := resolveClusterName(distCfg, canonicalPath)
	if clusterName == "" {
		if !fileExists {
			notify.Warningf(cmd.OutOrStderr(),
				"cannot auto-fetch Omni kubeconfig: unable to determine cluster name from config")
		}

		return
	}

	// Determine the desired kubeconfig context name.
	// If explicitly configured, use that; otherwise derive from the distribution convention.
	desiredContext := cfg.Spec.Cluster.Connection.Context
	if desiredContext == "" {
		desiredContext = cfg.Spec.Cluster.Distribution.ContextName(clusterName)
	}

	if !fileExists {
		mkErr := os.MkdirAll(filepath.Dir(canonicalPath), kubeconfigDirMode)
		if mkErr != nil {
			notify.Warningf(cmd.OutOrStderr(),
				"failed to create kubeconfig directory: %v", mkErr)

			return
		}
	}

	refreshErr := refreshKubeconfig(
		cmd.Context(),
		cfg.Spec.Provider.Omni,
		clusterName,
		desiredContext,
		canonicalPath,
	)
	if refreshErr != nil {
		notify.Warningf(cmd.OutOrStderr(), "failed to refresh Omni kubeconfig: %v", refreshErr)

		return
	}

	if !fileExists {
		notify.Activityf(cmd.OutOrStderr(),
			"fetched Omni kubeconfig to %s", canonicalPath)
	}
}

// resolveOmniKubeconfigPath loads the KSail config, verifies the provider is
// Omni, and returns the canonicalized kubeconfig path together with the
// cluster config and distribution config.
//
// Returns nils when a refresh/fetch is not applicable: non-Omni provider,
// missing config, or path resolution failure. Does NOT skip when the
// kubeconfig file is absent — callers decide whether to trigger an initial
// fetch in that case.
func resolveOmniKubeconfigPath(
	cmd *cobra.Command,
) (*v1alpha1.Cluster, *clusterprovisioner.DistributionConfig, string) {
	// If the user explicitly passed --kubeconfig, they are managing the
	// kubeconfig themselves — skip auto-refresh.
	if isKubeconfigFlagExplicit(cmd) {
		return nil, nil, ""
	}

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

	// Canonicalize for containment-safety, but fall back to filepath.Abs only
	// when the parent directory does not exist yet (e.g. on a fresh CI runner
	// where ~/.kube/ is still missing). EvalCanonicalPath requires the parent
	// to exist, and without this fallback the initial-fetch branch would be
	// silently skipped — the exact regression #4112 is about. For any other
	// error (permission denied, I/O error, etc.) we bail out rather than
	// bypass canonicalization on a non-canonical path.
	canonicalPath, canonErr := fsutil.EvalCanonicalPath(kubeconfigPath)
	if canonErr != nil {
		if !errors.Is(canonErr, fs.ErrNotExist) {
			return nil, nil, ""
		}

		absPath, absErr := filepath.Abs(kubeconfigPath)
		if absErr != nil {
			return nil, nil, ""
		}

		canonicalPath = absPath
	}

	return cfg, cfgManager.DistributionConfig, canonicalPath
}

// isKubeconfigFlagExplicit returns true when the command has a --kubeconfig
// flag and the user explicitly set it on the command line.
func isKubeconfigFlagExplicit(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	kubeconfigFlag := cmd.Flag("kubeconfig")

	return kubeconfigFlag != nil && kubeconfigFlag.Changed
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

// clusterNameFromKubeconfig extracts the cluster name from the kubeconfig file.
// Only returns a cluster name when the current context follows the Talos "admin@<name>"
// convention. Returns empty for arbitrary/renamed context names since those cannot
// reliably map to Omni cluster names.
func clusterNameFromKubeconfig(kubeconfigPath string) string {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return ""
	}

	if after, ok := strings.CutPrefix(cfg.CurrentContext, "admin@"); ok {
		return after
	}

	return ""
}

// refreshKubeconfig creates an Omni client and fetches a fresh kubeconfig.
// The Omni-generated context is renamed to desiredContext so the kubeconfig
// matches spec.cluster.connection.context. The write is atomic (temp file +
// rename) to prevent corruption if the process is interrupted.
func refreshKubeconfig(
	ctx context.Context,
	omniOpts v1alpha1.OptionsOmni,
	clusterName string,
	desiredContext string,
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

	// Rename the Omni-generated context to match the configured context name
	if desiredContext != "" {
		data, err = k8s.RenameKubeconfigContext(data, desiredContext)
		if err != nil {
			return fmt.Errorf("rename kubeconfig context: %w", err)
		}
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
	if renameErr != nil && runtime.GOOS == "windows" {
		// On Windows, os.Rename may fail when the destination already
		// exists. Remove the target and retry only on Windows to avoid
		// accidentally deleting a valid kubeconfig on Unix where rename
		// is atomic and failures indicate a different problem.
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

// IsKubeconfigStale performs a lightweight API server health check to detect
// kubeconfigs with valid (non-expired) tokens that are rejected by the server.
// This happens when a cluster is recreated with the same name — the old token
// is structurally valid but the new cluster does not accept it.
//
// Returns true when:
//   - The kubeconfig cannot be loaded or the configured context is missing
//     (subsequent K8s operations would fail anyway, so a refresh is warranted)
//   - The API server explicitly rejects the credentials (HTTP 401/403)
//
// Returns false when:
//   - The API server responds successfully (credentials are valid)
//   - A non-auth error occurs (connection refused, timeout, TLS errors) —
//     the cluster is simply unreachable, not necessarily using stale credentials
func IsKubeconfigStale(kubeconfigPath, kubeconfigContext string) bool {
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, kubeconfigContext)
	if err != nil {
		// The kubeconfig cannot be loaded or the configured context is
		// missing. A refresh is warranted since downstream operations
		// using this kubeconfig would fail.
		return true
	}

	restConfig.Timeout = staleCheckTimeout

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		// The REST config is unusable (e.g., missing cert/key files,
		// invalid host). A refresh is warranted since downstream
		// operations would fail with the same config.
		return true
	}

	_, err = clientset.Discovery().ServerVersion()
	if err == nil {
		return false
	}

	return apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err)
}

// IsTokenExpired checks whether the bearer token in the kubeconfig's specified
// context has expired (or will expire within the expiryBuffer).
//
// When kubeconfigContext is non-empty, the token for that context is checked.
// When empty, the kubeconfig's CurrentContext is used.
//
// Returns false (not expired) when the kubeconfig cannot be parsed, has no
// token, or the token is not a JWT — erring on the side of not refreshing
// unnecessarily.
func IsTokenExpired(kubeconfigPath, kubeconfigContext string) bool {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return false
	}

	contextName := kubeconfigContext
	if contextName == "" {
		contextName = cfg.CurrentContext
	}

	currentCtx, contextExists := cfg.Contexts[contextName]
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
