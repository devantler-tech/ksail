package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/internal/buildmeta"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
	// bytesPerMB is the number of bytes in a megabyte.
	bytesPerMB = 1024 * 1024
	// defaultCompressionLevel is gzip.DefaultCompression (-1), which tells
	// the gzip writer to use its built-in default compression level.
	defaultCompressionLevel = -1
	// minCompressionLevel is the minimum gzip compression level.
	minCompressionLevel = -1
	// maxCompressionLevel is the maximum gzip compression level.
	maxCompressionLevel = 9
)

// ErrKubeconfigNotFound is returned when no kubeconfig path can be resolved.
var ErrKubeconfigNotFound = errors.New(
	"kubeconfig not found; ensure cluster is created and configured",
)

// ErrInvalidCompressionLevel is returned when the compression level is
// outside the valid range.
var ErrInvalidCompressionLevel = errors.New(
	"compression level out of range",
)

// BackupMetadata contains metadata about a backup.
type BackupMetadata struct {
	Version       string    `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
	ClusterName   string    `json:"clusterName"`
	Distribution  string    `json:"distribution"`
	Provider      string    `json:"provider"`
	KSailVersion  string    `json:"ksailVersion"`
	ResourceCount int       `json:"resourceCount"`
	ResourceTypes []string  `json:"resourceTypes"`
}

type backupFlags struct {
	outputPath       string
	includeVolumes   bool
	namespaces       []string
	excludeTypes     []string
	compressionLevel int
}

// NewBackupCmd creates the cluster backup command.
func NewBackupCmd(_ *di.Runtime) *cobra.Command {
	flags := &backupFlags{
		compressionLevel: defaultCompressionLevel,
	}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup cluster resources",
		Long: `Creates a backup archive containing Kubernetes resource manifests.

The backup is stored as a compressed tarball (.tar.gz) with resources ` +
			`organized by type.
Metadata about the backup is included for restore operations.

Note: This backs up resource manifests (YAML) only. Persistent volume
contents are not included in the current implementation.

Example:
  ksail cluster backup --output ./my-backup.tar.gz
  ksail cluster backup -o ./backup.tar.gz --namespaces default,kube-system
  ksail cluster backup -o ./backup.tar.gz --exclude-types events,pods`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackup(cmd.Context(), cmd, flags)
		},
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().StringVarP(
		&flags.outputPath, "output", "o", "",
		"Output path for backup archive (required)",
	)
	cmd.Flags().BoolVar(
		&flags.includeVolumes, "include-volumes", true,
		"Include persistent volume data in backup (not yet implemented)",
	)
	cmd.Flags().StringSliceVarP(
		&flags.namespaces, "namespaces", "n", []string{},
		"Namespaces to backup (default: all)",
	)
	cmd.Flags().StringSliceVar(
		&flags.excludeTypes, "exclude-types", []string{"events"},
		"Resource types to exclude from backup",
	)
	cmd.Flags().IntVar(
		&flags.compressionLevel, "compression", defaultCompressionLevel,
		"Compression level (0-9, default: -1 (gzip default))",
	)

	cobra.CheckErr(cmd.MarkFlagRequired("output"))

	return cmd
}

// prepareOutputPath creates the output directory if needed and canonicalizes
// the output path via EvalCanonicalPath to prevent symlink-escape attacks.
func prepareOutputPath(outputPath string) (string, error) {
	outputDir := filepath.Dir(outputPath)
	if outputDir != "." && outputDir != "" {
		err := os.MkdirAll(outputDir, dirPerm)
		if err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Canonicalize after MkdirAll so the parent directory exists for symlink resolution.
	canonOutput, err := fsutil.EvalCanonicalPath(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolve output path %q: %w", outputPath, err)
	}

	return canonOutput, nil
}

func runBackup(ctx context.Context, cmd *cobra.Command, flags *backupFlags) error {
	if flags.compressionLevel < minCompressionLevel ||
		flags.compressionLevel > maxCompressionLevel {
		return fmt.Errorf(
			"%w: must be between %d and %d",
			ErrInvalidCompressionLevel,
			minCompressionLevel, maxCompressionLevel,
		)
	}

	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)
	if kubeconfigPath == "" {
		return ErrKubeconfigNotFound
	}

	writer := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(writer, "Starting cluster backup...\n")
	_, _ = fmt.Fprintf(writer, "   Output: %s\n", flags.outputPath)

	if flags.includeVolumes {
		_, _ = fmt.Fprintln(writer,
			"Warning: --include-volumes is not yet implemented;"+
				" volume data will NOT be included in this backup.",
		)
	}

	if len(flags.namespaces) > 0 {
		_, _ = fmt.Fprintf(writer, "   Namespaces: %v\n", flags.namespaces)
	} else {
		_, _ = fmt.Fprintf(writer, "   Namespaces: all\n")
	}

	canonOutput, err := prepareOutputPath(flags.outputPath)
	if err != nil {
		return err
	}

	flags.outputPath = canonOutput

	err = createBackupArchive(ctx, kubeconfigPath, writer, flags)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	printBackupSummary(writer, flags.outputPath)

	return nil
}

func printBackupSummary(writer io.Writer, outputPath string) {
	_, _ = fmt.Fprintf(writer, "Backup completed successfully\n")

	info, err := os.Stat(outputPath)
	if err == nil {
		sizeMB := float64(info.Size()) / bytesPerMB
		_, _ = fmt.Fprintf(writer, "   Archive size: %.2f MB\n", sizeMB)
	}
}

func createBackupArchive(
	ctx context.Context,
	kubeconfigPath string,
	writer io.Writer,
	flags *backupFlags,
) error {
	tmpDir, err := os.MkdirTemp("", "ksail-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	_, _ = fmt.Fprintf(writer, "Gathering cluster metadata...\n")

	metadata := &BackupMetadata{
		Version:      "v1",
		Timestamp:    time.Now(),
		ClusterName:  getClusterNameFromKubeconfig(kubeconfigPath),
		KSailVersion: buildmeta.Version,
	}

	//nolint:contextcheck // best-effort detection; DetectInfo API does not accept context
	populateClusterInfo(metadata, kubeconfigPath)

	_, _ = fmt.Fprintf(writer, "Exporting cluster resources...\n")

	filteredTypes := filterExcludedTypes(
		backupResourceTypes(), flags.excludeTypes,
	)

	resourceCount, backedUpTypes := exportResources(
		ctx, kubeconfigPath, tmpDir, writer, flags, filteredTypes,
	)

	metadata.ResourceCount = resourceCount
	metadata.ResourceTypes = backedUpTypes

	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	err = writeMetadata(metadata, metadataPath)
	if err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	_, _ = fmt.Fprintf(writer, "Creating compressed archive...\n")

	err = createTarball(tmpDir, flags.outputPath, flags.compressionLevel)
	if err != nil {
		return fmt.Errorf("failed to create tarball: %w", err)
	}

	return nil
}

// populateClusterInfo uses the cluster detector to populate
// distribution and provider fields in the metadata. Errors are
// intentionally ignored because detection may fail when the
// kubeconfig context pattern is unrecognized (e.g., imported configs);
// in that case the fields remain empty strings.
func populateClusterInfo(metadata *BackupMetadata, kubeconfigPath string) {
	info, err := clusterdetector.DetectInfo(kubeconfigPath, "")
	if err != nil {
		return
	}

	metadata.Distribution = string(info.Distribution)
	metadata.Provider = string(info.Provider)
}

// clusterScopedResourceTypes returns resource types that are cluster-scoped
// (not namespaced). These should never use -n or --all-namespaces flags.
func clusterScopedResourceTypes() map[string]bool {
	return map[string]bool{
		"customresourcedefinitions": true,
		"namespaces":                true,
		"storageclasses":            true,
		"persistentvolumes":         true,
		"clusterroles":              true,
		"clusterrolebindings":       true,
	}
}

// backupResourceTypes returns the ordered list of resource types for backup.
// CRDs and cluster-scoped resources come first, followed by storage, RBAC,
// and workloads.
func backupResourceTypes() []string {
	return []string{
		"customresourcedefinitions",
		"namespaces",
		"storageclasses",
		"persistentvolumes",
		"persistentvolumeclaims",
		"secrets",
		"configmaps",
		"serviceaccounts",
		"roles",
		"rolebindings",
		"clusterroles",
		"clusterrolebindings",
		"services",
		"deployments",
		"statefulsets",
		"daemonsets",
		"jobs",
		"cronjobs",
		"ingresses",
	}
}

// exportResult holds the outcome of exporting a single resource type.
type exportResult struct {
	resourceType string
	count        int
	err          error
}

func exportResources(
	ctx context.Context,
	kubeconfigPath, outputDir string,
	writer io.Writer,
	flags *backupFlags,
	filteredTypes []string,
) (int, []string) {
	results := make([]exportResult, len(filteredTypes))

	group, groupCtx := errgroup.WithContext(ctx)

	for idx, resourceType := range filteredTypes {
		group.Go(func() error {
			count, err := exportResourceType(
				groupCtx, kubeconfigPath, outputDir, resourceType, flags,
			)
			// Store at the pre-allocated index; no mutex needed because
			// each goroutine writes to a distinct slot.
			results[idx] = exportResult{resourceType: resourceType, count: count, err: err}

			return nil
		})
	}

	_ = group.Wait()

	// Collect results in original order for deterministic output.
	totalCount := 0

	var backedUpTypes []string

	for _, result := range results {
		if result.err != nil {
			_, _ = fmt.Fprintf(
				writer,
				"Warning: failed to export %s: %v\n",
				result.resourceType, result.err,
			)

			continue
		}

		if result.count > 0 {
			_, _ = fmt.Fprintf(
				writer, "   Exported %d %s\n", result.count, result.resourceType,
			)
			totalCount += result.count

			backedUpTypes = append(backedUpTypes, result.resourceType)
		}
	}

	return totalCount, backedUpTypes
}

func filterExcludedTypes(resourceTypes, excludeTypes []string) []string {
	excluded := make(map[string]bool, len(excludeTypes))
	for _, excludeType := range excludeTypes {
		excluded[excludeType] = true
	}

	var filtered []string

	for _, resourceType := range resourceTypes {
		if !excluded[resourceType] {
			filtered = append(filtered, resourceType)
		}
	}

	return filtered
}

func exportResourceType(
	ctx context.Context,
	kubeconfigPath, outputDir, resourceType string,
	flags *backupFlags,
) (int, error) {
	resourceDir := filepath.Join(outputDir, "resources", resourceType)

	err := os.MkdirAll(resourceDir, dirPerm)
	if err != nil {
		return 0, fmt.Errorf("failed to create resource directory: %w", err)
	}

	isClusterScoped := clusterScopedResourceTypes()[resourceType]

	// Cluster-scoped resources are always fetched without namespace flags,
	// even when specific namespaces are requested.
	if isClusterScoped {
		return executeGetAndSave(
			ctx, kubeconfigPath, resourceDir, resourceType, "", true,
		)
	}

	if len(flags.namespaces) > 0 {
		totalCount := 0

		for _, ns := range flags.namespaces {
			count, err := executeGetAndSave(
				ctx, kubeconfigPath, resourceDir, resourceType, ns, false,
			)
			if err != nil {
				return totalCount, err
			}

			totalCount += count
		}

		return totalCount, nil
	}

	return executeGetAndSave(
		ctx, kubeconfigPath, resourceDir, resourceType, "", false,
	)
}

func executeGetAndSave(
	ctx context.Context,
	kubeconfigPath, resourceDir, resourceType, namespace string,
	clusterScoped bool,
) (int, error) {
	filename := resourceType + ".yaml"
	if namespace != "" {
		filename = fmt.Sprintf("%s-%s.yaml", resourceType, namespace)
	}

	outputPath := filepath.Join(resourceDir, filename)

	output, stderr, err := runKubectlGet(
		ctx, kubeconfigPath, resourceType, namespace, clusterScoped,
	)
	if err != nil {
		if strings.Contains(stderr, "the server doesn't have a resource type") {
			return 0, nil
		}

		return 0, fmt.Errorf("failed to get resources: %w", err)
	}

	if len(output) == 0 || strings.Contains(output, "No resources found") {
		return 0, nil
	}

	sanitized, err := sanitizeYAMLOutput(output)
	if err != nil {
		return 0, fmt.Errorf("failed to sanitize output: %w", err)
	}

	err = os.WriteFile(outputPath, []byte(sanitized), filePerm)
	if err != nil {
		return 0, fmt.Errorf("failed to write resource file: %w", err)
	}

	count := countYAMLDocuments(sanitized)

	return count, nil
}

func runKubectlGet(
	ctx context.Context,
	kubeconfigPath, resourceType, namespace string,
	clusterScoped bool,
) (string, string, error) {
	var outBuf, errBuf bytes.Buffer

	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    &outBuf,
		ErrOut: &errBuf,
	})

	getCmd := client.CreateGetCommand(kubeconfigPath)

	args := []string{resourceType, "-o", "yaml"}

	if !clusterScoped && namespace != "" {
		args = append(args, "-n", namespace)
	} else if !clusterScoped {
		args = append(args, "--all-namespaces")
	}

	getCmd.SetArgs(args)
	getCmd.SilenceUsage = true
	getCmd.SilenceErrors = true

	err := getCmd.ExecuteContext(ctx)
	if err != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf(
			"kubectl get %s: %w", resourceType, err,
		)
	}

	return outBuf.String(), errBuf.String(), nil
}

func getClusterNameFromKubeconfig(kubeconfigPath string) string {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return filepath.Base(kubeconfigPath)
	}

	if config.CurrentContext != "" {
		return config.CurrentContext
	}

	return filepath.Base(kubeconfigPath)
}

func countYAMLDocuments(content string) int {
	count := 0
	isListWrapper := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Count list items via "- apiVersion:" (indented in kubectl output).
		if strings.HasPrefix(trimmed, "- apiVersion:") {
			count++

			continue
		}

		// Count top-level documents via non-indented "kind:" lines.
		// Indented kind: lines belong to list items already counted above.
		if strings.HasPrefix(line, "kind:") {
			count++

			if !isListWrapper {
				kindValue := strings.TrimSpace(
					strings.TrimPrefix(line, "kind:"),
				)

				isListWrapper = strings.HasSuffix(kindValue, "List")
			}
		}
	}

	// Subtract the List wrapper kind if items were counted.
	if isListWrapper && count > 1 {
		count--
	}

	if count == 0 {
		return 1
	}

	return count
}
