package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/buildmeta"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"golang.org/x/sync/errgroup"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
)

// BackupOptions configures a [Backupper]. The fields mirror the cluster backup
// CLI flags plus the resolved kubeconfig path.
//
//nolint:revive // BackupOptions reads clearly at call sites despite the package-name stutter.
type BackupOptions struct {
	// KubeconfigPath is the resolved path to the kubeconfig used for export.
	KubeconfigPath string
	// Context, when non-empty, pins the backup to a specific kubeconfig context
	// (e.g. resolved from --name) instead of the kubeconfig's current-context.
	Context string
	// OutputPath is the destination path for the backup archive. Callers should
	// canonicalize it (e.g. via fsutil.EvalCanonicalPath) before invoking.
	OutputPath string
	// Namespaces limits the backup to the given namespaces (empty = all).
	Namespaces []string
	// ExcludeTypes lists resource types to skip.
	ExcludeTypes []string
	// CompressionLevel is the gzip compression level (-1..9, -1 = gzip default).
	CompressionLevel int
}

// Backupper creates backup archives of cluster resources.
type Backupper struct {
	opts BackupOptions
}

// NewBackupper constructs a Backupper from the given options.
func NewBackupper(opts BackupOptions) *Backupper {
	return &Backupper{opts: opts}
}

// Backup exports cluster resources into a compressed archive at the configured
// output path, writing human-readable progress to writer.
func (b *Backupper) Backup(ctx context.Context, writer io.Writer) error {
	if b.opts.CompressionLevel < minCompressionLevel ||
		b.opts.CompressionLevel > maxCompressionLevel {
		return fmt.Errorf(
			"%w: must be between %d and %d",
			ErrInvalidCompressionLevel,
			minCompressionLevel, maxCompressionLevel,
		)
	}

	err := b.createBackupArchive(ctx, writer)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	return nil
}

func (b *Backupper) createBackupArchive(
	ctx context.Context,
	writer io.Writer,
) error {
	tmpDir, err := os.MkdirTemp("", "ksail-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	_, _ = fmt.Fprintf(writer, "Gathering cluster metadata...\n")

	// Record the targeted context when --name pinned one; otherwise fall back to
	// the kubeconfig's current-context (the cluster the backup actually runs against).
	clusterName := b.opts.Context
	if clusterName == "" {
		clusterName = getClusterNameFromKubeconfig(b.opts.KubeconfigPath)
	}

	metadata := &BackupMetadata{
		Version:      "v1",
		Timestamp:    time.Now(),
		ClusterName:  clusterName,
		KSailVersion: buildmeta.Version,
	}

	populateClusterInfo(ctx, metadata, b.opts.KubeconfigPath, b.opts.Context)

	_, _ = fmt.Fprintf(writer, "Exporting cluster resources...\n")

	filteredTypes := filterExcludedTypes(
		backupResourceTypes(), b.opts.ExcludeTypes,
	)

	resourceCount, backedUpTypes := b.exportResources(
		ctx, tmpDir, writer, filteredTypes,
	)

	metadata.ResourceCount = resourceCount
	metadata.ResourceTypes = backedUpTypes

	metadataPath := filepath.Join(tmpDir, metadataFileName)

	err = writeMetadata(metadata, metadataPath)
	if err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	_, _ = fmt.Fprintf(writer, "Creating compressed archive...\n")

	err = createTarball(tmpDir, b.opts.OutputPath, b.opts.CompressionLevel)
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
func populateClusterInfo(
	ctx context.Context,
	metadata *BackupMetadata,
	kubeconfigPath, kubeContext string,
) {
	info, err := clusterdetector.DetectInfo(ctx, kubeconfigPath, kubeContext)
	if err != nil {
		return
	}

	metadata.Distribution = string(info.Distribution)
	metadata.Provider = string(info.Provider)
}

// exportResult holds the outcome of exporting a single resource type.
type exportResult struct {
	resourceType string
	count        int
	err          error
}

func (b *Backupper) exportResources(
	ctx context.Context,
	outputDir string,
	writer io.Writer,
	filteredTypes []string,
) (int, []string) {
	results := make([]exportResult, len(filteredTypes))

	group, groupCtx := errgroup.WithContext(ctx)

	for idx, resourceType := range filteredTypes {
		group.Go(func() error {
			count, err := b.exportResourceType(
				groupCtx, outputDir, resourceType,
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

func (b *Backupper) exportResourceType(
	ctx context.Context,
	outputDir, resourceType string,
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
		return b.executeGetAndSave(
			ctx, resourceDir, resourceType, "", true,
		)
	}

	if len(b.opts.Namespaces) > 0 {
		totalCount := 0

		for _, ns := range b.opts.Namespaces {
			count, err := b.executeGetAndSave(
				ctx, resourceDir, resourceType, ns, false,
			)
			if err != nil {
				return totalCount, err
			}

			totalCount += count
		}

		return totalCount, nil
	}

	return b.executeGetAndSave(
		ctx, resourceDir, resourceType, "", false,
	)
}

func (b *Backupper) executeGetAndSave(
	ctx context.Context,
	resourceDir, resourceType, namespace string,
	clusterScoped bool,
) (int, error) {
	filename := resourceType + ".yaml"
	if namespace != "" {
		filename = fmt.Sprintf("%s-%s.yaml", resourceType, namespace)
	}

	outputPath := filepath.Join(resourceDir, filename)

	output, stderr, err := runKubectlGet(
		ctx, b.opts.KubeconfigPath, b.opts.Context, resourceType, namespace, clusterScoped,
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

	if len(sanitized) == 0 {
		return 0, nil
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
	kubeconfigPath, kubeContext, resourceType, namespace string,
	clusterScoped bool,
) (string, string, error) {
	var outBuf, errBuf bytes.Buffer

	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    &outBuf,
		ErrOut: &errBuf,
	}).WithKubeContext(kubeContext)

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

	err := kubectl.ExecuteSafely(ctx, getCmd)
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
