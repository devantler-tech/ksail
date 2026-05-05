package k8s

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// kubeconfigFileMode is the file mode for kubeconfig files.
const kubeconfigFileMode = 0o600

// kubeconfigDirMode is the directory mode for kubeconfig parent directories.
const kubeconfigDirMode = 0o700

// MergeKubeconfig merges the cluster, context, and user entries from newKubeconfigData
// into the existing kubeconfig file at kubeconfigPath. If the file does not exist, it
// creates it with the new entries. Existing entries with the same keys are overwritten
// by the new data. The current context is set to the new config's current context if
// it is non-empty.
//
// MergeKubeconfig creates the parent directory if it does not exist and canonicalizes
// the path (resolving symlinks) before reading or writing to prevent symlink-escape
// attacks.
//
// This prevents data loss when multiple clusters share the same kubeconfig file.
func MergeKubeconfig(kubeconfigPath string, newKubeconfigData []byte) error {
	newConfig, err := clientcmd.Load(newKubeconfigData)
	if err != nil {
		return fmt.Errorf("failed to parse new kubeconfig: %w", err)
	}

	// Ensure parent directory exists before canonicalization (EvalCanonicalPath
	// requires the parent to exist).
	kubeconfigDir := filepath.Dir(kubeconfigPath)
	if kubeconfigDir != "" && kubeconfigDir != "." {
		mkdirErr := os.MkdirAll(kubeconfigDir, kubeconfigDirMode)
		if mkdirErr != nil {
			return fmt.Errorf("failed to create kubeconfig directory: %w", mkdirErr)
		}
	}

	// Canonicalize the path to prevent writes through symlinks.
	kubeconfigPath, err = fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to canonicalize kubeconfig path: %w", err)
	}

	existing, err := loadOrCreateKubeconfig(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load existing kubeconfig: %w", err)
	}

	mergeKubeconfigEntries(existing, newConfig)

	result, err := clientcmd.Write(*existing)
	if err != nil {
		return fmt.Errorf("failed to serialize merged kubeconfig: %w", err)
	}

	err = fsutil.AtomicWriteFile(kubeconfigPath, result, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write merged kubeconfig: %w", err)
	}

	return nil
}

// mergeKubeconfigEntries copies clusters, contexts, and users from src into dst,
// overwriting entries with the same keys. It also updates the current context.
func mergeKubeconfigEntries(dst, src *api.Config) {
	if dst.Clusters == nil {
		dst.Clusters = make(map[string]*api.Cluster)
	}

	maps.Copy(dst.Clusters, src.Clusters)

	if dst.Contexts == nil {
		dst.Contexts = make(map[string]*api.Context)
	}

	maps.Copy(dst.Contexts, src.Contexts)

	if dst.AuthInfos == nil {
		dst.AuthInfos = make(map[string]*api.AuthInfo)
	}

	maps.Copy(dst.AuthInfos, src.AuthInfos)

	if src.CurrentContext != "" {
		dst.CurrentContext = src.CurrentContext
	}
}

// loadOrCreateKubeconfig loads the kubeconfig from disk, or returns an empty
// config if the file does not exist.
//
//nolint:gosec // G304: kubeconfigPath is validated by caller
func loadOrCreateKubeconfig(kubeconfigPath string) (*api.Config, error) {
	data, err := os.ReadFile(kubeconfigPath)
	if os.IsNotExist(err) {
		return api.NewConfig(), nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	return config, nil
}

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

	logIdentifier := clusterName
	if logIdentifier == "" {
		logIdentifier = contextName
	}

	_, _ = fmt.Fprintf(logWriter, "Cleaned up kubeconfig entries for cluster %q\n", logIdentifier)

	// Serialize and write back
	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = fsutil.AtomicWriteFile(kubeconfigPath, result, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return nil
}

// RenameKubeconfigContext renames the current context (and its associated cluster and
// user entries) in raw kubeconfig bytes to desiredContext.
//
// If desiredContext is empty, the kubeconfig is returned unchanged. If desiredContext
// already matches the current context, the kubeconfig is returned unchanged. If there
// is no current context but the kubeconfig can be resolved without ambiguity, the
// kubeconfig is updated to set CurrentContext to desiredContext. Returns an error when
// the desired context name collides with an existing different context entry, or when
// CurrentContext is empty and no single context entry can be unambiguously selected.
func RenameKubeconfigContext(kubeconfigData []byte, desiredContext string) ([]byte, error) {
	if desiredContext == "" {
		return kubeconfigData, nil
	}

	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	oldContext, err := resolveOldContext(config)
	if err != nil {
		return nil, err
	}

	if oldContext == "" {
		if len(config.Contexts) == 0 {
			return nil, fmt.Errorf("%w: no contexts in kubeconfig", ErrKubeconfigContextNotFound)
		}

		config.CurrentContext = desiredContext

		return writeConfig(config)
	}

	if oldContext == desiredContext {
		config.CurrentContext = desiredContext

		return writeConfig(config)
	}

	ctxEntry, exists := config.Contexts[oldContext]
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrKubeconfigContextNotFound, oldContext)
	}

	if _, collision := config.Contexts[desiredContext]; collision {
		return nil, fmt.Errorf(
			"%w: %q already exists in kubeconfig", ErrKubeconfigContextCollision, desiredContext,
		)
	}

	// Rename context entry
	delete(config.Contexts, oldContext)
	config.Contexts[desiredContext] = ctxEntry

	// Rename cluster and user references when they match the old context name
	renameKubeconfigClusterRef(config, ctxEntry, oldContext, desiredContext)
	renameKubeconfigAuthInfoRef(config, ctxEntry, oldContext, desiredContext)

	config.CurrentContext = desiredContext

	return writeConfig(config)
}

// resolveOldContext determines the context to rename. If CurrentContext is set,
// it is returned. If empty, the sole context entry is selected; multiple entries
// produce an error.
func resolveOldContext(config *api.Config) (string, error) {
	if config.CurrentContext != "" {
		return config.CurrentContext, nil
	}

	switch len(config.Contexts) {
	case 0:
		return "", nil
	case 1:
		for name := range config.Contexts {
			return name, nil
		}
	}

	return "", fmt.Errorf(
		"%w and %d context entries; cannot determine which to rename",
		ErrKubeconfigNoCurrentContext, len(config.Contexts),
	)
}

// writeConfig serializes a kubeconfig, wrapping the error.
func writeConfig(config *api.Config) ([]byte, error) {
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

// OIDCExecConfig holds the parameters for configuring an OIDC exec credential plugin in kubeconfig.
type OIDCExecConfig struct {
	// KubeconfigPath is the absolute path to the kubeconfig file.
	KubeconfigPath string
	// ClusterEntryName is the kubeconfig cluster entry name (e.g. "kind-local", "k3d-local").
	// This is used as the context.cluster reference.
	ClusterEntryName string
	// DisplayName is the user-friendly cluster name (e.g. "local") used for
	// naming the OIDC user ("oidc-local") and context ("oidc@local").
	DisplayName string
	// IssuerURL is the OIDC provider issuer URL.
	IssuerURL string
	// ClientID is the OIDC client ID.
	ClientID string
	// ExtraScopes are additional OIDC scopes to request.
	ExtraScopes []string
	// CAFile is an optional path to the OIDC provider's CA certificate.
	CAFile string
}

// AddOIDCKubeconfigEntries adds an exec-based OIDC user and context to the kubeconfig.
// The user is named "oidc-<clusterName>" and the context is named "oidc@<clusterName>".
// The admin context remains the current context.
func AddOIDCKubeconfigEntries(cfg *OIDCExecConfig, logWriter io.Writer) error {
	canonicalPath, err := fsutil.EvalCanonicalPath(cfg.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	kubeconfigBytes, err := os.ReadFile(canonicalPath) //nolint:gosec // canonicalized above
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	userName := "oidc-" + cfg.DisplayName
	contextName := "oidc@" + cfg.DisplayName

	execArgs, err := buildOIDCExecArgs(cfg)
	if err != nil {
		return err
	}

	kubeConfig.AuthInfos[userName] = &api.AuthInfo{
		Exec: &api.ExecConfig{
			APIVersion:      "client.authentication.k8s.io/v1",
			Command:         "ksail",
			Args:            execArgs,
			InteractiveMode: api.IfAvailableExecInteractiveMode,
		},
	}

	kubeConfig.Contexts[contextName] = &api.Context{
		Cluster:  cfg.ClusterEntryName,
		AuthInfo: userName,
	}

	_, _ = fmt.Fprintf(logWriter, "Added OIDC context %q to kubeconfig\n", contextName)

	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = fsutil.AtomicWriteFile(canonicalPath, result, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return nil
}

func buildOIDCExecArgs(cfg *OIDCExecConfig) ([]string, error) {
	args := []string{
		"oidc", "get-token",
		"--issuer-url=" + cfg.IssuerURL,
		"--client-id=" + cfg.ClientID,
	}

	for _, scope := range cfg.ExtraScopes {
		args = append(args, "--extra-scope="+scope)
	}

	if cfg.CAFile != "" {
		canonicalCAFile, err := fsutil.EvalCanonicalPath(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve OIDC CA file path: %w", err)
		}

		args = append(args, "--ca-file="+canonicalCAFile)
	}

	return args, nil
}

// CleanupOIDCKubeconfigEntries removes the OIDC user and context entries for a cluster.
// The displayName is the user-friendly cluster name (e.g. "local") used in OIDC naming.
func CleanupOIDCKubeconfigEntries(kubeconfigPath, displayName string, logWriter io.Writer) error {
	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	userName := "oidc-" + displayName
	contextName := "oidc@" + displayName

	// Pass empty clusterName because OIDC cleanup only removes the user and
	// context entries — the shared cluster entry is owned by the admin context.
	// Use contextName as the log identifier since clusterName is intentionally empty.
	return CleanupKubeconfig(canonicalPath, "", contextName, userName, logWriter)
}
