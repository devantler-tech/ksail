package cluster

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	sigsyaml "sigs.k8s.io/yaml"
)

// ErrInvalidResourcePolicy is returned when an unsupported
// existing-resource-policy value is provided.
var ErrInvalidResourcePolicy = errors.New(
	"invalid existing-resource-policy: must be 'none' or 'update'",
)

const (
	// resourcePolicyNone skips resources that already exist in the cluster.
	resourcePolicyNone = "none"
	// resourcePolicyUpdate updates resources that already exist in the cluster.
	resourcePolicyUpdate = "update"
)

// ErrInvalidTarPath is returned when a tar entry contains a path
// traversal attempt.
var ErrInvalidTarPath = errors.New("invalid tar entry path")

// ErrSymlinkInArchive is returned when a tar archive contains
// symbolic or hard links, which are not supported.
var ErrSymlinkInArchive = errors.New(
	"symbolic and hard links are not supported in backup archives",
)

// ErrRestoreFailed is returned when one or more resources fail to restore.
var ErrRestoreFailed = errors.New("resource restore failed")

type restoreFlags struct {
	inputPath              string
	existingResourcePolicy string
	dryRun                 bool
}

// NewRestoreCmd creates the cluster restore command.
func NewRestoreCmd(_ *di.Runtime) *cobra.Command {
	flags := &restoreFlags{
		existingResourcePolicy: resourcePolicyNone,
	}

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore cluster resources from backup",
		Long: `Restores Kubernetes resources from a backup archive ` +
			`to the target cluster.

Resources are restored in the correct order ` +
			`(CRDs first, then namespaces, storage, workloads).
Existing resources can be skipped or updated based on the policy.

Example:
  ksail cluster restore --input ./my-backup.tar.gz
  ksail cluster restore -i ./backup.tar.gz --existing-resource-policy update
  ksail cluster restore --input ./backup.tar.gz --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRestore(cmd.Context(), cmd, flags)
		},
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().StringVarP(
		&flags.inputPath, "input", "i", "",
		"Input backup archive path (required)",
	)
	cmd.Flags().StringVar(
		&flags.existingResourcePolicy,
		"existing-resource-policy", resourcePolicyNone,
		"Policy for existing resources: none (skip) or update (patch)",
	)
	cmd.Flags().BoolVar(
		&flags.dryRun, "dry-run", false,
		"Print what would be restored without applying",
	)

	cobra.CheckErr(cmd.MarkFlagRequired("input"))

	return cmd
}

func runRestore(
	ctx context.Context,
	cmd *cobra.Command,
	flags *restoreFlags,
) error {
	if flags.existingResourcePolicy != resourcePolicyNone &&
		flags.existingResourcePolicy != resourcePolicyUpdate {
		return ErrInvalidResourcePolicy
	}

	// Canonicalize user-supplied input path (resolve symlinks + absolute)
	// so that the actual file being read is predictable and symlink-escape
	// attacks are prevented in CI pipelines.
	canonInput, err := fsutil.EvalCanonicalPath(flags.inputPath)
	if err != nil {
		return fmt.Errorf("resolve input path %q: %w", flags.inputPath, err)
	}

	flags.inputPath = canonInput

	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)
	if kubeconfigPath == "" {
		return ErrKubeconfigNotFound
	}

	writer := cmd.OutOrStdout()

	printRestoreHeader(writer, flags)

	tmpDir, metadata, err := extractBackupArchive(flags.inputPath)
	if err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	printRestoreMetadata(writer, metadata)

	backupName := deriveBackupName(flags.inputPath)
	restoreName := fmt.Sprintf("restore-%d", time.Now().UTC().UnixNano())

	_, _ = fmt.Fprintf(writer, "Restoring cluster resources...\n")

	err = restoreResources(
		ctx, kubeconfigPath, tmpDir, writer, flags,
		backupName, restoreName,
	)
	if err != nil {
		return fmt.Errorf("failed to restore resources: %w", err)
	}

	if flags.dryRun {
		_, _ = fmt.Fprintf(
			writer,
			"Dry-run completed successfully (no changes applied)\n",
		)
	} else {
		_, _ = fmt.Fprintf(writer, "Restore completed successfully\n")
	}

	return nil
}

// printRestoreHeader writes the initial restore status lines to the writer.
func printRestoreHeader(writer io.Writer, flags *restoreFlags) {
	_, _ = fmt.Fprintf(writer, "Starting cluster restore...\n")
	_, _ = fmt.Fprintf(writer, "   Input: %s\n", flags.inputPath)
	_, _ = fmt.Fprintf(
		writer, "   Policy: %s\n", flags.existingResourcePolicy,
	)

	if flags.dryRun {
		_, _ = fmt.Fprintf(
			writer, "   Mode: dry-run (no changes will be applied)\n",
		)
	}

	_, _ = fmt.Fprintf(writer, "Extracting backup archive...\n")
}

func printRestoreMetadata(writer io.Writer, metadata *BackupMetadata) {
	_, _ = fmt.Fprintf(writer, "Backup metadata:\n")
	_, _ = fmt.Fprintf(writer, "   Version: %s\n", metadata.Version)
	_, _ = fmt.Fprintf(
		writer, "   Timestamp: %s\n",
		metadata.Timestamp.Format("2006-01-02 15:04:05"),
	)
	_, _ = fmt.Fprintf(writer, "   Cluster: %s\n", metadata.ClusterName)

	if metadata.Distribution != "" {
		_, _ = fmt.Fprintf(
			writer, "   Distribution: %s\n", metadata.Distribution,
		)
	}

	if metadata.Provider != "" {
		_, _ = fmt.Fprintf(
			writer, "   Provider: %s\n", metadata.Provider,
		)
	}

	_, _ = fmt.Fprintf(
		writer, "   Resources: %d\n", metadata.ResourceCount,
	)
}

// deriveBackupName extracts a human-readable backup name from the archive path.
func deriveBackupName(inputPath string) string {
	base := filepath.Base(inputPath)
	name := strings.TrimSuffix(base, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")

	return name
}

func extractBackupArchive(
	inputPath string,
) (string, *BackupMetadata, error) {
	tmpDir, err := os.MkdirTemp("", "ksail-restore-*")
	if err != nil {
		return "", nil, fmt.Errorf(
			"failed to create temp directory: %w", err,
		)
	}

	file, err := os.Open(inputPath) //nolint:gosec // user-provided input
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", nil, fmt.Errorf(
			"failed to open backup archive: %w", err,
		)
	}

	defer func() { _ = file.Close() }()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", nil, fmt.Errorf(
			"failed to create gzip reader: %w", err,
		)
	}

	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)

	err = extractTarEntries(tarReader, tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", nil, err
	}

	metadata, err := readBackupMetadata(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return "", nil, err
	}

	return tmpDir, metadata, nil
}

func extractTarEntries(tarReader *tar.Reader, destDir string) error {
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		targetPath, err := validateTarEntry(header, destDir)
		if err != nil {
			return err
		}

		if header.Typeflag == tar.TypeDir {
			err = os.MkdirAll(
				targetPath,
				dirPerm,
			)
			if err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			continue
		}

		err = os.MkdirAll(
			filepath.Dir(targetPath),
			dirPerm,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to create parent directory: %w", err,
			)
		}

		err = extractFile(tarReader, targetPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateTarEntry(
	header *tar.Header,
	destDir string,
) (string, error) {
	// Only allow regular files and directories; reject symlinks,
	// hard links, char/block devices, FIFOs, and other special types.
	if header.Typeflag != tar.TypeDir &&
		header.Typeflag != tar.TypeReg {
		if header.Typeflag == tar.TypeSymlink ||
			header.Typeflag == tar.TypeLink {
			return "", ErrSymlinkInArchive
		}

		return "", fmt.Errorf(
			"%w: unsupported entry type %d for %s",
			ErrInvalidTarPath, header.Typeflag, header.Name,
		)
	}

	cleanName := filepath.Clean(header.Name)
	if filepath.IsAbs(cleanName) ||
		cleanName == ".." ||
		strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"%w: %s", ErrInvalidTarPath, header.Name,
		)
	}

	targetPath := filepath.Join(destDir, cleanName)

	relPath, err := filepath.Rel(destDir, targetPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf(
			"%w: %s", ErrInvalidTarPath, header.Name,
		)
	}

	return targetPath, nil
}

func extractFile(tarReader *tar.Reader, targetPath string) error {
	outFile, err := os.OpenFile( //nolint:gosec // path is sanitized by extractTarEntries
		targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm,
	)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer func() { _ = outFile.Close() }()

	_, err = io.Copy(outFile, tarReader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func readBackupMetadata(tmpDir string) (*BackupMetadata, error) {
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	metadataData, err := os.ReadFile(metadataPath) //nolint:gosec // path is constructed internally
	if err != nil {
		return nil, fmt.Errorf("failed to read backup metadata: %w", err)
	}

	var metadata BackupMetadata

	err = json.Unmarshal(metadataData, &metadata)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to parse backup metadata: %w", err,
		)
	}

	return &metadata, nil
}

func restoreResources(
	ctx context.Context,
	kubeconfigPath, tmpDir string,
	writer io.Writer,
	flags *restoreFlags,
	backupName, restoreName string,
) error {
	resourcesDir := filepath.Join(tmpDir, "resources")

	var restoreErrors []string

	for _, resourceType := range backupResourceTypes() {
		errs, err := restoreResourceType(
			ctx, kubeconfigPath, resourcesDir, resourceType,
			writer, flags, backupName, restoreName,
		)
		if err != nil {
			return err
		}

		restoreErrors = append(restoreErrors, errs...)
	}

	if len(restoreErrors) > 0 {
		return fmt.Errorf(
			"%w: %d resource(s): %s",
			ErrRestoreFailed,
			len(restoreErrors),
			strings.Join(restoreErrors, "; "),
		)
	}

	return nil
}

// restoreResourceType restores all YAML files for a single resource type
// from the backup directory, returning any per-file errors.
func restoreResourceType(
	ctx context.Context,
	kubeconfigPath, resourcesDir, resourceType string,
	writer io.Writer,
	flags *restoreFlags,
	backupName, restoreName string,
) ([]string, error) {
	resourceDir := filepath.Join(resourcesDir, resourceType)

	_, statErr := os.Stat(resourceDir)
	if os.IsNotExist(statErr) {
		return nil, nil
	}

	files, err := filepath.Glob(
		filepath.Join(resourceDir, "*.yaml"),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to list files for %s: %w", resourceType, err,
		)
	}

	if len(files) == 0 {
		return nil, nil
	}

	var errs []string

	for _, file := range files {
		err = restoreResourceFile(
			ctx, kubeconfigPath, file, flags,
			backupName, restoreName,
		)
		if err != nil {
			msg := fmt.Sprintf("%s: %v", filepath.Base(file), err)
			errs = append(errs, msg)

			_, _ = fmt.Fprintf(
				writer,
				"Warning: failed to restore %s: %v\n",
				filepath.Base(file), err,
			)

			continue
		}
	}

	// Only report success when every file for this type restored; partial
	// failures are surfaced via the per-file warnings above and the overall
	// ErrRestoreFailed the caller returns.
	if len(errs) == 0 {
		_, _ = fmt.Fprintf(writer, "   Restored %s\n", resourceType)
	}

	return errs, nil
}

func restoreResourceFile(
	ctx context.Context,
	kubeconfigPath, filePath string,
	flags *restoreFlags,
	backupName, restoreName string,
) error {
	labeledPath, err := injectRestoreLabels(
		filePath, backupName, restoreName,
	)
	if err != nil {
		return fmt.Errorf("failed to inject labels: %w", err)
	}

	defer func() { _ = os.Remove(labeledPath) }()

	// Skip files with no Kubernetes objects (empty backup category).
	if isEmptyYAML(labeledPath) {
		return nil
	}

	var outBuf, errBuf bytes.Buffer

	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    &outBuf,
		ErrOut: &errBuf,
	})

	var cmd *cobra.Command

	if flags.existingResourcePolicy == resourcePolicyNone {
		cmd = client.CreateCreateCommand(kubeconfigPath)
	} else {
		cmd = client.CreateApplyCommand(kubeconfigPath)
	}

	args := []string{"-f", labeledPath}

	useServerSideApply := flags.existingResourcePolicy == resourcePolicyUpdate
	if useServerSideApply {
		// Server-side apply avoids the client-side
		// last-applied-configuration annotation that can exceed the
		// 262144-byte annotation limit for large resources (e.g.
		// ArgoCD CRDs, Helm release Secrets).
		args = append(args, "--server-side", "--force-conflicts")
	}

	if flags.dryRun {
		if useServerSideApply {
			args = append(args, "--dry-run=server")
		} else {
			args = append(args, "--dry-run=client")
		}
	}

	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err = kubectl.ExecuteSafely(ctx, cmd)

	return classifyRestoreError(err, errBuf.String(), flags)
}

// isEmptyYAML returns true if the file at path contains no Kubernetes
// objects — only whitespace and YAML document separators.
func isEmptyYAML(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // path from extracted temp dir
	if err != nil {
		return false
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed != "---" {
			return false
		}
	}

	return true
}

// classifyRestoreError returns nil for benign errors (already-existing
// resources) and wraps real failures.
func classifyRestoreError(err error, stderr string, flags *restoreFlags) error {
	if err == nil {
		return nil
	}

	if flags.existingResourcePolicy == resourcePolicyNone {
		// Some resource types (e.g. DaemonSet, Job) route
		// "AlreadyExists" through BehaviorOnFatal instead of stderr.
		// Fall back to err.Error() when stderr is empty or
		// whitespace-only (which allLinesContain would also reject).
		source := stderr
		if strings.TrimSpace(source) == "" {
			source = err.Error()
		}

		if allLinesContain(source, "already exists") {
			return nil
		}
	}

	if stderr != "" {
		return fmt.Errorf("kubectl failed: %w (output: %s)", err, stderr)
	}

	return fmt.Errorf("kubectl failed: %w", err)
}

// injectRestoreLabels reads a YAML file, adds restore labels to each
// document, and writes the result to a temporary file. Returns the path
// to the temporary file.
func injectRestoreLabels(
	filePath, backupName, restoreName string,
) (string, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // path from extracted temp dir
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	docs := splitYAMLDocuments(string(data))

	var builder strings.Builder

	const estimatedBytesPerDoc = 512
	builder.Grow(len(docs) * estimatedBytesPerDoc)

	for idx, doc := range docs {
		if strings.TrimSpace(doc) == "" {
			continue
		}

		labeled, labelErr := addLabelsToDocument(
			doc, backupName, restoreName,
		)
		if labelErr != nil {
			return "", fmt.Errorf(
				"failed to inject restore labels: %w", labelErr,
			)
		}

		if idx > 0 {
			builder.WriteString("---\n")
		}

		builder.WriteString(labeled)
	}

	tmpFile, err := os.CreateTemp("", "ksail-restore-labeled-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() { _ = tmpFile.Close() }()

	_, err = tmpFile.WriteString(builder.String())
	if err != nil {
		_ = os.Remove(tmpFile.Name())

		return "", fmt.Errorf("failed to write labeled file: %w", err)
	}

	return tmpFile.Name(), nil
}

// addLabelsToDocument parses a single YAML document and adds restore labels.
func addLabelsToDocument(
	doc, backupName, restoreName string,
) (string, error) {
	var obj unstructured.Unstructured

	err := sigsyaml.Unmarshal([]byte(doc), &obj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to parse YAML: %w", err)
	}

	if obj.Object == nil {
		return doc, nil
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels["ksail.io/backup-name"] = backupName
	labels["ksail.io/restore-name"] = restoreName
	obj.SetLabels(labels)

	result, err := sigsyaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	return string(result), nil
}

// splitYAMLDocuments splits a multi-document YAML string into individual
// documents using the "---" separator.
func splitYAMLDocuments(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	var docs []string

	current := strings.Builder{}

	for line := range strings.SplitSeq(content, "\n") {
		if line == "---" {
			if current.Len() > 0 {
				docs = append(docs, current.String())
				current.Reset()
			}

			continue
		}

		current.WriteString(line)
		current.WriteString("\n")
	}

	if current.Len() > 0 {
		docs = append(docs, current.String())
	}

	return docs
}

func allLinesContain(output, substr string) bool {
	hasNonEmptyLine := false

	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		hasNonEmptyLine = true

		if !strings.Contains(trimmed, substr) {
			return false
		}
	}

	return hasNonEmptyLine
}

// defaultClusterMutationFieldSelectors returns the full set of field selectors
// used by commands that modify cluster state (create, update).
// This centralizes the selector list to avoid duplication between commands.
func defaultClusterMutationFieldSelectors() []ksailconfigmanager.FieldSelector[v1alpha1.Cluster] {
	selectors := ksailconfigmanager.DefaultClusterFieldSelectors()

	return append(
		selectors,
		ksailconfigmanager.DefaultProviderFieldSelector(),
		ksailconfigmanager.DefaultCNIFieldSelector(),
		ksailconfigmanager.DefaultMetricsServerFieldSelector(),
		ksailconfigmanager.DefaultLoadBalancerFieldSelector(),
		ksailconfigmanager.DefaultCertManagerFieldSelector(),
		ksailconfigmanager.DefaultPolicyEngineFieldSelector(),
		ksailconfigmanager.DefaultCSIFieldSelector(),
		ksailconfigmanager.DefaultCDIFieldSelector(),
		ksailconfigmanager.DefaultImportImagesFieldSelector(),
		ksailconfigmanager.ControlPlanesFieldSelector(),
		ksailconfigmanager.WorkersFieldSelector(),
		ksailconfigmanager.NodeAutoscalingFieldSelector(), //nolint:staticcheck // backward compat
		ksailconfigmanager.NodeAutoscalerEnabledFieldSelector(),
		ksailconfigmanager.OIDCIssuerURLFieldSelector(),
		ksailconfigmanager.OIDCClientIDFieldSelector(),
		ksailconfigmanager.OIDCUsernameClaimFieldSelector(),
		ksailconfigmanager.OIDCUsernamePrefixFieldSelector(),
		ksailconfigmanager.OIDCGroupsClaimFieldSelector(),
		ksailconfigmanager.OIDCGroupsPrefixFieldSelector(),
		ksailconfigmanager.OIDCCAFileFieldSelector(),
	)
}

// registerMirrorRegistryFlag adds the --mirror-registry flag to a command.
// The flag is intentionally NOT bound to Viper to allow custom merge logic
// via getMirrorRegistriesWithDefaults() in setup/mirrorregistry.
func registerMirrorRegistryFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with optional authentication. Format: [user:pass@]host[=upstream]. "+
			"Credentials support environment variables using ${VAR} syntax (quote placeholders so KSail can expand them). "+
			"Examples: docker.io=https://registry-1.docker.io, '${USER}:${TOKEN}@ghcr.io=https://ghcr.io'")
}

// registerNameFlag adds the --name flag to a command and binds it to Viper.
func registerNameFlag(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().StringP("name", "n", "",
		"Cluster name used for container names, registry names, and kubeconfig context")
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))
}

// registerOIDCExtraScopeFlag adds the --oidc-extra-scope flag to a command.
// Like --mirror-registry, this is a string slice flag that is NOT bound to Viper
// and instead merged manually after config loading.
func registerOIDCExtraScopeFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("oidc-extra-scope", []string{},
		"Additional OIDC scopes beyond openid (repeatable)")
}

// applyOIDCExtraScopeFlag merges --oidc-extra-scope flag values into the cluster config.
// CLI flag values take precedence over config file values when explicitly set.
func applyOIDCExtraScopeFlag(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	scopeFlag := cmd.Flags().Lookup("oidc-extra-scope")
	if scopeFlag == nil || !scopeFlag.Changed {
		return
	}

	scopes, err := cmd.Flags().GetStringSlice("oidc-extra-scope")
	if err != nil {
		return
	}

	// When the flag is explicitly set, always assign — even if empty — so the
	// user can clear extraScopes from a config file via CLI.
	clusterCfg.Spec.Cluster.OIDC.ExtraScopes = scopes
}

// registerAllowedCIDRsFlag adds the --allowed-cidrs flag to a command.
// Like --mirror-registry, this is a string slice flag NOT bound to Viper
// and merged manually via applyAllowedCIDRsFlag.
func registerAllowedCIDRsFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("allowed-cidrs", []string{},
		"CIDR blocks allowed to access the Kubernetes API and Talos API on control-plane nodes. "+
			"When empty, both APIs are open to 0.0.0.0/0 and ::/0 (all IPv4 and IPv6). "+
			"Example: --allowed-cidrs 203.0.113.0/24 --allowed-cidrs 198.51.100.0/24")
}

// applyAllowedCIDRsFlag merges --allowed-cidrs flag values into the cluster config.
// CLI flag values take precedence over config file values when explicitly set.
func applyAllowedCIDRsFlag(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	cidrFlag := cmd.Flags().Lookup("allowed-cidrs")
	if cidrFlag == nil || !cidrFlag.Changed {
		return
	}

	cidrs, err := cmd.Flags().GetStringSlice("allowed-cidrs")
	if err != nil {
		return
	}

	clusterCfg.Spec.Provider.Hetzner.AllowedCIDRs = cidrs
}

// setupMutationCmdFlags creates the shared config manager and registers the
// common flags (--mirror-registry and --name) used by cluster mutation commands.
// Returns the config manager for further flag bindings.
func setupMutationCmdFlags(cmd *cobra.Command) *ksailconfigmanager.ConfigManager {
	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		defaultClusterMutationFieldSelectors(),
	)

	registerMirrorRegistryFlag(cmd)
	registerNameFlag(cmd, cfgManager)
	registerOIDCExtraScopeFlag(cmd)
	registerAllowedCIDRsFlag(cmd)

	return cfgManager
}

// loadAndValidateClusterConfig loads configuration, applies name override, and validates
// the distribution x provider combination. This shared sequence is used by both
// create and update commands.
func loadAndValidateClusterConfig(
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) (*localregistry.Context, string, error) {
	outputTimer := deps.Timer

	ctx, err := loadClusterConfiguration(cfgManager, outputTimer)
	if err != nil {
		return nil, "", err
	}

	// Apply cluster name override: --name flag takes priority, then metadata.name
	nameOverride := cfgManager.Viper.GetString("name")
	if nameOverride == "" {
		nameOverride = ctx.ClusterCfg.Name
	}

	if nameOverride != "" {
		validationErr := v1alpha1.ValidateClusterName(nameOverride)
		if validationErr != nil {
			return nil, "", fmt.Errorf("invalid cluster name %q: %w", nameOverride, validationErr)
		}

		err = applyClusterNameOverride(ctx, nameOverride)
		if err != nil {
			return nil, "", err
		}
	}

	// Validate distribution x provider combination
	err = ctx.ClusterCfg.Spec.Cluster.Provider.ValidateForDistribution(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
	)
	if err != nil {
		return nil, "", fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate autoscaler configuration (pool names, min/max, server limit)
	err = v1alpha1.ValidateAutoscalerConfig(
		&ctx.ClusterCfg.Spec.Cluster,
		&ctx.ClusterCfg.Spec.Provider,
	)
	if err != nil {
		return nil, "", fmt.Errorf("invalid autoscaler configuration: %w", err)
	}

	// Validate OIDC configuration
	err = v1alpha1.ValidateOIDCConfig(&ctx.ClusterCfg.Spec.Cluster.OIDC)
	if err != nil {
		return nil, "", fmt.Errorf("OIDC configuration: %w", err)
	}

	clusterName := resolveClusterNameFromContext(ctx)

	return ctx, clusterName, nil
}

// runClusterCreationWorkflow performs the full cluster creation workflow.
// This is the shared implementation used by both the create handler and
// the update command's recreate flow.
//
//nolint:funlen // Sequential workflow steps are clearer kept together
func runClusterCreationWorkflow(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
) error {
	localDeps := getLocalRegistryDeps()

	err := ensureLocalRegistriesReady(
		cmd,
		ctx,
		deps,
		cfgManager,
		localDeps,
	)
	if err != nil {
		return err
	}

	setupK3dCNI(ctx.ClusterCfg, ctx.K3dConfig)
	setupK3dMetricsServer(ctx.ClusterCfg, ctx.K3dConfig)
	setupK3dCSI(ctx.ClusterCfg, ctx.K3dConfig)
	setupK3dLoadBalancer(ctx.ClusterCfg, ctx.K3dConfig)
	setupVClusterCNI(ctx.ClusterCfg, ctx.VClusterConfig)

	err = resolveNestedMirrorSpecs(cmd, cfgManager, ctx)
	if err != nil {
		return err
	}

	configureProvisionerFactory(&deps, ctx)

	err = executeClusterLifecycle(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return err
	}

	// Post-creation Docker steps are only needed for local Docker clusters.
	// Cloud providers (Omni, Hetzner) run nodes remotely and the Kubernetes
	// provider runs nodes as pods — neither can access local Docker infrastructure.
	if ctx.ClusterCfg.Spec.Cluster.Provider.NeedsLocalDocker() {
		configureRegistryMirrorsInClusterWithWarning(
			cmd,
			ctx,
			deps,
			cfgManager,
			localDeps,
		)

		err = localregistry.ExecuteStage(
			cmd,
			ctx,
			deps,
			localregistry.StageConnect,
			localDeps,
		)
		if err != nil {
			return fmt.Errorf("failed to connect local registry: %w", err)
		}
	}

	err = localregistry.WaitForK3dLocalRegistryReady(
		cmd,
		ctx.ClusterCfg,
		ctx.K3dConfig,
		localDeps.DockerInvoker,
	)
	if err != nil {
		return fmt.Errorf("failed to wait for local registry: %w", err)
	}

	// Set Connection.Context so post-CNI setup (InstallCNI, helm, kubectl) can resolve
	// the correct kubeconfig context. This MUST happen after local registry operations
	// (which resolve cluster name from distribution configs, not from context) but before
	// post-CNI setup (which needs the kubectl context name like "kind-kind").
	//
	// For Omni clusters, the kubeconfig context is now renamed during saveOmniKubeconfig
	// to match the configured context or the Talos convention (admin@<name>).
	// If an explicit context is already configured, preserve it.
	if ctx.ClusterCfg.Spec.Cluster.Connection.Context == "" {
		clusterName := resolveClusterNameFromContext(ctx)
		ctx.ClusterCfg.Spec.Cluster.Connection.Context = resolveCreatedContextName(
			ctx.ClusterCfg.Spec.Cluster.Distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
			clusterName,
		)
	}

	maybeImportCachedImages(cmd, ctx, deps.Timer)

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer)
}

// resolveCreatedContextName returns the kubeconfig context name a freshly created
// cluster is written under, so post-creation setup (CNI install, helm, kubectl) can
// target it. The Kubernetes provider runs K3s via the k3k operator, which writes a
// "k3k-<name>" context rather than the standalone k3d "k3d-<name>" context; without
// this, installing a CNI like Calico on a nested K3s cluster fails to find the
// context. All other distribution/provider combinations use the standalone
// distribution context name.
func resolveCreatedContextName(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	clusterName string,
) string {
	if clusterName != "" &&
		provider == v1alpha1.ProviderKubernetes &&
		distribution == v1alpha1.DistributionK3s {
		return "k3k-" + clusterName
	}

	return distribution.ContextName(clusterName)
}
