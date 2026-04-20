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
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/buildmeta"
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/dockerutil"
	"github.com/devantler-tech/ksail/v7/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/picker"
	argocdclient "github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/k9s"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	imagesvc "github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
	// bytesPerMB is the number of bytes in a megabyte.
	bytesPerMB = 1024 * 1024
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
		"Compression level (-1..9, -1 = gzip default)",
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

// defaultCompressionLevel uses gzip.DefaultCompression so the constant stays
// co-located with the gzip import and avoids a magic number in backup.go.
const defaultCompressionLevel = gzip.DefaultCompression

func writeMetadata(metadata *BackupMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	err = os.WriteFile(path, data, filePerm)
	if err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

func createTarball(
	sourceDir, targetPath string,
	compressionLevel int,
) error {
	// Use os.CreateTemp so the temp path is unique — avoids clobbering a
	// pre-existing .tmp file from a previous failed run and reduces races.
	tmpDir := filepath.Dir(targetPath)
	tmpPrefix := filepath.Base(targetPath) + ".tmp-"

	outFile, err := os.CreateTemp(tmpDir, tmpPrefix)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := outFile.Name()

	gzipWriter, err := gzip.NewWriterLevel(outFile, compressionLevel)
	if err != nil {
		_ = outFile.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to create gzip writer: %w", err)
	}

	tarWriter := tar.NewWriter(gzipWriter)

	err = filepath.Walk(
		sourceDir,
		func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			return addFileToTar(tarWriter, sourceDir, path, info)
		},
	)
	if err != nil {
		// Surface any close errors alongside the walk error so callers see both.
		closeErr := errors.Join(tarWriter.Close(), gzipWriter.Close(), outFile.Close())
		_ = os.Remove(tmpPath)

		return errors.Join(fmt.Errorf("failed to walk source directory: %w", err), closeErr)
	}

	return commitTarball(tarWriter, gzipWriter, outFile, tmpPath, targetPath)
}

// commitTarball flushes and closes the writers, then atomically renames the
// temp file to targetPath. It is extracted from createTarball to keep that
// function within the project's line-length limit.
func commitTarball(
	tarWriter *tar.Writer,
	gzipWriter *gzip.Writer,
	outFile *os.File,
	tmpPath, targetPath string,
) error {
	err := tarWriter.Close()
	if err != nil {
		_ = gzipWriter.Close()
		_ = outFile.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		_ = outFile.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	err = outFile.Close()
	if err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to close output file: %w", err)
	}

	// Try an atomic rename first; on Unix this replaces the destination in one
	// operation, so the previous archive survives if Rename fails.
	// On Windows, Rename can fail with a permission/access error when the
	// destination already exists. Fall back to remove-and-retry only when the
	// target actually exists (os.Stat succeeds) so unrelated failures never
	// destroy a valid backup.
	err = os.Rename(tmpPath, targetPath)
	if err != nil {
		_, statErr := os.Stat(targetPath)
		if statErr == nil {
			_ = os.Remove(targetPath)

			err = os.Rename(tmpPath, targetPath)
		}
	}

	if err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("failed to finalize archive: %w", err)
	}

	return nil
}

func addFileToTar(
	tarWriter *tar.Writer,
	sourceDir, path string,
	info os.FileInfo,
) error {
	// Skip symlinks and special files (devices, pipes, sockets, etc.).
	// restore explicitly rejects non-regular files, so including them would
	// produce backups that cannot be restored.
	if !info.IsDir() && info.Mode()&os.ModeType != 0 {
		return nil
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create tar header: %w", err)
	}

	relPath, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	header.Name = filepath.ToSlash(relPath)

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if info.IsDir() {
		return nil
	}

	file, err := os.Open( //nolint:gosec // G304: path from archive walk
		path,
	)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func() { _ = file.Close() }()

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return fmt.Errorf("failed to write file to tar: %w", err)
	}

	return nil
}

// sanitizeYAMLOutput removes server-assigned metadata fields from kubectl
// output to produce portable, apply-able manifests. Fields stripped include
// resourceVersion, uid, selfLink, creationTimestamp, managedFields, and
// the entire status block.
func sanitizeYAMLOutput(output string) (string, error) {
	var obj unstructured.Unstructured

	err := sigsyaml.Unmarshal([]byte(output), &obj.Object)
	if err != nil {
		// If we can't parse it, return the original output unchanged.
		return output, nil //nolint:nilerr // non-parseable output is kept as-is
	}

	kind := obj.GetKind()
	if strings.HasSuffix(kind, "List") {
		return sanitizeList(&obj)
	}

	if isHelmReleaseSecret(&obj) {
		return "", nil
	}

	sanitizeObject(&obj)

	result, err := sigsyaml.Marshal(obj.Object)
	if err != nil {
		return output, nil //nolint:nilerr // marshal failure falls back to original
	}

	return string(result), nil
}

func sanitizeList(list *unstructured.Unstructured) (string, error) {
	items, found, err := unstructured.NestedSlice(
		list.Object, "items",
	)
	if err != nil || !found {
		// No items found; sanitize the list object itself.
		sanitizeObject(list)

		result, marshalErr := sigsyaml.Marshal(list.Object)
		if marshalErr != nil {
			return "", fmt.Errorf("failed to marshal list: %w", marshalErr)
		}

		return string(result), nil
	}

	var builder strings.Builder

	// Pre-allocate capacity: estimate ~256 bytes per item for YAML output
	// plus separator overhead.
	const estimatedBytesPerItem = 256
	builder.Grow(len(items) * estimatedBytesPerItem)

	wroteAny := false

	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue // Skip malformed items that aren't maps
		}

		obj := &unstructured.Unstructured{Object: itemMap}

		if isHelmReleaseSecret(obj) {
			continue
		}

		sanitizeObject(obj)

		data, marshalErr := sigsyaml.Marshal(obj.Object)
		if marshalErr != nil {
			continue // Skip items that can't be marshaled
		}

		if wroteAny {
			builder.WriteString("---\n")
		}

		builder.Write(data)

		wroteAny = true
	}

	return builder.String(), nil
}

func sanitizeObject(obj *unstructured.Unstructured) {
	// Remove server-assigned metadata fields
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "resourceVersion",
	)
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "metadata", "selfLink")
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "creationTimestamp",
	)
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "managedFields",
	)
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "generation",
	)

	// Remove last-applied-configuration annotation — it can be very large
	// (duplicates the entire resource body) and causes annotation-size-limit
	// errors (>262144 bytes) on restore for resources with large specs
	// such as ArgoCD CRDs or Helm-managed resources.
	annotations := obj.GetAnnotations()
	if _, ok := annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")

		if len(annotations) == 0 {
			obj.SetAnnotations(nil)
		} else {
			obj.SetAnnotations(annotations)
		}
	}

	// Remove status block
	unstructured.RemoveNestedField(obj.Object, "status")

	// For Jobs, remove auto-generated selector and controller-uid labels.
	// Kubernetes auto-generates spec.selector with a controller-uid label
	// that is tied to the original Job's UID. Restoring with these fields
	// fails because the selector is immutable and the UID won't match.
	if obj.GetKind() == "Job" {
		unstructured.RemoveNestedField(
			obj.Object, "spec", "selector",
		)
		// Also remove the matching label from the pod template so the
		// new auto-generated selector will work correctly.
		removeAutoGeneratedJobLabels(obj,
			"spec", "template", "metadata", "labels",
		)
	}

	// For Services, remove cluster-assigned ClusterIP fields. These are
	// ephemeral values assigned by the cluster's service CIDR allocator
	// and are not portable across clusters. Restoring with them causes
	// "provided IP is already allocated" errors when the service already
	// exists. Headless services (clusterIP: "None") are preserved.
	if obj.GetKind() == "Service" {
		removeServiceClusterIPs(obj)
	}
}

// removeAutoGeneratedJobLabels removes batch.kubernetes.io/controller-uid
// and controller-uid labels from the given nested field path.
func removeAutoGeneratedJobLabels(
	obj *unstructured.Unstructured, fields ...string,
) {
	labels, found, err := unstructured.NestedStringMap(
		obj.Object, fields...,
	)
	if err != nil || !found {
		return
	}

	delete(labels, "batch.kubernetes.io/controller-uid")
	delete(labels, "controller-uid")

	if len(labels) == 0 {
		unstructured.RemoveNestedField(obj.Object, fields...)
	} else {
		_ = unstructured.SetNestedStringMap(
			obj.Object, labels, fields...,
		)
	}
}

// removeServiceClusterIPs strips spec.clusterIP and spec.clusterIPs from a
// Service object unless it is a headless service (clusterIP: "None").
func removeServiceClusterIPs(obj *unstructured.Unstructured) {
	clusterIP, found, _ := unstructured.NestedString(
		obj.Object, "spec", "clusterIP",
	)

	// Headless services use "None" — preserve that value.
	if found && clusterIP == "None" {
		return
	}

	unstructured.RemoveNestedField(obj.Object, "spec", "clusterIP")
	unstructured.RemoveNestedField(obj.Object, "spec", "clusterIPs")
}

// isHelmReleaseSecret returns true if the object is a Helm release Secret
// (type "helm.sh/release.v1"). These internal Helm state objects should not
// be backed up as they are regenerated automatically when Helm releases are
// reapplied.
func isHelmReleaseSecret(obj *unstructured.Unstructured) bool {
	if obj.GetKind() != "Secret" {
		return false
	}

	secretType, _, _ := unstructured.NestedString(obj.Object, "type")

	return secretType == "helm.sh/release.v1" //nolint:gosec // Kubernetes Secret type identifier, not credentials
}

// NewClusterCmd creates the parent cluster command and wires lifecycle subcommands beneath it.
func NewClusterCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage cluster lifecycle",
		Long: `Manage lifecycle operations for local Kubernetes clusters, including ` +
			`provisioning, teardown, and status.`,
		Args:         cobra.NoArgs,
		RunE:         handleClusterRunE,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: "command",
		},
	}

	cmd.AddCommand(NewInitCmd(runtimeContainer))
	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewUpdateCmd(runtimeContainer))
	cmd.AddCommand(NewDeleteCmd(runtimeContainer))
	cmd.AddCommand(NewStartCmd(runtimeContainer))
	cmd.AddCommand(NewStopCmd(runtimeContainer))
	cmd.AddCommand(NewListCmd(runtimeContainer))
	cmd.AddCommand(NewInfoCmd(runtimeContainer))
	cmd.AddCommand(NewConnectCmd(runtimeContainer))
	cmd.AddCommand(NewBackupCmd(runtimeContainer))
	cmd.AddCommand(NewRestoreCmd(runtimeContainer))
	cmd.AddCommand(NewSwitchCmd(runtimeContainer))

	return cmd
}

//nolint:gochecknoglobals // Injected for testability to simulate help failures.
var helpRunner = func(cmd *cobra.Command) error {
	return cmd.Help()
}

func handleClusterRunE(cmd *cobra.Command, _ []string) error {
	// Cobra Help() can return an error (e.g., output stream or template issues); wrap it for clarity.
	err := helpRunner(cmd)
	if err != nil {
		return fmt.Errorf("displaying cluster command help: %w", err)
	}

	return nil
}

// NewConnectCmd creates the connect command for clusters.
func NewConnectCmd(_ *di.Runtime) *cobra.Command {
	var editorFlag string

	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to cluster with k9s",
		Long: `Launch k9s terminal UI to interactively manage your Kubernetes cluster.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. EDITOR or VISUAL environment variables
  4. Fallback to vim, nano, or vi

All k9s flags and arguments are passed through unchanged, allowing you to use
any k9s functionality. Examples:

  ksail cluster connect
  ksail cluster connect --editor "code --wait"
  ksail cluster connect --namespace default
  ksail cluster connect --context my-context
  ksail cluster connect --readonly`,
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Hide flags that connect doesn't use but that are needed for config
	// defaults and validation (distribution, distributionConfig, gitopsEngine,
	// localRegistry).
	for _, flagName := range []string{"distribution", "distribution-config", "gitops-engine", "local-registry"} {
		if f := cmd.Flags().Lookup(flagName); f != nil {
			f.Hidden = true
		}
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return handleConnectRunE(cmd, cfgManager, args, editorFlag)
	}

	cmd.Flags().StringVar(
		&editorFlag,
		"editor",
		"",
		"editor command to use for k9s edit actions (e.g., 'code --wait', 'vim', 'nano')",
	)

	return cmd
}

// handleConnectRunE handles the connect command execution.
func handleConnectRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	args []string,
	editorFlag string,
) error {
	// Load configuration
	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	// Set up editor environment variables before connecting
	cleanup := setupEditorEnv(editorFlag, cfg)
	defer cleanup()

	// Get kubeconfig path with tilde expansion
	kubeConfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("get kubeconfig path: %w", err)
	}

	// Get context from config
	context := cfg.Spec.Cluster.Connection.Context

	// Create k9s client and command
	k9sClient := k9s.NewClient()
	k9sCmd := k9sClient.CreateConnectCommand(kubeConfigPath, context)

	// Transfer the context from parent command
	k9sCmd.SetContext(cmd.Context())

	// Set the args that were passed through
	k9sCmd.SetArgs(args)

	// Execute k9s command
	err = k9sCmd.Execute()
	if err != nil {
		return fmt.Errorf("execute k9s: %w", err)
	}

	return nil
}

// setupEditorEnv sets up the editor environment variables based on flag and config.
// It returns a cleanup function that should be called to restore the original environment.
func setupEditorEnv(editorFlag string, cfg *v1alpha1.Cluster) func() {
	// Create editor resolver
	resolver := editor.NewResolver(editorFlag, cfg)

	// Resolve the editor
	editorCmd := resolver.Resolve()

	// Set environment variables for connect command
	return resolver.SetEnvVars(editorCmd, "connect")
}

// NOTE: Some imports above (configmanager, k3dconfigmanager, kindconfigmanager,
// talosconfigmanager, etc.) are used by functions that remain in this file
// (loadClusterConfiguration, resolveClusterNameFromContext, etc.)

const (
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
	k3sDisableLocalStorageFlag  = "--disable=local-storage"
	k3sDisableServiceLBFlag     = "--disable=servicelb"
	k3sFlanelBackendNoneFlag    = "--flannel-backend=none"
	k3sDisableNetworkPolicyFlag = "--disable-network-policy"
)

// newCreateLifecycleConfig creates the lifecycle configuration for cluster creation.
func newCreateLifecycleConfig() lifecycle.Config {
	return lifecycle.Config{
		TitleEmoji:         "🚀",
		TitleContent:       "Create cluster...",
		ActivityContent:    "creating cluster",
		SuccessContent:     "cluster created",
		ErrorMessagePrefix: "failed to create cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.Provisioner, clusterName string) error {
			return provisioner.Create(ctx, clusterName)
		},
	}
}

// NewCreateCmd wires the cluster create command using the shared runtime container.
func NewCreateCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a cluster",
		Long:         `Create a Kubernetes cluster as defined by configuration.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cfgManager := setupMutationCmdFlags(cmd)

	cmd.Flags().String("ttl", "",
		"Auto-destroy cluster after duration (e.g. 1h, 30m, 2h30m). If not set, cluster persists indefinitely.")

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleCreateRunE)

	return cmd
}

// handleCreateRunE executes cluster creation with mirror registry setup and CNI installation.
//

func handleCreateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	deps.Timer.Start()

	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	err = runClusterCreationWorkflow(cmd, cfgManager, ctx, deps)
	if err != nil {
		return err
	}

	return maybeWaitForTTL(cmd, clusterName, ctx.ClusterCfg)
}

// newProvisionerFactory returns the cluster provisioner factory, using any test override if set.
func newProvisionerFactory(ctx *localregistry.Context) clusterprovisioner.Factory {
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		return factoryOverride
	}

	return clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:     ctx.KindConfig,
			K3d:      ctx.K3dConfig,
			Talos:    ctx.TalosConfig,
			VCluster: ctx.VClusterConfig,
			KWOK:     ctx.KWOKConfig,
			EKS:      ctx.EKSConfig,
		},
	}
}

// configureProvisionerFactory sets up the cluster provisioner factory on deps.
// Uses test override if available, otherwise creates a default factory.
func configureProvisionerFactory(
	deps *lifecycle.Deps,
	ctx *localregistry.Context,
) {
	deps.Factory = newProvisionerFactory(ctx)
}

// maybeImportCachedImages imports cached container images if configured.
// Logs warnings but does not fail cluster creation on import errors.
func maybeImportCachedImages(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	tmr timer.Timer,
) {
	importPath := ctx.ClusterCfg.Spec.Cluster.ImportImages
	if importPath == "" {
		return
	}

	// Image import is not supported for Talos and VCluster clusters
	if ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos ||
		ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionVCluster {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "image import is not supported for %s clusters; ignoring --import-images value %q",
			Args:    []any{ctx.ClusterCfg.Spec.Cluster.Distribution, importPath},
			Writer:  cmd.OutOrStderr(),
		})

		return
	}

	err := importCachedImages(cmd, ctx, importPath, tmr)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "failed to import images from %s: %v",
			Args:    []any{importPath, err},
			Writer:  cmd.OutOrStderr(),
		})
	}
}

func loadClusterConfiguration(
	cfgManager *ksailconfigmanager.ConfigManager,
	tmr timer.Timer,
) (*localregistry.Context, error) {
	// Load config to populate cfgManager.Config and cfgManager.DistributionConfig
	// The returned config is cached in cfgManager.Config, which is used by NewContextFromConfigManager
	_, err := cfgManager.Load(configmanager.LoadOptions{Timer: tmr})
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	// Create context from the now-populated config manager
	return localregistry.NewContextFromConfigManager(cfgManager), nil
}

// buildRegistryStageParams creates a StageParams struct for registry operations.
// This helper reduces code duplication when calling registry stage functions.
func buildRegistryStageParams(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) mirrorregistry.StageParams {
	return mirrorregistry.StageParams{
		Cmd:            cmd,
		ClusterCfg:     ctx.ClusterCfg,
		Deps:           deps,
		CfgManager:     cfgManager,
		KindConfig:     ctx.KindConfig,
		K3dConfig:      ctx.K3dConfig,
		TalosConfig:    ctx.TalosConfig,
		VClusterConfig: ctx.VClusterConfig,
		DockerInvoker:  localDeps.DockerInvoker,
	}
}

func validateRegistryForProvider(ctx *localregistry.Context) error {
	provider := ctx.ClusterCfg.Spec.Cluster.Provider

	registry := ctx.ClusterCfg.Spec.Cluster.LocalRegistry
	if provider.IsCloud() && registry.Enabled() && !registry.IsExternal() {
		return localregistry.ErrCloudProviderRequiresExternalRegistry
	}

	return nil
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) error {
	provider := ctx.ClusterCfg.Spec.Cluster.Provider

	// Cloud providers cannot use a Docker-based local registry — reject early with a clear error.
	err := validateRegistryForProvider(ctx)
	if err != nil {
		return err
	}

	if !provider.IsCloud() {
		// Stage 1: Provision local registry (skipped for external registries)
		err := localregistry.ExecuteStage(
			cmd,
			ctx,
			deps,
			localregistry.StageProvision,
			localDeps,
		)
		if err != nil {
			return fmt.Errorf("failed to provision local registry: %w", err)
		}
	}

	// Stage 2: Verify registry access.
	// Called unconditionally here, but VerifyRegistryAccess returns early unless an enabled
	// external registry is configured, in which case it validates any required registry auth.
	err = localregistry.VerifyRegistryAccess(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return fmt.Errorf("failed to verify registry access: %w", err)
	}

	if !provider.IsCloud() {
		params := buildRegistryStageParams(cmd, ctx, deps, cfgManager, localDeps)

		// Stage 3: Create and configure registry containers (local + mirrors)
		err = mirrorregistry.SetupRegistries(params)
		if err != nil {
			return fmt.Errorf("failed to setup registries: %w", err)
		}

		// Stage 4: Create Docker network
		err = mirrorregistry.CreateNetwork(params)
		if err != nil {
			return fmt.Errorf("failed to create docker network: %w", err)
		}

		// Stage 5: Connect registries to network (before cluster creation)
		err = mirrorregistry.ConnectRegistriesToNetwork(params)
		if err != nil {
			return fmt.Errorf("failed to connect registries to network: %w", err)
		}
	}

	return nil
}

func executeClusterLifecycle(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
) error {
	deps.Timer.NewStage()

	err := lifecycle.RunWithConfig(cmd, deps, newCreateLifecycleConfig(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to execute cluster lifecycle: %w", err)
	}

	return nil
}

func configureRegistryMirrorsInClusterWithWarning(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) {
	params := buildRegistryStageParams(cmd, ctx, deps, cfgManager, localDeps)

	// Configure containerd inside cluster nodes to use registry mirrors (Kind only)
	err := mirrorregistry.ConfigureRegistryMirrorsInCluster(params)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to configure registry mirrors in cluster: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// handlePostCreationSetup installs CNI, CSI, cert-manager, metrics-server, and GitOps engines.
func handlePostCreationSetup(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
) error {
	cniInstalled, err := setup.InstallCNI(cmd, clusterCfg, tmr)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	factories := getInstallerFactories()
	outputTimer := flags.MaybeTimer(cmd, tmr)

	// OCI artifact push is now handled inside InstallPostCNIComponents after Flux is installed
	err = setup.InstallPostCNIComponents(
		cmd,
		clusterCfg,
		factories,
		outputTimer,
		cniInstalled,
	)
	if err != nil {
		return fmt.Errorf("failed to install post-CNI components: %w", err)
	}

	return nil
}

// maybeDisableK3dFeature conditionally appends a K3s --disable flag to K3d config.
// It is a no-op when the distribution is not K3s, k3dConfig is nil, the feature
// is not in the disabled state, or the flag is already present.
func maybeDisableK3dFeature(
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	isDisabled bool,
	flag string,
) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	if !isDisabled {
		return
	}

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == flag {
			return
		}
	}

	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         flag,
			NodeFilters: []string{"server:*"},
		},
	)
}

// setupK3dCNI configures K3d to disable flannel and network policy when a non-default
// CNI (Cilium or Calico) is selected. Without this, K3s starts with flannel enabled,
// causing conflicts when the custom CNI is installed post-creation.
func setupK3dCNI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	cni := clusterCfg.Spec.Cluster.CNI
	if cni != v1alpha1.CNICilium && cni != v1alpha1.CNICalico {
		return
	}

	for _, flag := range []string{k3sFlanelBackendNoneFlag, k3sDisableNetworkPolicyFlag} {
		if !hasK3sArg(k3dConfig, flag) {
			k3dConfig.Options.K3sOptions.ExtraArgs = append(
				k3dConfig.Options.K3sOptions.ExtraArgs,
				v1alpha5.K3sArgWithNodeFilters{
					Arg:         flag,
					NodeFilters: []string{"server:*"},
				},
			)
		}
	}
}

// hasK3sArg checks whether a K3s arg flag is already present in the K3d config.
func hasK3sArg(k3dConfig *v1alpha5.SimpleConfig, flag string) bool {
	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == flag {
			return true
		}
	}

	return false
}

func setupK3dMetricsServer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	maybeDisableK3dFeature(
		clusterCfg, k3dConfig,
		clusterCfg.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerDisabled,
		k3sDisableMetricsServerFlag,
	)
}

// setupK3dCSI configures K3d to disable local-storage when CSI is explicitly disabled.
func setupK3dCSI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	maybeDisableK3dFeature(
		clusterCfg, k3dConfig,
		clusterCfg.Spec.Cluster.CSI == v1alpha1.CSIDisabled,
		k3sDisableLocalStorageFlag,
	)
}

// setupK3dLoadBalancer configures K3d to disable servicelb when LoadBalancer is explicitly disabled.
func setupK3dLoadBalancer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	maybeDisableK3dFeature(
		clusterCfg, k3dConfig,
		clusterCfg.Spec.Cluster.LoadBalancer == v1alpha1.LoadBalancerDisabled,
		k3sDisableServiceLBFlag,
	)
}

// setupVClusterCNI configures the vCluster to disable flannel when a non-default
// CNI (Cilium or Calico) is selected. Without this, the vCluster starts flannel,
// causing conflicts when the custom CNI is installed post-creation.
func setupVClusterCNI(
	clusterCfg *v1alpha1.Cluster,
	vclusterConfig *clusterprovisioner.VClusterConfig,
) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionVCluster {
		return
	}

	if vclusterConfig == nil {
		return
	}

	cni := clusterCfg.Spec.Cluster.CNI
	if cni != v1alpha1.CNICilium && cni != v1alpha1.CNICalico {
		return
	}

	vclusterConfig.DisableFlannel = true
}

// applyClusterNameOverride updates distribution configs with the cluster name override.
// This function mutates the distribution config pointers in ctx to apply the --name flag value.
// The name override takes highest priority over distribution config or context-derived names.
//
// For Talos, this regenerates the config bundle with the new cluster name because
// the cluster name is embedded in PKI certificates and the kubeconfig context name.
func applyClusterNameOverride(ctx *localregistry.Context, name string) error {
	if name == "" {
		return nil
	}

	// Update Kind config
	if ctx.KindConfig != nil {
		ctx.KindConfig.Name = name
	}

	// Update K3d config
	if ctx.K3dConfig != nil {
		ctx.K3dConfig.Name = name
	}

	// Update Talos config - must regenerate bundle for new cluster name
	// because cluster name is embedded in PKI and kubeconfig context
	if ctx.TalosConfig != nil {
		newConfig, err := ctx.TalosConfig.WithName(name)
		if err != nil {
			return fmt.Errorf("failed to apply cluster name override to Talos config: %w", err)
		}

		ctx.TalosConfig = newConfig
	}

	// Update VCluster config
	if ctx.VClusterConfig != nil {
		ctx.VClusterConfig.Name = name
	}

	// Update KWOK config
	if ctx.KWOKConfig != nil {
		ctx.KWOKConfig.Name = name
	}

	// Update the ksail.yaml context to match the distribution pattern
	if ctx.ClusterCfg != nil {
		dist := ctx.ClusterCfg.Spec.Cluster.Distribution
		ctx.ClusterCfg.Spec.Cluster.Connection.Context = dist.ContextName(name)
	}

	return nil
}

// importCachedImages imports container images from a tar archive to the cluster.
// This is called after cluster creation but before component installation to ensure
// CNI, CSI, metrics-server, and other components can use pre-loaded images.
func importCachedImages(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	importPath string,
	tmr timer.Timer,
) error {
	outputTimer := flags.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "📥",
		Content: "importing cached images from %s",
		Args:    []any{importPath},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Use the existing image import functionality
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	importer := imagesvc.NewImporter(dockerClient)

	// Resolve cluster name from distribution configs
	clusterName := resolveClusterNameFromContext(ctx)

	err = importer.Import(
		cmd.Context(),
		clusterName,
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
		imagesvc.ImportOptions{
			InputPath: importPath,
		},
	)
	if err != nil {
		return fmt.Errorf("import images: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "images imported successfully",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// resolveClusterNameFromContext determines the cluster name from distribution configs.
func resolveClusterNameFromContext(ctx *localregistry.Context) string {
	switch ctx.ClusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return kindconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.KindConfig)
	case v1alpha1.DistributionK3s:
		return k3dconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.K3dConfig)
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.TalosConfig)
	case v1alpha1.DistributionVCluster:
		return resolveVClusterName(ctx)
	case v1alpha1.DistributionKWOK:
		return resolveKWOKName(ctx)
	case v1alpha1.DistributionEKS:
		// EKS config is owned by eksctl (eks.yaml) and not cached on the
		// local registry context; fall back to the cluster-level name.
		return resolveFallbackName(ctx)
	default:
		return resolveFallbackName(ctx)
	}
}

func resolveVClusterName(ctx *localregistry.Context) string {
	if ctx.VClusterConfig != nil && ctx.VClusterConfig.Name != "" {
		return ctx.VClusterConfig.Name
	}

	return "vcluster-default"
}

func resolveKWOKName(ctx *localregistry.Context) string {
	if ctx.KWOKConfig != nil && ctx.KWOKConfig.Name != "" {
		return ctx.KWOKConfig.Name
	}

	return "kwok-default"
}

func resolveFallbackName(ctx *localregistry.Context) string {
	if name := strings.TrimSpace(ctx.ClusterCfg.Spec.Cluster.Connection.Context); name != "" {
		return name
	}

	return "ksail"
}

// maybeWaitForTTL parses the --ttl flag and, if set, blocks to auto-destroy the cluster
// after the TTL duration expires. TTL state is persisted for display in
// `ksail cluster list` and `ksail cluster info`, and the function then blocks by
// calling waitForTTLAndDelete until the cluster is removed or an error occurs.
func maybeWaitForTTL(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
) error {
	ttlStr, _ := cmd.Flags().GetString("ttl")
	if ttlStr == "" {
		return nil
	}

	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"invalid --ttl value %q: %v (cluster created without TTL)", ttlStr, err)

		return nil
	}

	if ttl <= 0 {
		return nil
	}

	// Persist TTL for informational display (ksail cluster list / info).
	saveErr := state.SaveClusterTTL(clusterName, ttl)
	if saveErr != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to save cluster TTL: %v", saveErr)
	}

	// Block and wait for TTL, then auto-destroy.
	return waitForTTLAndDelete(cmd, clusterName, clusterCfg, ttl)
}

const deleteLongDesc = `Destroy a cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

The kubeconfig is resolved in the following priority order:
  1. From --kubeconfig flag
  2. From KUBECONFIG environment variable
  3. From ksail.yaml config file (if present)
  4. Defaults to ~/.kube/config`

// deleteFlags holds all the flags for the delete command.
type deleteFlags struct {
	name       string
	provider   v1alpha1.Provider
	kubeconfig string
	storage    bool
	force      bool
}

// NewDeleteCmd creates and returns the delete command.
// Delete uses --name and --provider flags to determine the cluster to delete.
func NewDeleteCmd(runtimeContainer *di.Runtime) *cobra.Command {
	flags := &deleteFlags{}

	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          deleteLongDesc,
		SilenceUsage:  true,
		SilenceErrors: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeleteAction(cmd, runtimeContainer, flags)
		},
	}

	registerDeleteFlags(cmd, flags)

	return cmd
}

// registerDeleteFlags registers all flags for the delete command.
func registerDeleteFlags(cmd *cobra.Command, flags *deleteFlags) {
	cmd.Flags().StringVarP(&flags.name, "name", "n", "", "Name of the cluster to delete")
	cmd.Flags().VarP(&flags.provider, "provider", "p",
		fmt.Sprintf("Provider to use (%s)", flags.provider.ValidValues()))
	cmd.Flags().StringVarP(&flags.kubeconfig, "kubeconfig", "k", "",
		"Path to kubeconfig file for context cleanup")
	cmd.Flags().BoolVar(&flags.storage, "delete-storage", false,
		"Delete storage volumes when cleaning up (registry volumes for Docker, block storage for Hetzner)")
	cmd.Flags().BoolVarP(&flags.force, "force", "f", false,
		"Skip confirmation prompt and delete immediately")
}

// runDeleteAction executes the cluster deletion with registry cleanup.
func runDeleteAction(
	cmd *cobra.Command,
	runtimeContainer *di.Runtime,
	flags *deleteFlags,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	tmr := initTimer(runtimeContainer)

	// Resolve cluster info from flags, config, or kubeconfig
	resolved, err := lifecycle.ResolveClusterInfo(cmd, flags.name, flags.provider, flags.kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster info: %w", err)
	}

	// Detect cluster distribution and info before deletion
	// This must happen before deletion while kubeconfig is still available
	detectedInfo := detectClusterDistribution(resolved)
	isKindCluster := detectedInfo != nil &&
		detectedInfo.Distribution == v1alpha1.DistributionVanilla

	// Fallback: detect Kind cluster from container naming patterns if kubeconfig detection failed
	// This handles cases where kubeconfig context is missing but cluster containers exist
	if !isKindCluster && resolved.Provider == v1alpha1.ProviderDocker {
		nodes := discoverDockerNodes(cmd, resolved.ClusterName)
		isKindCluster = isKindClusterFromNodes(nodes, resolved.ClusterName)
	}

	// Create cluster info for provisioner creation, including detected distribution
	clusterInfo := &clusterdetector.Info{
		ClusterName:    resolved.ClusterName,
		Provider:       resolved.Provider,
		KubeconfigPath: resolved.KubeconfigPath,
	}
	if detectedInfo != nil {
		clusterInfo.Distribution = detectedInfo.Distribution
	}

	// Create provisioner for the provider
	provisioner, err := createDeleteProvisioner(clusterInfo, resolved.OmniOpts)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Pre-discover registries before deletion for Docker provider
	preDiscovered := prepareDockerDeletion(cmd, resolved, clusterInfo)

	// Show confirmation prompt unless force flag is set or non-TTY
	if !confirm.ShouldSkipPrompt(flags.force) {
		err := promptForDeletion(cmd, resolved, preDiscovered, isKindCluster)
		if err != nil {
			return err
		}
	}

	// Delete the cluster
	err = executeDelete(cmd, tmr, provisioner, resolved)
	if err != nil {
		return err
	}

	// Perform post-deletion cleanup
	performPostDeletionCleanup(cmd, tmr, resolved, flags, preDiscovered, isKindCluster)

	return nil
}

// detectClusterDistribution detects the distribution and other cluster info.
// This detection must happen before the cluster is deleted to ensure the kubeconfig
// entry is still available for reading cluster information.
// Returns nil if detection fails or the provider is not Docker.
func detectClusterDistribution(resolved *lifecycle.ResolvedClusterInfo) *clusterdetector.Info {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	name := strings.TrimSpace(resolved.ClusterName)

	// Each distribution uses a different kubeconfig context naming convention.
	prefixes := []string{
		"kind-",
		"k3d-",
		"vcluster-docker_",
		"kwok-",
	}

	for _, prefix := range prefixes {
		contextName := ""

		if name != "" {
			contextName = prefix + name
		}

		info, err := clusterdetector.DetectInfo(resolved.KubeconfigPath, contextName)
		if err == nil && info != nil {
			return info
		}
	}

	return nil
}

// prepareDockerDeletion prepares Docker-specific resources before deletion.
func prepareDockerDeletion(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	clusterInfo *clusterdetector.Info,
) *mirrorregistry.DiscoveredRegistries {
	if resolved.Provider != v1alpha1.ProviderDocker {
		return nil
	}

	preDiscovered := discoverRegistriesBeforeDelete(cmd, clusterInfo)
	disconnectRegistriesBeforeDelete(cmd, clusterInfo)

	return preDiscovered
}

// performPostDeletionCleanup handles all post-deletion cleanup tasks.
func performPostDeletionCleanup(
	cmd *cobra.Command,
	tmr timer.Timer,
	resolved *lifecycle.ResolvedClusterInfo,
	flags *deleteFlags,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) {
	// Cleanup registries after cluster deletion (only for Docker provider)
	if resolved.Provider == v1alpha1.ProviderDocker {
		cleanupRegistriesAfterDelete(cmd, tmr, resolved, flags.storage, preDiscovered)
	}

	// Cleanup cloud-provider-kind if this was the last kind cluster
	// Only run for Vanilla (Kind) distribution on Docker provider
	if isKindCluster {
		cleanupCloudProviderKindIfLastCluster(cmd, tmr)
	}
}

// initTimer initializes and starts the timer from the runtime container.
func initTimer(runtimeContainer *di.Runtime) timer.Timer {
	var tmr timer.Timer

	if runtimeContainer != nil {
		//nolint:wrapcheck // Error is captured to outer scope, not returned
		_ = runtimeContainer.Invoke(func(injector di.Injector) error {
			var err error

			tmr, err = di.ResolveTimer(injector)

			return err
		})
	}

	if tmr != nil {
		tmr.Start()
	}

	return tmr
}

// promptForDeletion shows the deletion preview and prompts for confirmation.
func promptForDeletion(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) error {
	preview := buildDeletionPreview(cmd, resolved, preDiscovered, isKindCluster)
	confirm.ShowDeletionPreview(cmd.OutOrStdout(), preview)

	if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
		return confirm.ErrDeletionCancelled
	}

	return nil
}

// createDeleteProvisioner creates the appropriate provisioner for cluster deletion.
// It first checks for test overrides, then falls back to creating a minimal provisioner.
func createDeleteProvisioner(
	clusterInfo *clusterdetector.Info,
	omniOpts v1alpha1.OptionsOmni,
) (clusterprovisioner.Provisioner, error) {
	// Check for test factory override
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		provisioner, _, err := factoryOverride.Create(context.Background(), nil)
		if err != nil {
			return nil, fmt.Errorf("factory override failed: %w", err)
		}

		return provisioner, nil
	}

	provisioner, err := lifecycle.CreateMinimalProvisionerForProvider(clusterInfo, omniOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner for provider: %w", err)
	}

	return provisioner, nil
}

// discoverRegistriesBeforeDelete discovers registries connected to the cluster network.
// This must be called BEFORE cluster deletion for Docker-based clusters.
func discoverRegistriesBeforeDelete(
	cmd *cobra.Command,
	clusterInfo *clusterdetector.Info,
) *mirrorregistry.DiscoveredRegistries {
	cleanupDeps := getCleanupDeps()

	// Use the detected distribution for correct network name resolution
	// Kind uses fixed "kind" network, Talos uses cluster name as network name
	distribution := clusterInfo.Distribution
	if distribution == "" {
		// Fallback to Talos if distribution is unknown (uses cluster name as network)
		distribution = v1alpha1.DistributionTalos
	}

	return mirrorregistry.DiscoverRegistriesByNetwork(
		cmd,
		distribution,
		clusterInfo.ClusterName,
		cleanupDeps,
	)
}

// disconnectRegistriesBeforeDelete disconnects registries from the cluster network.
// This is required for distributions like Talos and VCluster because they destroy
// the network during deletion, and the deletion will fail if containers are still
// connected to the network.
func disconnectRegistriesBeforeDelete(
	cmd *cobra.Command,
	clusterInfo *clusterdetector.Info,
) {
	cleanupDeps := getCleanupDeps()

	// Resolve the distribution-specific network name
	distribution := clusterInfo.Distribution
	if distribution == "" {
		distribution = v1alpha1.DistributionTalos
	}

	networkName := mirrorregistry.GetNetworkNameForDistribution(
		distribution,
		clusterInfo.ClusterName,
	)

	// Silently disconnect registries - errors are ignored since the cluster
	// may not have any registries connected, or the network may not exist
	_ = mirrorregistry.DisconnectRegistriesFromNetwork(cmd, networkName, cleanupDeps)
}

// buildDeletionPreview builds a preview of resources that will be deleted.
func buildDeletionPreview(
	cmd *cobra.Command,
	resolved *lifecycle.ResolvedClusterInfo,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
	isKindCluster bool,
) *confirm.DeletionPreview {
	preview := &confirm.DeletionPreview{
		ClusterName: resolved.ClusterName,
		Provider:    resolved.Provider,
	}

	switch resolved.Provider {
	case v1alpha1.ProviderDocker:
		// Collect registry names
		if preDiscovered != nil {
			for _, reg := range preDiscovered.Registries {
				preview.Registries = append(preview.Registries, reg.Name)
			}
		}

		// Try to discover cluster node containers
		preview.Nodes = discoverDockerNodes(cmd, resolved.ClusterName)

		// If this is the last Kind cluster, show shared containers that will be deleted
		if isKindCluster && countKindClusters(cmd) == 1 {
			preview.SharedContainers = listCloudProviderKindContainerNames(cmd)
		}
	case v1alpha1.ProviderHetzner:
		// For Hetzner, resources follow predictable naming patterns
		// Note: We can't list actual servers without API access, but we know infrastructure resources
		preview.PlacementGroup = resolved.ClusterName + "-placement"
		preview.Firewall = resolved.ClusterName + "-firewall"
		preview.Network = resolved.ClusterName + "-network"
		// Servers are labeled but we don't have API access here to list them
		// Add a placeholder to indicate servers will be deleted
		serverPlaceholder := "(all servers labeled with cluster: " + resolved.ClusterName + ")"
		preview.Servers = []string{serverPlaceholder}
	case v1alpha1.ProviderOmni:
		// For Omni, the cluster resource will be destroyed which deallocates all machines
		machinePlaceholder := "(all machines allocated to cluster: " + resolved.ClusterName + ")"
		preview.Servers = []string{machinePlaceholder}
	case v1alpha1.ProviderAWS:
		// For AWS/EKS, deletion is delegated to eksctl which tears down the
		// CloudFormation stacks owning the control plane and managed nodegroups.
		eksPlaceholder := "(EKS cluster and managed nodegroups for: " + resolved.ClusterName + ")"
		preview.Servers = []string{eksPlaceholder}
	}

	return preview
}

// executeDelete performs the cluster deletion operation.
func executeDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	provisioner clusterprovisioner.Provisioner,
	resolved *lifecycle.ResolvedClusterInfo,
) error {
	if tmr != nil {
		tmr.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Delete cluster...",
		Emoji:   "🗑️",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type: notify.ActivityType,
		Content: fmt.Sprintf(
			"deleting cluster '%s' on %s",
			resolved.ClusterName,
			resolved.Provider,
		),
		Writer: cmd.OutOrStdout(),
	})

	// Check if cluster exists
	exists, err := provisioner.Exists(cmd.Context(), resolved.ClusterName)
	if err != nil {
		return fmt.Errorf("check cluster existence: %w", err)
	}

	if !exists {
		return clustererr.ErrClusterNotFound
	}

	// Delete the cluster
	err = provisioner.Delete(cmd.Context(), resolved.ClusterName)
	if err != nil {
		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	// Clean up persisted state (spec + TTL) for the deleted cluster.
	// Best-effort: log a warning on failure rather than blocking success.
	stateErr := state.DeleteClusterState(resolved.ClusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(), "failed to clean up cluster state: %v", stateErr)
	}

	outputTimer := flags.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// cleanupRegistriesAfterDelete cleans up registries after cluster deletion.
func cleanupRegistriesAfterDelete(
	cmd *cobra.Command,
	tmr timer.Timer,
	resolved *lifecycle.ResolvedClusterInfo,
	deleteStorage bool,
	preDiscovered *mirrorregistry.DiscoveredRegistries,
) {
	cleanupDeps := getCleanupDeps()

	var err error
	if preDiscovered != nil && len(preDiscovered.Registries) > 0 {
		// Use pre-discovered registries
		err = mirrorregistry.CleanupPreDiscoveredRegistries(
			cmd,
			tmr,
			preDiscovered.Registries,
			deleteStorage,
			cleanupDeps,
		)
	} else {
		// Discover and cleanup registries by network
		// Use Talos as fallback since it uses cluster name as network name
		err = mirrorregistry.CleanupRegistriesByNetwork(
			cmd,
			tmr,
			v1alpha1.DistributionTalos,
			resolved.ClusterName,
			deleteStorage,
			cleanupDeps,
		)
	}

	if err != nil && !errors.Is(err, mirrorregistry.ErrNoRegistriesFound) {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// discoverDockerNodes discovers cluster node containers for Docker provider.
// Kind uses: {cluster}-control-plane, {cluster}-worker, etc.
// K3d uses: k3d-{cluster}-server-0, k3d-{cluster}-agent-0, etc.
// Talos uses: {cluster}-controlplane-*, {cluster}-worker-*.
func discoverDockerNodes(cmd *cobra.Command, clusterName string) []string {
	var nodes []string

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if IsClusterContainer(containerName, clusterName) {
			nodes = append(nodes, containerName)
		}

		return false // continue processing all containers
	})

	return nodes
}

// IsClusterContainer checks if a container name belongs to the given cluster.
// Exported for testing.
func IsClusterContainer(containerName, clusterName string) bool {
	// Kind pattern: {cluster}-control-plane, {cluster}-worker, {cluster}-worker{N}
	// Check for exact prefixes with valid suffixes to avoid partial cluster name matches
	if matchesKindPattern(containerName, clusterName) {
		return true
	}

	// K3d pattern: k3d-{cluster}-server-*, k3d-{cluster}-agent-*
	if strings.HasPrefix(containerName, "k3d-"+clusterName+"-server-") ||
		strings.HasPrefix(containerName, "k3d-"+clusterName+"-agent-") {
		return true
	}

	// Talos pattern: {cluster}-controlplane-*, {cluster}-worker-*
	if strings.HasPrefix(containerName, clusterName+"-controlplane-") ||
		strings.HasPrefix(containerName, clusterName+"-worker-") {
		return true
	}

	// VCluster pattern: vcluster.cp.{cluster}
	if containerName == "vcluster.cp."+clusterName {
		return true
	}

	return false
}

// isKindClusterFromNodes determines if a cluster is a Kind cluster by checking
// if any of its nodes match Kind's container naming convention.
// This is used as a fallback when kubeconfig-based detection fails.
func isKindClusterFromNodes(nodes []string, clusterName string) bool {
	for _, node := range nodes {
		if matchesKindPattern(node, clusterName) {
			return true
		}
	}

	return false
}

// matchesKindPattern checks if container matches Kind's naming convention.
// Kind uses: {cluster}-control-plane, {cluster}-worker, {cluster}-worker{N}.
func matchesKindPattern(containerName, clusterName string) bool {
	// Check control-plane (exact suffix)
	if containerName == clusterName+"-control-plane" {
		return true
	}

	// Check worker nodes: {cluster}-worker or {cluster}-worker{N}
	workerPrefix := clusterName + "-worker"
	if containerName == workerPrefix {
		return true
	}

	// Check for numbered workers: {cluster}-worker2, {cluster}-worker3, etc.
	if strings.HasPrefix(containerName, workerPrefix) {
		suffix := containerName[len(workerPrefix):]
		// Suffix must be a number for valid worker nodes
		if suffix != "" && isNumericString(suffix) {
			return true
		}
	}

	return false
}

// isNumericString checks if a non-empty string contains only digits.
func isNumericString(s string) bool {
	if len(s) == 0 {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// isCloudProviderKindContainer checks if a container name belongs to cloud-provider-kind.
func isCloudProviderKindContainer(name string) bool {
	return name == "ksail-cloud-provider-kind" || strings.HasPrefix(name, "cpk-")
}

// hasRemainingKindClusters checks if there are any Kind clusters remaining in Docker.
func hasRemainingKindClusters(cmd *cobra.Command) bool {
	return countKindClusters(cmd) > 0
}

// hasCloudProviderKindContainers checks if there are any cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func hasCloudProviderKindContainers(cmd *cobra.Command) bool {
	return len(listCloudProviderKindContainerNames(cmd)) > 0
}

// listCloudProviderKindContainerNames returns the names of all cloud-provider-kind containers.
// This includes both the main ksail-cloud-provider-kind controller and cpk-* service containers.
func listCloudProviderKindContainerNames(cmd *cobra.Command) []string {
	var names []string

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if isCloudProviderKindContainer(containerName) {
			names = append(names, containerName)
		}

		return false // continue processing all containers
	})

	return names
}

// countKindClusters counts the number of Kind clusters currently running.
// This is determined by counting containers with the -control-plane suffix.
func countKindClusters(cmd *cobra.Command) int {
	var count int

	_ = forEachContainerName(cmd, func(containerName string) bool {
		if strings.HasSuffix(containerName, "-control-plane") {
			count++
		}

		return false // continue processing all containers
	})

	return count
}

// cleanupCloudProviderKindIfLastCluster uninstalls cloud-provider-kind if no kind clusters remain.
// Cloud-provider-kind creates containers that can be shared across multiple kind clusters,
// so we only uninstall when the last kind cluster is deleted.
func cleanupCloudProviderKindIfLastCluster(
	cmd *cobra.Command,
	tmr timer.Timer,
) {
	// Check if there are any remaining Kind clusters by looking for Kind containers
	if hasRemainingKindClusters(cmd) {
		return
	}

	// Check if there are any cloud-provider-kind containers to clean up
	if !hasCloudProviderKindContainers(cmd) {
		return
	}

	// No kind clusters remain - proceed with cloud-provider-kind cleanup
	if tmr != nil {
		tmr.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Cleanup cloud-provider-kind...",
		Emoji:   "🧹",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "uninstalling cloud-provider-kind (no kind clusters remain)",
		Writer:  cmd.OutOrStdout(),
	})

	// We need to uninstall from one of the recently deleted clusters
	// Since all clusters are gone, we can't actually uninstall via Helm
	// Instead, we need to clean up any remaining cloud-provider-kind containers
	cleanupErr := cleanupCloudProviderKindContainers(cmd)
	if cleanupErr != nil {
		notify.WriteMessage(notify.Message{
			Type: notify.WarningType,
			Content: fmt.Sprintf(
				"failed to cleanup cloud-provider-kind containers: %v",
				cleanupErr,
			),
			Writer: cmd.OutOrStdout(),
		})

		return
	}

	outputTimer := flags.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cloud-provider-kind cleaned up",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})
}

// cleanupCloudProviderKindContainers removes any cloud-provider-kind related containers.
// This includes:
// - The main ksail-cloud-provider-kind controller container
// - Any cpk-* containers created by cloud-provider-kind for LoadBalancer services.
func cleanupCloudProviderKindContainers(cmd *cobra.Command) error {
	return forEachContainer(
		cmd,
		func(dockerClient client.APIClient, ctr container.Summary, name string) error {
			if !isCloudProviderKindContainer(name) {
				return nil
			}

			err := dockerClient.ContainerRemove(
				cmd.Context(),
				ctr.ID,
				container.RemoveOptions{Force: true},
			)
			if err != nil {
				return fmt.Errorf("failed to remove container %s: %w", name, err)
			}

			return nil
		},
	)
}

// Package-level dependencies for cluster commands.
// These variables support dependency injection for testing while providing production defaults.
// Use the Set*ForTests functions in testing.go to override these values in tests.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverrideMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverride *setup.InstallerFactories
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvokerMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryOverride clusterprovisioner.Factory
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvoker = dockerutil.WithDockerClient
	//nolint:gochecknoglobals // dependency injection for tests
	localRegistryServiceFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	localRegistryServiceFactory localregistry.ServiceFactoryFunc
)

// errStopIteration is a sentinel error used to stop container iteration early.
var errStopIteration = errors.New("stop iteration")

// getInstallerFactories returns the installer factories to use, allowing test override.
func getInstallerFactories() *setup.InstallerFactories {
	installerFactoriesOverrideMu.RLock()
	defer installerFactoriesOverrideMu.RUnlock()

	if installerFactoriesOverride != nil {
		return installerFactoriesOverride
	}

	return setup.DefaultInstallerFactories()
}

// getLocalRegistryDeps returns the local registry dependencies, respecting any test overrides.
func getLocalRegistryDeps() localregistry.Dependencies {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	opts := []localregistry.Option{
		localregistry.WithDockerInvoker(invoker),
	}

	localRegistryServiceFactoryMu.RLock()

	factory := localRegistryServiceFactory

	localRegistryServiceFactoryMu.RUnlock()

	if factory != nil {
		opts = append(opts, localregistry.WithServiceFactory(factory))
	}

	return localregistry.NewDependencies(opts...)
}

// getCleanupDeps returns the cleanup dependencies for mirror registry operations.
func getCleanupDeps() mirrorregistry.CleanupDependencies {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	return mirrorregistry.CleanupDependencies{
		DockerInvoker:     invoker,
		LocalRegistryDeps: getLocalRegistryDeps(),
	}
}

// withDockerClient executes an operation with the Docker client, handling locking and invoker retrieval.
// This is the canonical way to access Docker in this package, ensuring thread-safe access to the invoker.
func withDockerClient(cmd *cobra.Command, operation func(client.APIClient) error) error {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	return invoker(cmd, operation)
}

// forEachContainerName lists all Docker containers and calls the provided function for each container name.
// The function receives the normalized container name (without leading slash).
// Container processing stops early if the callback returns true (indicating done).
func forEachContainerName(
	cmd *cobra.Command,
	callback func(containerName string) (done bool),
) error {
	return forEachContainer(
		cmd,
		func(_ client.APIClient, _ container.Summary, name string) error {
			if callback(name) {
				return errStopIteration
			}

			return nil
		},
	)
}

// forEachContainer lists all Docker containers and calls the callback for each container name.
// The callback receives the docker client, container info, and normalized container name.
// Return an error to stop iteration (use errStopIteration for normal early exit).
func forEachContainer(
	cmd *cobra.Command,
	callback func(dockerClient client.APIClient, ctr container.Summary, name string) error,
) error {
	return withDockerClient(cmd, func(dockerClient client.APIClient) error {
		containers, err := dockerClient.ContainerList(cmd.Context(), container.ListOptions{
			All: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		for _, ctr := range containers {
			for _, name := range ctr.Names {
				containerName := strings.TrimPrefix(name, "/")

				err := callback(dockerClient, ctr, containerName)
				if err != nil {
					if errors.Is(err, errStopIteration) {
						return nil // Normal early exit
					}

					return err
				}
			}
		}

		return nil
	})
}

// errNoClusterInfo is a sentinel error returned when no information is available
// from any source (provider API or Kubernetes API).
var errNoClusterInfo = errors.New("no cluster info available")

// errUnsupportedProvider is a sentinel error for unrecognized provider values.
var errUnsupportedProvider = errors.New("unsupported provider")

// errProviderNotConfigured is returned when provider credentials are missing.
var errProviderNotConfigured = errors.New("provider not configured")

// NewInfoCmd creates the cluster info command.
// The command queries the infrastructure provider API first, then attempts
// kubectl cluster-info, and only fails if no information is available at all.
func NewInfoCmd(_ *di.Runtime) *cobra.Command {
	var (
		nameFlag     string
		providerFlag v1alpha1.Provider
	)

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display cluster information",
		Long: "Display cluster information from the infrastructure provider" +
			" and Kubernetes API. Succeeds if information is available from any source.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfoCmd(cmd, nameFlag, providerFlag)
		},
	}

	cmd.Flags().StringVarP(
		&nameFlag,
		"name",
		"n",
		"",
		"Name of the cluster to target",
	)

	cmd.Flags().VarP(
		&providerFlag,
		"provider",
		"p",
		fmt.Sprintf("Provider to use (%s)", providerFlag.ValidValues()),
	)

	return cmd
}

// runInfoCmd orchestrates the cluster info command flow:
// 1. Resolve cluster identity (name, provider, kubeconfig)
// 2. Query provider API for cluster status
// 3. Attempt kubectl cluster-info
// 4. Display combined results
// 5. Return nil (exit 0) if any info available, error (exit 1) if nothing.
func runInfoCmd(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
) error {
	resolved, err := lifecycle.ResolveClusterInfo(
		cmd, nameFlag, providerFlag, "",
	)
	if err != nil {
		return fmt.Errorf("resolve cluster info: %w", err)
	}

	writer := cmd.OutOrStdout()

	// Phase 1: Query provider API
	status, provErr := getProviderStatus(
		cmd,
		resolved.Provider,
		resolved.ClusterName,
		resolved.OmniOpts,
	)

	if errors.Is(provErr, errUnsupportedProvider) {
		return provErr
	}

	provErr = classifyProviderError(provErr)

	hasProviderInfo := provErr == nil && status != nil
	if hasProviderInfo {
		displayProviderStatus(writer, resolved.Provider, resolved.ClusterName, status)
	}

	// Phase 2: Attempt kubectl cluster-info
	kubeErr := tryKubeClusterInfo(cmd, resolved.KubeconfigPath)
	hasKubeInfo := kubeErr == nil

	if !hasKubeInfo && hasProviderInfo {
		_, _ = fmt.Fprintln(writer)
		_, _ = fmt.Fprintln(writer, "  Kubernetes API: unreachable")
	}

	// Phase 3: Append KSail details (TTL, components)
	if hasProviderInfo || hasKubeInfo {
		displayKSailDetails(cmd, resolved.KubeconfigPath)

		return nil
	}

	return buildNoInfoError(resolved.ClusterName, provErr)
}

// classifyProviderError returns nil for soft errors that mean "no provider info"
// (missing credentials, cluster not found) and passes through real errors.
func classifyProviderError(err error) error {
	if errors.Is(err, errProviderNotConfigured) ||
		errors.Is(err, provider.ErrClusterNotFound) {
		return nil
	}

	return err
}

// buildNoInfoError creates the final error when no info is available.
func buildNoInfoError(clusterName string, provErr error) error {
	if provErr != nil {
		return fmt.Errorf(
			"%w for %q: provider: %w",
			errNoClusterInfo,
			clusterName,
			provErr,
		)
	}

	return fmt.Errorf(
		"%w for %q",
		errNoClusterInfo,
		clusterName,
	)
}

// getProviderStatus queries the infrastructure provider for cluster status.
// Returns nil status if the cluster doesn't exist in the provider.
func getProviderStatus(
	cmd *cobra.Command,
	prov v1alpha1.Provider,
	clusterName string,
	omniOpts v1alpha1.OptionsOmni,
) (*provider.ClusterStatus, error) {
	switch prov {
	case v1alpha1.ProviderDocker, "":
		return getDockerProviderStatus(cmd, clusterName)
	case v1alpha1.ProviderHetzner:
		return getHetznerProviderStatus(cmd.Context(), clusterName)
	case v1alpha1.ProviderOmni:
		return getOmniProviderStatus(cmd.Context(), clusterName, omniOpts)
	case v1alpha1.ProviderAWS:
		// AWS/EKS status is derived from the EKS API through the provisioner,
		// not from local container inspection. Return a minimal stub so callers
		// that rely on this helper do not fail for EKS.
		return &provider.ClusterStatus{Phase: "unknown"}, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedProvider, prov)
	}
}

// getDockerProviderStatus queries Docker for cluster status by trying all label schemes.
func getDockerProviderStatus(
	cmd *cobra.Command,
	clusterName string,
) (*provider.ClusterStatus, error) {
	var result *provider.ClusterStatus

	err := withDockerClient(cmd, func(dockerClient client.APIClient) error {
		schemes := []dockerprovider.LabelScheme{
			dockerprovider.LabelSchemeKind,
			dockerprovider.LabelSchemeK3d,
			dockerprovider.LabelSchemeTalos,
			dockerprovider.LabelSchemeVCluster,
			dockerprovider.LabelSchemeKWOK,
		}

		for _, scheme := range schemes {
			prov := dockerprovider.NewProvider(dockerClient, scheme)

			status, err := prov.GetClusterStatus(cmd.Context(), clusterName)
			if err != nil {
				if errors.Is(err, provider.ErrClusterNotFound) {
					continue
				}

				return fmt.Errorf(
					"docker label scheme %s: %w", scheme, err,
				)
			}

			if status != nil && status.NodesTotal > 0 {
				result = status

				return nil
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("docker provider status: %w", err)
	}

	return result, nil
}

// getHetznerProviderStatus queries Hetzner Cloud for cluster status.
func getHetznerProviderStatus(
	ctx context.Context,
	clusterName string,
) (*provider.ClusterStatus, error) {
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("HCLOUD_TOKEN: %w", errProviderNotConfigured)
	}

	hetznerClient := hcloud.NewClient(hcloud.WithToken(token))
	prov := hetzner.NewProvider(hetznerClient)

	result, err := prov.GetClusterStatus(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("hetzner provider status: %w", err)
	}

	return result, nil
}

// getOmniProviderStatus queries Omni for cluster status.
func getOmniProviderStatus(
	ctx context.Context,
	clusterName string,
	omniOpts v1alpha1.OptionsOmni,
) (*provider.ClusterStatus, error) {
	omniProvider, err := omni.NewProviderFromOptions(omniOpts)
	if err != nil {
		if errors.Is(err, omni.ErrEndpointRequired) ||
			errors.Is(err, omni.ErrServiceAccountKeyRequired) {
			return nil, fmt.Errorf(
				"%w: %w", errProviderNotConfigured, err,
			)
		}

		return nil, fmt.Errorf("omni provider: %w", err)
	}

	result, err := omniProvider.GetClusterStatus(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("omni provider status: %w", err)
	}

	return result, nil
}

// displayProviderStatus prints the provider-level cluster status.
func displayProviderStatus(
	writer io.Writer,
	prov v1alpha1.Provider,
	clusterName string,
	status *provider.ClusterStatus,
) {
	_, _ = fmt.Fprintf(writer, "Provider:     %s\n", prov)
	_, _ = fmt.Fprintf(writer, "Cluster:      %s\n", clusterName)

	if status.Endpoint != "" {
		_, _ = fmt.Fprintf(writer, "Endpoint:     %s\n", status.Endpoint)
	}

	_, _ = fmt.Fprintf(writer, "Status:       %s\n", strings.ToUpper(status.Phase))
	_, _ = fmt.Fprintf(writer, "Ready:        %d/%d (ready/total)\n",
		status.NodesReady, status.NodesTotal)

	if len(status.Nodes) > 0 {
		_, _ = fmt.Fprintln(writer, "Nodes:")

		for _, node := range status.Nodes {
			_, _ = fmt.Fprintf(writer, "  - %-40s %-15s %s\n",
				node.Name, node.Role, node.State)
		}
	}
}

// Retry configuration for kubectl cluster-info.
// The API server may not be ready immediately after cluster creation
// (e.g., K3d reports "created successfully" before K3s API is reachable).
const (
	clusterInfoMaxAttempts = 3
	clusterInfoRetryDelay  = 2 * time.Second
)

// tryKubeClusterInfo attempts kubectl cluster-info with retries and writes
// output to cmd's writer. Output is buffered during retries so that failed
// attempts do not leak partial output. Returns nil on success, an error if
// the Kubernetes API is unreachable after all attempts.
func tryKubeClusterInfo(cmd *cobra.Command, kubeconfigPath string) error {
	var lastErr error

	for attempt := 1; attempt <= clusterInfoMaxAttempts; attempt++ {
		var buf bytes.Buffer

		kubectlClient := kubectl.NewClient(genericiooptions.IOStreams{
			In:     os.Stdin,
			Out:    &buf,
			ErrOut: io.Discard,
		})

		kubeCmd := kubectlClient.CreateClusterInfoCommand(kubeconfigPath)

		// Suppress kubectl's own error output
		kubeCmd.SetErr(io.Discard)
		kubeCmd.SilenceErrors = true
		kubeCmd.SilenceUsage = true
		// Prevent Cobra from parsing the parent ksail command's os.Args.
		kubeCmd.SetArgs([]string{})

		_, lastErr = kubeCmd.ExecuteC()
		if lastErr == nil {
			// Success — flush buffered output to the real writer.
			_, _ = io.Copy(cmd.OutOrStdout(), &buf)

			return nil
		}

		if attempt < clusterInfoMaxAttempts {
			select {
			case <-time.After(clusterInfoRetryDelay):
			case <-cmd.Context().Done():
				return fmt.Errorf("kubectl cluster-info cancelled: %w", cmd.Context().Err())
			}
		}
	}

	return fmt.Errorf("kubectl cluster-info failed after %d attempts: %w",
		clusterInfoMaxAttempts, lastErr)
}

// displayKSailDetails appends KSail-specific cluster metadata after kubectl output.
// This includes cluster identity (name, distribution, provider), TTL status,
// and enabled component summary from persisted state. Each section fails gracefully.
func displayKSailDetails(cmd *cobra.Command, kubeconfigPath string) {
	info, err := clusterdetector.DetectInfo(kubeconfigPath, "")
	if err != nil || info == nil {
		// If detection fails, skip KSail details because cluster identity could not be determined.
		return
	}

	writer := cmd.OutOrStdout()

	// Blank line to separate from kubectl output.
	_, _ = fmt.Fprintln(writer)

	displayClusterIdentity(writer, info)
	displayTTLInfo(writer, info.ClusterName)
	displayComponents(writer, info.ClusterName)
}

// displayClusterIdentity prints the cluster name, distribution, provider, kubeconfig context,
// server URL, and kubeconfig path.
func displayClusterIdentity(writer io.Writer, info *clusterdetector.Info) {
	_, _ = fmt.Fprintln(writer, "KSail Cluster Details:")
	_, _ = fmt.Fprintf(writer, "  Cluster:        %s\n", info.ClusterName)
	_, _ = fmt.Fprintf(writer, "  Distribution:   %s\n", info.Distribution)
	_, _ = fmt.Fprintf(writer, "  Provider:       %s\n", info.Provider)

	if info.Context != "" {
		_, _ = fmt.Fprintf(writer, "  Context:        %s\n", info.Context)
	}

	if info.ServerURL != "" {
		_, _ = fmt.Fprintf(writer, "  Server:         %s\n", info.ServerURL)
	}

	if info.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(writer, "  Kubeconfig:     %s\n", info.KubeconfigPath)
	}
}

// displayTTLInfo prints TTL status if set.
func displayTTLInfo(writer io.Writer, clusterName string) {
	ttlInfo, err := state.LoadClusterTTL(clusterName)
	if err != nil || ttlInfo == nil {
		return
	}

	_, _ = fmt.Fprintln(writer)

	remaining := ttlInfo.Remaining()
	if remaining <= 0 {
		notify.Warningf(writer,
			"cluster TTL has EXPIRED (was set to %s)", ttlInfo.Duration)
	} else {
		notify.Infof(
			writer,
			"cluster TTL: %s remaining (set to %s)",
			formatRemainingDuration(remaining),
			ttlInfo.Duration,
		)
	}
}

// displayComponents loads the persisted ClusterSpec and prints the enabled components summary.
func displayComponents(writer io.Writer, clusterName string) {
	spec, err := state.LoadClusterSpec(clusterName)
	if err != nil {
		return
	}

	type row struct{ label, value string }

	rows := []row{
		{"GitOps Engine:", componentLabel(string(spec.GitOpsEngine))},
		{"CNI:", componentLabel(string(spec.CNI))},
		{"CSI:", componentLabel(string(spec.CSI))},
		{"Metrics Server:", componentLabel(string(spec.MetricsServer))},
		{"Load Balancer:", componentLabel(string(spec.LoadBalancer))},
		{"Cert Manager:", componentLabel(string(spec.CertManager))},
		{"Policy Engine:", componentLabel(string(spec.PolicyEngine))},
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "  Components:")

	for _, r := range rows {
		_, _ = fmt.Fprintf(writer, "    %-16s%s\n", r.label, r.value)
	}
}

// componentLabel returns a display label for a component value.
// Empty strings and "None" sentinel values are shown as "(none)".
// "Disabled" sentinel values (used by CSI, MetricsServer, CertManager, etc.) are shown as "(disabled)".
func componentLabel(value string) string {
	switch value {
	case "":
		return "(none)"
	case "None":
		return "(none)"
	case "Disabled":
		return "(disabled)"
	default:
		return value
	}
}

// NewInitCmd creates and returns the init command.
func NewInitCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize a new project",
		Long:         "Initialize a new project in the specified directory (or current directory if none specified).",
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, InitFieldSelectors())

	// Bind init-local flags (not part of shared cluster config). Keeping this scoped
	// here avoids polluting the generic config manager with scaffolding concerns.
	bindInitLocalFlags(cmd, cfgManager)

	cmd.RunE = di.RunEWithRuntime(
		runtimeContainer,
		di.WithTimer(func(cmd *cobra.Command, _ di.Injector, tmr timer.Timer) error {
			deps := InitDeps{Timer: tmr}

			return HandleInitRunE(cmd, cfgManager, deps)
		}),
	)

	return cmd
}

// InitFieldSelectors returns the field selectors used by the init command.
// Kept local (rather than separate file) to keep init-specific wiring cohesive.
func InitFieldSelectors() []ksailconfigmanager.FieldSelector[v1alpha1.Cluster] {
	selectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	selectors = append(selectors, ksailconfigmanager.DefaultProviderFieldSelector())
	selectors = append(selectors, ksailconfigmanager.StandardSourceDirectoryFieldSelector())
	selectors = append(selectors, ksailconfigmanager.StandardKustomizationFileFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCNIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCSIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCDIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultLoadBalancerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultPolicyEngineFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultImportImagesFieldSelector())
	// Unified node count selectors for all distributions
	selectors = append(selectors, ksailconfigmanager.ControlPlanesFieldSelector())
	selectors = append(selectors, ksailconfigmanager.WorkersFieldSelector())
	// Talos-specific selectors
	selectors = append(selectors, ksailconfigmanager.ImageVerificationFieldSelector())

	return selectors
}

// bindInitLocalFlags adds and binds flags that are specific to the init command only.
// They intentionally do not belong to the shared cluster field selectors.
func bindInitLocalFlags(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().StringP("output", "o", "", "Output directory for the project")
	_ = cfgManager.Viper.BindPFlag("output", cmd.Flags().Lookup("output"))
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))
	cmd.Flags().StringSlice(
		"mirror-registry",
		[]string{},
		"Configure mirror registries with optional authentication. Format: [user:pass@]host[=upstream]. "+
			"Credentials support environment variables using ${VAR} syntax (quote placeholders so KSail can expand them). "+
			"Examples: docker.io=https://registry-1.docker.io, '${USER}:${TOKEN}@ghcr.io=https://ghcr.io'",
	)
	// NOTE: mirror-registry is NOT bound to Viper to allow custom merge logic
	// It's handled manually in mirrorregistry.GetMirrorRegistriesWithDefaults()
	cmd.Flags().StringP(
		"name",
		"n",
		"",
		"Cluster name used for container names, registry names, and kubeconfig context",
	)
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))
}

// InitDeps captures dependencies required for the init command.
type InitDeps struct {
	Timer timer.Timer
}

// validateInitConfig validates the cluster configuration for the init command.
func validateInitConfig(clusterCfg *v1alpha1.Cluster) error {
	// Early validation of distribution x provider combination
	err := clusterCfg.Spec.Cluster.Provider.ValidateForDistribution(
		clusterCfg.Spec.Cluster.Distribution,
	)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate local registry configuration for the provider
	err = v1alpha1.ValidateLocalRegistryForProvider(
		clusterCfg.Spec.Cluster.Provider,
		clusterCfg.Spec.Cluster.LocalRegistry,
	)
	if err != nil {
		return fmt.Errorf("local registry validation: %w", err)
	}

	return nil
}

// HandleInitRunE handles the init command.
func HandleInitRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps InitDeps,
) error {
	if deps.Timer != nil {
		deps.Timer.Start()
	}

	clusterCfg, err := cfgManager.Load(
		configmanager.LoadOptions{Silent: true, IgnoreConfigFile: true},
	)
	if err != nil {
		return fmt.Errorf("failed to resolve configuration for scaffolding: %w", err)
	}

	err = validateInitConfig(clusterCfg)
	if err != nil {
		return err
	}

	scaffolderInstance, targetPath, force, err := prepareScaffolder(cmd, cfgManager, clusterCfg)
	if err != nil {
		return err
	}

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Initialize project...",
		Emoji:   "📂",
		Writer:  cmd.OutOrStdout(),
	})

	err = scaffolderInstance.Scaffold(targetPath, force)
	if err != nil {
		return fmt.Errorf("failed to scaffold project files: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "initialized project",
		Timer:   flags.MaybeTimer(cmd, deps.Timer),
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// prepareScaffolder sets up the scaffolder with configuration from flags.
func prepareScaffolder(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
) (*scaffolder.Scaffolder, string, bool, error) {
	targetPath, err := resolveInitTargetPath(cfgManager)
	if err != nil {
		return nil, "", false, err
	}

	force := cfgManager.Viper.GetBool("force")
	mirrorRegistries := mirrorregistry.GetMirrorRegistriesWithDefaults(
		cmd, cfgManager, clusterCfg.Spec.Cluster.Provider,
	)
	clusterName := cfgManager.Viper.GetString("name")

	// Validate mirror registries are compatible with the provider
	err = v1alpha1.ValidateMirrorRegistriesForProvider(
		clusterCfg.Spec.Cluster.Provider,
		mirrorRegistries,
	)
	if err != nil {
		return nil, "", false, fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate cluster name is DNS-1123 compliant
	if clusterName != "" {
		validationErr := v1alpha1.ValidateClusterName(clusterName)
		if validationErr != nil {
			return nil, "", false, fmt.Errorf("invalid --name flag: %w", validationErr)
		}
	}

	scaffolderInstance := scaffolder.NewScaffolder(
		*clusterCfg,
		cmd.OutOrStdout(),
		mirrorRegistries,
	)

	// Apply cluster name override if provided
	if clusterName != "" {
		scaffolderInstance.WithClusterName(clusterName)
	}

	return scaffolderInstance, targetPath, force, nil
}

func resolveInitTargetPath(cfgManager *ksailconfigmanager.ConfigManager) (string, error) {
	flagOutputPath := cfgManager.Viper.GetString("output")
	if flagOutputPath != "" {
		return flagOutputPath, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return wd, nil
}

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider

// allDistributions returns all supported distributions.
func allDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
	}
}

// allProviders returns all supported providers.
func allProviders() []v1alpha1.Provider {
	return []v1alpha1.Provider{
		v1alpha1.ProviderDocker,
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
	}
}

// listResult holds a cluster name with its provider and distribution for display purposes.
type listResult struct {
	Provider     v1alpha1.Provider
	Distribution v1alpha1.Distribution
	ClusterName  string
	TTL          *state.TTLInfo // nil if no TTL has been set for this cluster
}

// clusterWithDistribution pairs a cluster name with its distribution.
type clusterWithDistribution struct {
	Name         string
	Distribution v1alpha1.Distribution
}

// tableColumnGap is the minimum gap between columns in table output.
const tableColumnGap = 3

const listLongDesc = `List all Kubernetes clusters managed by KSail.

By default, lists clusters from all distributions across all providers.
Use --provider to filter results to a specific provider.

Output Format:
  PROVIDER   DISTRIBUTION   CLUSTER
  docker     Vanilla        dev-cluster
  docker     K3s            test-cluster
  hetzner    Talos          prod-cluster

When any cluster has a TTL set, a TTL column is included:
  PROVIDER   DISTRIBUTION   CLUSTER       TTL
  docker     K3s            dev-cluster   2h 30m

The PROVIDER and CLUSTER values from the output can be used directly
with other cluster commands:
  ksail cluster delete --name <cluster> --provider <provider>
  ksail cluster stop --name <cluster> --provider <provider>

Examples:
  # List all clusters
  ksail cluster list

  # List only Docker-based clusters
  ksail cluster list --provider Docker

  # List only Hetzner clusters
  ksail cluster list --provider Hetzner

  # List only Omni clusters
  ksail cluster list --provider Omni`

// NewListCmd creates the list command for clusters.
func NewListCmd(runtimeContainer *di.Runtime) *cobra.Command {
	var providerFilter v1alpha1.Provider

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         listLongDesc,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runtimeContainer.Invoke(func(_ di.Injector) error {
				deps := ListDeps{}

				return HandleListRunE(cmd, providerFilter, deps)
			})
		},
	}

	// Add --provider flag as optional filter (no default - lists all by default)
	cmd.Flags().VarP(
		&providerFilter,
		"provider", "p",
		fmt.Sprintf("Filter by provider (%s). If not specified, lists all providers.",
			strings.Join(providerFilter.ValidValues(), ", ")),
	)

	return cmd
}

// ListDeps captures dependencies needed for the list command logic.
type ListDeps struct {
	// DistributionFactoryCreator is an optional function that creates factories for distributions.
	// If nil, real factories with empty configs are used.
	// This is primarily for testing purposes.
	DistributionFactoryCreator func(v1alpha1.Distribution) clusterprovisioner.Factory

	// HetznerProvider is an optional Hetzner provider for listing Hetzner clusters.
	// If nil, a real provider will be created if HCLOUD_TOKEN is set.
	HetznerProvider *hetzner.Provider

	// OmniProvider is an optional Omni provider for listing Omni clusters.
	// If nil, a real provider will be created if OMNI_SERVICE_ACCOUNT_KEY is set.
	OmniProvider *omni.Provider
}

// HandleListRunE handles the list command.
// Exported for testing purposes.
func HandleListRunE(
	cmd *cobra.Command,
	providerFilter v1alpha1.Provider,
	deps ListDeps,
) error {
	// Determine which providers to query
	providers := resolveProviders(providerFilter)

	// Collect clusters from all providers
	var allResults []listResult

	for _, prov := range providers {
		clusters, err := getProviderClusters(cmd.Context(), deps, prov)
		if err != nil {
			// Log warning but continue with other providers
			_, _ = fmt.Fprintf(
				cmd.ErrOrStderr(),
				"Warning: failed to list %s clusters: %v\n",
				prov,
				err,
			)

			continue
		}

		for _, cluster := range clusters {
			ttlInfo, ttlErr := state.LoadClusterTTL(cluster.Name)
			if ttlErr != nil && !errors.Is(ttlErr, state.ErrTTLNotSet) {
				notify.Warningf(
					cmd.ErrOrStderr(),
					"failed to load TTL for cluster %q: %v",
					cluster.Name,
					ttlErr,
				)
			}

			var ttl *state.TTLInfo
			if ttlErr == nil {
				ttl = ttlInfo
			}

			allResults = append(allResults, listResult{
				Provider:     prov,
				Distribution: cluster.Distribution,
				ClusterName:  cluster.Name,
				TTL:          ttl,
			})
		}
	}

	// Display results
	displayListResults(cmd.OutOrStdout(), providers, allResults)

	return nil
}

// resolveProviders returns the list of providers to query based on the filter.
func resolveProviders(filter v1alpha1.Provider) []v1alpha1.Provider {
	if filter == "" {
		return allProviders()
	}

	return []v1alpha1.Provider{filter}
}

// getProviderClusters returns all clusters for a given provider.
func getProviderClusters(
	ctx context.Context,
	deps ListDeps,
	provider v1alpha1.Provider,
) ([]clusterWithDistribution, error) {
	switch provider {
	case v1alpha1.ProviderDocker:
		return getDockerClusters(ctx, deps)
	case v1alpha1.ProviderHetzner:
		return getHetznerClusters(ctx, deps)
	case v1alpha1.ProviderOmni:
		return getOmniClusters(ctx, deps)
	case v1alpha1.ProviderAWS:
		// EKS cluster listing goes through the EKS API; not yet implemented
		// in the local list path. Return an empty slice so `cluster list`
		// does not error when AWS is configured in a profile.
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

// getDockerClusters returns all Docker-based clusters across all distributions.
// Results are deduplicated by cluster name because different distributions
// (Kind, K3d, Talos, VCluster) each manage their own namespace and a cluster
// name uniquely identifies a cluster within the Docker provider.
func getDockerClusters(ctx context.Context, deps ListDeps) ([]clusterWithDistribution, error) {
	seen := make(map[string]struct{})

	var allClusters []clusterWithDistribution

	for _, dist := range allDistributions() {
		clusters, err := getDistributionClusters(ctx, deps, dist)
		if err != nil {
			// Log and continue - don't fail on one distribution
			continue
		}

		for _, c := range clusters {
			if _, ok := seen[c]; !ok {
				seen[c] = struct{}{}
				allClusters = append(allClusters, clusterWithDistribution{
					Name:         c,
					Distribution: dist,
				})
			}
		}
	}

	return allClusters, nil
}

// getHetznerClusters returns all Hetzner-based clusters.
func getHetznerClusters(ctx context.Context, deps ListDeps) ([]clusterWithDistribution, error) {
	// Use injected provider if available (for testing)
	if deps.HetznerProvider != nil {
		clusters, err := deps.HetznerProvider.ListAllClusters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Hetzner clusters: %w", err)
		}

		return toTalosClusters(clusters), nil
	}

	// Check for HCLOUD_TOKEN
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		// No token, skip Hetzner silently
		return nil, nil
	}

	client := hcloud.NewClient(hcloud.WithToken(token))
	provider := hetzner.NewProvider(client)

	clusters, err := provider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Hetzner clusters: %w", err)
	}

	return toTalosClusters(clusters), nil
}

// getOmniClusters returns all Omni-based clusters.
func getOmniClusters(ctx context.Context, deps ListDeps) ([]clusterWithDistribution, error) {
	// Use injected provider if available (for testing)
	if deps.OmniProvider != nil {
		clusters, err := deps.OmniProvider.ListAllClusters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Omni clusters: %w", err)
		}

		return toTalosClusters(clusters), nil
	}

	// Check for OMNI_SERVICE_ACCOUNT_KEY
	serviceAccountKey := os.Getenv("OMNI_SERVICE_ACCOUNT_KEY")
	if serviceAccountKey == "" {
		// No key, skip Omni silently
		return nil, nil
	}

	// Check for OMNI_ENDPOINT
	endpoint := os.Getenv("OMNI_ENDPOINT")
	if endpoint == "" {
		// No endpoint, skip Omni silently
		return nil, nil
	}

	client, err := omniclient.New(
		endpoint,
		omniclient.WithServiceAccount(serviceAccountKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Omni client: %w", err)
	}

	provider := omni.NewProvider(client)

	clusters, err := provider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Omni clusters: %w", err)
	}

	return toTalosClusters(clusters), nil
}

// toTalosClusters wraps cluster names as Talos distribution clusters.
// Hetzner and Omni providers only support Talos.
func toTalosClusters(names []string) []clusterWithDistribution {
	result := make([]clusterWithDistribution, 0, len(names))
	for _, name := range names {
		result = append(result, clusterWithDistribution{
			Name:         name,
			Distribution: v1alpha1.DistributionTalos,
		})
	}

	return result
}

func getDistributionClusters(
	ctx context.Context,
	deps ListDeps,
	distribution v1alpha1.Distribution,
) ([]string, error) {
	// Create a minimal cluster config for the factory
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: distribution,
			},
		},
	}

	// Use custom factory creator if provided (for testing), otherwise create real factory.
	var factory clusterprovisioner.Factory
	if deps.DistributionFactoryCreator != nil {
		factory = deps.DistributionFactoryCreator(distribution)
	} else {
		// Create a factory with an empty config for the distribution.
		// For list operations, we only need the provisioner type, not specific config data.
		factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: createEmptyDistributionConfig(distribution),
		}
	}

	provisioner, _, err := factory.Create(ctx, clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner for %s: %w", distribution, err)
	}

	clusters, err := provisioner.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s clusters: %w", distribution, err)
	}

	return clusters, nil
}

// createEmptyDistributionConfig creates an empty distribution config for the given distribution.
// This is used for list operations where we only need the provisioner type, not specific config data.
func createEmptyDistributionConfig(
	distribution v1alpha1.Distribution,
) *clusterprovisioner.DistributionConfig {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		}
	case v1alpha1.DistributionK3s:
		return &clusterprovisioner.DistributionConfig{
			K3d: &v1alpha5.SimpleConfig{},
		}
	case v1alpha1.DistributionTalos:
		return &clusterprovisioner.DistributionConfig{
			Talos: &talosconfigmanager.Configs{},
		}
	case v1alpha1.DistributionVCluster:
		return &clusterprovisioner.DistributionConfig{
			VCluster: &clusterprovisioner.VClusterConfig{},
		}
	case v1alpha1.DistributionKWOK:
		return &clusterprovisioner.DistributionConfig{
			KWOK: &clusterprovisioner.KWOKConfig{},
		}
	case v1alpha1.DistributionEKS:
		return &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{},
		}
	default:
		return &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		}
	}
}

// tableRow holds pre-formatted strings for a single row in the cluster list table.
type tableRow struct {
	provider     string
	distribution string
	cluster      string
	ttl          string
}

// displayListResults outputs the cluster list as an aligned table.
// Columns: PROVIDER, DISTRIBUTION, CLUSTER, and optionally TTL (when any cluster has one).
// If no clusters exist, displays "No clusters found.".
func displayListResults(
	writer io.Writer,
	providers []v1alpha1.Provider,
	results []listResult,
) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(writer, "No clusters found.")

		return
	}

	rows, hasTTL := buildTableRows(providers, results)
	printTable(writer, rows, hasTTL)
}

// buildTableRows converts listResults into ordered tableRows following provider order.
// Returns the rows and whether any row has a TTL value.
func buildTableRows(providers []v1alpha1.Provider, results []listResult) ([]tableRow, bool) {
	hasTTL := false

	var rows []tableRow

	for _, prov := range providers {
		for _, result := range results {
			if result.Provider != prov {
				continue
			}

			ttlStr := formatTTLValue(result.TTL)
			if ttlStr != "" {
				hasTTL = true
			}

			rows = append(rows, tableRow{
				provider:     strings.ToLower(string(result.Provider)),
				distribution: string(result.Distribution),
				cluster:      result.ClusterName,
				ttl:          ttlStr,
			})
		}
	}

	return rows, hasTTL
}

// formatTTLValue returns the human-readable TTL string for display, or "" if no TTL is set.
func formatTTLValue(ttl *state.TTLInfo) string {
	if ttl == nil {
		return ""
	}

	remaining := ttl.Remaining()
	if remaining <= 0 {
		return "EXPIRED"
	}

	return formatRemainingDuration(remaining)
}

// printTable writes an aligned table of cluster rows to the writer.
func printTable(writer io.Writer, rows []tableRow, hasTTL bool) {
	provW := len("PROVIDER")
	distW := len("DISTRIBUTION")
	clusterW := len("CLUSTER")

	for _, row := range rows {
		if len(row.provider) > provW {
			provW = len(row.provider)
		}

		if len(row.distribution) > distW {
			distW = len(row.distribution)
		}

		if len(row.cluster) > clusterW {
			clusterW = len(row.cluster)
		}
	}

	if hasTTL {
		_, _ = fmt.Fprintf(writer, "%-*s%-*s%-*s%s\n",
			provW+tableColumnGap, "PROVIDER",
			distW+tableColumnGap, "DISTRIBUTION",
			clusterW+tableColumnGap, "CLUSTER",
			"TTL",
		)
	} else {
		_, _ = fmt.Fprintf(writer, "%-*s%-*s%s\n",
			provW+tableColumnGap, "PROVIDER",
			distW+tableColumnGap, "DISTRIBUTION",
			"CLUSTER",
		)
	}

	for _, row := range rows {
		printTableRow(writer, row, provW, distW, clusterW, hasTTL)
	}
}

// printTableRow writes a single data row. When the table has a TTL column,
// the cluster field is padded for alignment even on rows without a TTL value.
func printTableRow(writer io.Writer, row tableRow, provW, distW, clusterW int, hasTTLColumn bool) {
	if hasTTLColumn {
		_, _ = fmt.Fprintf(writer, "%-*s%-*s%-*s%s\n",
			provW+tableColumnGap, row.provider,
			distW+tableColumnGap, row.distribution,
			clusterW+tableColumnGap, row.cluster,
			row.ttl,
		)

		return
	}

	_, _ = fmt.Fprintf(writer, "%-*s%-*s%s\n",
		provW+tableColumnGap, row.provider,
		distW+tableColumnGap, row.distribution,
		row.cluster,
	)
}

// minutesPerHour is the number of minutes in one hour.
const minutesPerHour = 60

// formatRemainingDuration formats a positive duration as a human-readable string.
// Durations are truncated (floored) to whole minutes so the display never overstates
// remaining time. Values under one minute display as "<1m".
func formatRemainingDuration(d time.Duration) string {
	d = d.Truncate(time.Minute)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % minutesPerHour

	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return "<1m"
	}
}

// defaultReconcileTimeout is the default timeout for component reconciliation operations.
const defaultReconcileTimeout = 5 * time.Minute

// errMetricsServerDisableUnsupported is returned when attempting to disable metrics-server in-place.
var errMetricsServerDisableUnsupported = errors.New(
	"disabling metrics-server in-place is not yet supported; use 'ksail cluster delete && ksail cluster create'",
)

// componentReconciler applies component-level changes detected by the DiffEngine.
// It maps field names from the diff to installer Install/Uninstall operations.
type componentReconciler struct {
	cmd         *cobra.Command
	clusterCfg  *v1alpha1.Cluster
	clusterName string
	factories   *setup.InstallerFactories
}

// newComponentReconciler creates a reconciler for applying component changes.
func newComponentReconciler(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) *componentReconciler {
	return &componentReconciler{
		cmd:         cmd,
		clusterCfg:  clusterCfg,
		clusterName: clusterName,
		factories:   getInstallerFactories(),
	}
}

// reconcileComponents applies in-place component changes from the diff.
// It processes each component change and records results in the provided UpdateResult.
// Returns the number of successfully applied changes and any error from the last failure.
func (r *componentReconciler) reconcileComponents(
	ctx context.Context,
	diff *clusterupdate.UpdateResult,
	result *clusterupdate.UpdateResult,
) error {
	var lastErr error

	for _, change := range diff.InPlaceChanges {
		handler, ok := r.handlerForField(change.Field)
		if !ok {
			continue // Not a component field — handled by provisioner
		}

		err := handler(ctx, change)
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:    change.Field,
				OldValue: change.OldValue,
				NewValue: change.NewValue,
				Category: clusterupdate.ChangeCategoryInPlace,
				Reason:   fmt.Sprintf("failed to reconcile: %v", err),
			})

			lastErr = err

			continue
		}

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    change.Field,
			OldValue: change.OldValue,
			NewValue: change.NewValue,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "component reconciled successfully",
		})
	}

	return lastErr
}

// handlerForField returns the reconciliation handler for a given diff field name.
// Returns false if the field is not a component field (e.g., node counts, registry settings).
func (r *componentReconciler) handlerForField(
	field string,
) (func(context.Context, clusterupdate.Change) error, bool) {
	handlers := map[string]func(context.Context, clusterupdate.Change) error{
		"cluster.cni":           r.reconcileCNI,
		"cluster.csi":           r.reconcileCSI,
		"cluster.metricsServer": r.reconcileMetricsServer,
		"cluster.loadBalancer":  r.reconcileLoadBalancer,
		"cluster.certManager":   r.reconcileCertManager,
		"cluster.policyEngine":  r.reconcilePolicyEngine,
		"cluster.gitOpsEngine":  r.reconcileGitOpsEngine,
		"cluster.workload.tag":  r.reconcileWorkloadTag,
	}

	handler, ok := handlers[field]

	return handler, ok
}

// reconcileCNI switches the CNI by installing the new CNI.
// The old CNI is not uninstalled — the new CNI replaces it via Helm upgrade.
func (r *componentReconciler) reconcileCNI(_ context.Context, _ clusterupdate.Change) error {
	_, err := setup.InstallCNI(r.cmd, r.clusterCfg, nil)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	return nil
}

// reconcileCSI installs or uninstalls the CSI driver.
func (r *componentReconciler) reconcileCSI(ctx context.Context, change clusterupdate.Change) error {
	if r.factories.CSI == nil {
		return setup.ErrCSIInstallerFactoryNil
	}

	newValue := v1alpha1.CSI(change.NewValue)
	oldValue := v1alpha1.CSI(change.OldValue)

	// If new value disables CSI, uninstall the old one (only if it was installed)
	if newValue == v1alpha1.CSIDisabled {
		if oldValue == v1alpha1.CSIDisabled || oldValue == "" {
			return nil
		}

		return r.uninstallWithFactory(ctx, r.factories.CSI)
	}

	// Install the new CSI
	err := setup.InstallCSISilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return fmt.Errorf("failed to install CSI: %w", err)
	}

	return nil
}

// reconcileMetricsServer installs or uninstalls the metrics server.
func (r *componentReconciler) reconcileMetricsServer(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	newValue := v1alpha1.MetricsServer(change.NewValue)

	if newValue == v1alpha1.MetricsServerDisabled {
		return errMetricsServerDisableUnsupported
	}

	if setup.NeedsMetricsServerInstall(r.clusterCfg) {
		err := setup.InstallMetricsServerSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install metrics-server: %w", err)
		}
	}

	return nil
}

// reconcileLoadBalancer installs or uninstalls the load balancer.
func (r *componentReconciler) reconcileLoadBalancer(
	ctx context.Context,
	_ clusterupdate.Change,
) error {
	if setup.NeedsLoadBalancerInstall(r.clusterCfg) {
		err := setup.InstallLoadBalancerSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install load balancer: %w", err)
		}
	}

	return nil
}

// reconcileCertManager installs or uninstalls cert-manager.
func (r *componentReconciler) reconcileCertManager(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	if r.factories.CertManager == nil {
		return setup.ErrCertManagerInstallerFactoryNil
	}

	newValue := v1alpha1.CertManager(change.NewValue)
	oldValue := v1alpha1.CertManager(change.OldValue)

	if newValue == v1alpha1.CertManagerDisabled {
		// If already disabled, nothing to uninstall
		if oldValue == v1alpha1.CertManagerDisabled || oldValue == "" {
			return nil
		}

		return r.uninstallWithFactory(ctx, r.factories.CertManager)
	}

	err := setup.InstallCertManagerSilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return fmt.Errorf("failed to install cert-manager: %w", err)
	}

	return nil
}

// reconcilePolicyEngine installs or uninstalls the policy engine.
func (r *componentReconciler) reconcilePolicyEngine(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	newValue := v1alpha1.PolicyEngine(change.NewValue)
	oldValue := v1alpha1.PolicyEngine(change.OldValue)

	if newValue == v1alpha1.PolicyEngineNone || newValue == "" {
		// If already none/disabled, nothing to uninstall
		if oldValue == v1alpha1.PolicyEngineNone || oldValue == "" {
			return nil
		}

		if r.factories.PolicyEngine == nil {
			return setup.ErrPolicyEngineInstallerFactoryNil
		}

		return r.uninstallWithFactory(ctx, r.factories.PolicyEngine)
	}

	if r.factories.PolicyEngine == nil {
		return setup.ErrPolicyEngineInstallerFactoryNil
	}

	err := setup.InstallPolicyEngineSilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return fmt.Errorf("failed to install policy engine: %w", err)
	}

	return nil
}

// reconcileGitOpsEngine installs or uninstalls the GitOps engine.
//
//nolint:exhaustive // Only Flux and ArgoCD are installable; None is handled above
func (r *componentReconciler) reconcileGitOpsEngine(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	newValue := v1alpha1.GitOpsEngine(change.NewValue)
	oldValue := v1alpha1.GitOpsEngine(change.OldValue)

	if newValue == v1alpha1.GitOpsEngineNone || newValue == "" {
		// If already none/disabled, nothing to uninstall
		if oldValue == v1alpha1.GitOpsEngineNone || oldValue == "" {
			return nil
		}

		return r.uninstallGitOpsEngine(ctx, change)
	}

	// Install the new GitOps engine
	switch newValue {
	case v1alpha1.GitOpsEngineFlux:
		err := setup.InstallFluxSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install Flux: %w", err)
		}

		return nil
	case v1alpha1.GitOpsEngineArgoCD:
		err := setup.InstallArgoCDSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install ArgoCD: %w", err)
		}

		return nil
	default:
		return nil
	}
}

// uninstallGitOpsEngine uninstalls the old GitOps engine.
//
//nolint:exhaustive // Only Flux and ArgoCD can be uninstalled; other values are no-op
func (r *componentReconciler) uninstallGitOpsEngine(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	oldValue := v1alpha1.GitOpsEngine(change.OldValue)

	switch oldValue {
	case v1alpha1.GitOpsEngineFlux:
		helmClient, _, err := r.factories.HelmClientFactory(r.clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to create helm client for Flux uninstall: %w", err)
		}

		fluxInst := r.factories.Flux(helmClient, defaultReconcileTimeout)

		err = fluxInst.Uninstall(ctx)
		if err != nil {
			return fmt.Errorf("failed to uninstall Flux: %w", err)
		}

		return nil

	case v1alpha1.GitOpsEngineArgoCD:
		if r.factories.ArgoCD == nil {
			return setup.ErrArgoCDInstallerFactoryNil
		}

		return r.uninstallWithFactory(ctx, r.factories.ArgoCD)

	default:
		return nil
	}
}

// reconcileWorkloadTag updates the GitOps sync resource (FluxInstance or ArgoCD
// Application) to match the desired workload tag from configuration.
//
//nolint:exhaustive // Only Flux and ArgoCD have sync resources to update
func (r *componentReconciler) reconcileWorkloadTag(
	ctx context.Context,
	_ clusterupdate.Change,
) error {
	gitOpsEngine := r.clusterCfg.Spec.Cluster.GitOpsEngine

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(r.clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig path: %w", err)
	}

	switch gitOpsEngine {
	case v1alpha1.GitOpsEngineFlux:
		// Resolve registry host for VCluster (others return empty string)
		registryHost, resolveErr := setup.ResolveRegistryHostForCluster(
			ctx, r.clusterCfg, r.clusterName,
		)
		if resolveErr != nil {
			return fmt.Errorf("resolve registry host for flux: %w", resolveErr)
		}

		err = fluxinstaller.SetupInstance(
			ctx, kubeconfigPath, r.clusterCfg, r.clusterName, registryHost,
		)
		if err != nil {
			return fmt.Errorf("setup flux instance: %w", err)
		}

		return nil

	case v1alpha1.GitOpsEngineArgoCD:
		err = setup.EnsureArgoCDResources(
			ctx, kubeconfigPath, r.clusterCfg, r.clusterName,
		)
		if err != nil {
			return fmt.Errorf("ensure argocd resources: %w", err)
		}

		return nil

	default:
		return nil
	}
}

// uninstallWithFactory creates an installer from the factory and calls Uninstall.
func (r *componentReconciler) uninstallWithFactory(
	ctx context.Context,
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) error {
	inst, err := factory(r.clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create installer for uninstall: %w", err)
	}

	err = inst.Uninstall(ctx)
	if err != nil {
		return fmt.Errorf("failed to uninstall component: %w", err)
	}

	return nil
}

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
			annotations.AnnotationPermission: "write",
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

	_, _ = fmt.Fprintf(writer, "   Restored %s\n", resourceType)

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

	return append(selectors,
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

	// Apply cluster name override from --name flag if provided
	nameOverride := cfgManager.Viper.GetString("name")
	if nameOverride != "" {
		validationErr := v1alpha1.ValidateClusterName(nameOverride)
		if validationErr != nil {
			return nil, "", fmt.Errorf("invalid --name flag: %w", validationErr)
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

	configureProvisionerFactory(&deps, ctx)

	err = executeClusterLifecycle(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return err
	}

	// Post-creation Docker steps are only needed for local Docker clusters.
	// Cloud providers (Omni, Hetzner) run nodes remotely and cannot access
	// local Docker infrastructure.
	if !ctx.ClusterCfg.Spec.Cluster.Provider.IsCloud() {
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
		ctx.ClusterCfg.Spec.Cluster.Connection.Context = ctx.ClusterCfg.Spec.Cluster.Distribution.ContextName(
			clusterName,
		)
	}

	maybeImportCachedImages(cmd, ctx, deps.Timer)

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer)
}

const startLongDesc = `Start a previously stopped Kubernetes cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

Supported distributions are automatically detected from existing clusters.`

// NewStartCmd creates and returns the start command.
func NewStartCmd(_ any) *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:          "start",
		Short:        "Start a stopped cluster",
		Long:         startLongDesc,
		TitleEmoji:   "▶️",
		TitleContent: "Start cluster...",
		Activity:     "starting",
		Success:      "cluster started",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			return provisioner.Start(ctx, clusterName)
		},
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

const stopLongDesc = `Stop a running Kubernetes cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

Supported distributions are automatically detected from existing clusters.`

// NewStopCmd creates and returns the stop command.
func NewStopCmd(_ any) *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:          "stop",
		Short:        "Stop a running cluster",
		Long:         stopLongDesc,
		TitleEmoji:   "🛑",
		TitleContent: "Stop cluster...",
		Activity:     "stopping",
		Success:      "cluster stopped",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			return provisioner.Stop(ctx, clusterName)
		},
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// ErrContextNotFound is returned when the specified cluster does not have a matching context in the kubeconfig.
var ErrContextNotFound = errors.New("no matching context found for cluster")

// ErrAmbiguousCluster is returned when multiple distribution contexts match the cluster name.
var ErrAmbiguousCluster = errors.New("ambiguous cluster name")

// ErrNoClusters is returned when no KSail-managed clusters are found in the kubeconfig.
var ErrNoClusters = errors.New("no KSail-managed clusters found in kubeconfig")

// switchKubeconfigFileMode is the file mode for kubeconfig files.
const switchKubeconfigFileMode = 0o600

const switchLongDesc = `Switch the active kubeconfig context to the named cluster.

This command accepts a cluster name and automatically resolves it to the
correct kubeconfig context by checking all supported distribution prefixes
(kind-, k3d-, admin@, vcluster-docker_).

If multiple distributions have contexts for the same cluster name, the
command returns an error listing the matching contexts.

The kubeconfig is resolved in the following priority order:
  1. From KUBECONFIG environment variable
  2. From ksail.yaml config file (if present)
  3. Defaults to ~/.kube/config

When called without arguments, an interactive picker is shown
to select from available clusters.

Examples:
  # Switch to a Vanilla (Kind) cluster named "dev"
  ksail cluster switch dev

  # Switch to a cluster named "staging"
  ksail cluster switch staging

  # Select a cluster interactively
  ksail cluster switch`

// NewSwitchCmd creates the switch command for clusters.
func NewSwitchCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch [cluster-name]",
		Short: "Switch active cluster context",
		Long:  switchLongDesc,
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(
			cmd *cobra.Command,
			_ []string,
			_ string,
		) ([]string, cobra.ShellCompDirective) {
			return listClusterNames(cmd), cobra.ShellCompDirectiveNoFileComp
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps := SwitchDeps{}

			if len(args) > 0 {
				return HandleSwitchRunE(cmd, args[0], deps)
			}

			clusterName, err := pickCluster(cmd, deps)
			if err != nil {
				return err
			}

			return HandleSwitchRunE(cmd, clusterName, deps)
		},
	}

	return cmd
}

// SwitchDeps captures injectable dependencies for the switch command.
type SwitchDeps struct {
	// KubeconfigPath overrides the kubeconfig path resolution.
	// If empty, the path is resolved from KUBECONFIG env, ksail.yaml, or the default.
	KubeconfigPath string

	// PickCluster overrides the interactive picker for testing.
	// If nil, the default bubbletea picker is used.
	PickCluster func(title string, items []string) (string, error)
}

// resolveSwitchKubeconfig returns the kubeconfig path for switch operations.
// It uses the injected path from deps when provided, otherwise delegates to
// resolveKubeconfigForSwitch (which checks KUBECONFIG env, ksail.yaml, and the default).
func resolveSwitchKubeconfig(cmd *cobra.Command, deps SwitchDeps) (string, error) {
	if deps.KubeconfigPath != "" {
		return deps.KubeconfigPath, nil
	}

	path, err := resolveKubeconfigForSwitch(cmd)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return path, nil
}

// HandleSwitchRunE handles the switch command.
// Exported for testing purposes.
func HandleSwitchRunE(
	cmd *cobra.Command,
	clusterName string,
	deps SwitchDeps,
) error {
	kubeconfigPath, err := resolveSwitchKubeconfig(cmd, deps)
	if err != nil {
		return err
	}

	contextName, err := switchContext(kubeconfigPath, clusterName)
	if err != nil {
		return err
	}

	notify.Successf(
		cmd.OutOrStdout(),
		"Switched to cluster '%s' (context: %s)",
		clusterName,
		contextName,
	)

	return nil
}

// pickCluster resolves the kubeconfig, lists available cluster names, and
// presents an interactive picker for the user to select one.
func pickCluster(cmd *cobra.Command, deps SwitchDeps) (string, error) {
	kubeconfigPath, err := resolveSwitchKubeconfig(cmd, deps)
	if err != nil {
		return "", err
	}

	names := clusterNamesFromPath(kubeconfigPath)
	if len(names) == 0 {
		return "", fmt.Errorf("%w", ErrNoClusters)
	}

	pick := deps.PickCluster
	if pick == nil {
		pick = picker.Run
	}

	selected, err := pick("Select a cluster:", names)
	if err != nil {
		return "", fmt.Errorf("cluster selection: %w", err)
	}

	return selected, nil
}

// resolveContextName finds the matching kubeconfig context for a cluster name
// by checking all known distribution context-name prefixes.
// Parenthetical suffixes (e.g., " (Vanilla)") are stripped defensively so that
// cluster names containing distribution hints still resolve correctly.
func resolveContextName(
	config *clientcmdapi.Config,
	clusterName string,
) (string, error) {
	// Strip trailing parenthetical suffix (e.g., " (Vanilla)") that may be
	// present if the name was copied from enriched list output.
	cleanName := stripParenthetical(clusterName)

	var matches []string

	for _, dist := range v1alpha1.ValidDistributions() {
		candidate := dist.ContextName(cleanName)

		if _, exists := config.Contexts[candidate]; exists {
			matches = append(matches, candidate)
		}
	}

	// Fallback: if no distribution-prefix match was found, look for contexts
	// that contain the cluster name as a substring. This handles providers like
	// Omni whose kubeconfig context format (<org>-<cluster>-<sa>) doesn't
	// follow the standard distribution prefix conventions.
	if len(matches) == 0 {
		for ctxName := range config.Contexts {
			if strings.Contains(ctxName, cleanName) {
				matches = append(matches, ctxName)
			}
		}
	}

	switch len(matches) {
	case 0:
		available := make([]string, 0, len(config.Contexts))
		for name := range config.Contexts {
			available = append(available, name)
		}

		sort.Strings(available)

		return "", fmt.Errorf(
			"%w: %s (available contexts: %s)",
			ErrContextNotFound,
			clusterName,
			strings.Join(available, ", "),
		)
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)

		return "", fmt.Errorf(
			"%w: '%s' matches multiple contexts: %s",
			ErrAmbiguousCluster,
			clusterName,
			strings.Join(matches, ", "),
		)
	}
}

// stripParenthetical removes a trailing " (<text>)" suffix from input.
// Returns input unchanged if no such suffix is present.
func stripParenthetical(input string) string {
	idx := strings.LastIndex(input, " (")
	if idx < 0 {
		return input
	}

	if strings.HasSuffix(input, ")") {
		return input[:idx]
	}

	return input
}

// switchContext loads the kubeconfig, resolves the cluster name to a context, and sets current-context.
//
//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
func switchContext(kubeconfigPath, clusterName string) (string, error) {
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	contextName, err := resolveContextName(config, clusterName)
	if err != nil {
		return "", err
	}

	config.CurrentContext = contextName

	result, err := clientcmd.Write(*config)
	if err != nil {
		return "", fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	err = os.WriteFile(kubeconfigPath, result, switchKubeconfigFileMode)
	if err != nil {
		return "", fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return contextName, nil
}

// listClusterNames returns deduplicated cluster names from the kubeconfig for shell completion.
// It strips known distribution prefixes from context names to produce cluster names.
// When cmd is non-nil, the --config persistent flag is honored for config loading.
func listClusterNames(cmd *cobra.Command) []string {
	kubeconfigPath, err := resolveKubeconfigForSwitch(cmd)
	if err != nil {
		return nil
	}

	return clusterNamesFromPath(kubeconfigPath)
}

// clusterNamesFromPath reads the given kubeconfig and returns sorted, deduplicated
// cluster names by stripping distribution prefixes from context names.
//
//nolint:gosec // G304: kubeconfigPath is resolved from trusted config or default
func clusterNamesFromPath(kubeconfigPath string) []string {
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})

	for contextName := range config.Contexts {
		if name := stripDistributionPrefix(contextName); name != "" {
			seen[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// stripDistributionPrefix removes the distribution-specific prefix from a context name,
// returning the underlying cluster name. Returns empty string if the context name
// does not match any known distribution prefix.
func stripDistributionPrefix(contextName string) string {
	const sentinel = "\x00"

	for _, dist := range v1alpha1.ValidDistributions() {
		prefix := strings.TrimSuffix(dist.ContextName(sentinel), sentinel)

		if after, found := strings.CutPrefix(contextName, prefix); found {
			return after
		}
	}

	return ""
}

// resolveKubeconfigForSwitch resolves the kubeconfig path using the same priority
// order as other cluster commands: KUBECONFIG env > ksail.yaml > default (~/.kube/config).
// When KUBECONFIG contains multiple paths separated by the OS path list separator,
// only the first path is used.
// When cmd is non-nil, the --config persistent flag is honored for config loading.
func resolveKubeconfigForSwitch(cmd *cobra.Command) (string, error) {
	// 1. Check KUBECONFIG environment variable
	if os.Getenv("KUBECONFIG") != "" {
		// ResolveKubeconfigPath("") checks KUBECONFIG env, splits on path separator,
		// expands ~, and returns the first path.
		resolved, err := clusterdetector.ResolveKubeconfigPath("")
		if err != nil {
			return "", fmt.Errorf("resolve kubeconfig from KUBECONFIG env: %w", err)
		}

		return resolved, nil
	}

	// 2. Try ksail.yaml config file, falls back to default (~/.kube/config)
	path := kubeconfig.GetKubeconfigPathSilently(cmd)

	resolved, err := clusterdetector.ResolveKubeconfigPath(path)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return resolved, nil
}

// overrideInstallerFactory is a helper that applies a factory override and returns a restore function.
func overrideInstallerFactory(apply func(*setup.InstallerFactories)) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := setup.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	apply(override)
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()

		installerFactoriesOverride = previous

		installerFactoriesOverrideMu.Unlock()
	}
}

// SetCertManagerInstallerFactoryForTests overrides the cert-manager installer factory.
func SetCertManagerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.CertManager = factory
	})
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.CSI = factory
	})
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.ArgoCD = factory
	})
}

// SetPolicyEngineInstallerFactoryForTests overrides the policy engine installer factory.
func SetPolicyEngineInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.PolicyEngine = factory
	})
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster, string) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureArgoCDResources = fn
	})
}

// SetFluxInstallerFactoryForTests overrides the Flux installer factory.
func SetFluxInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		// Wrap the simplified test factory to match the Flux factory signature
		f.Flux = func(_ helm.Interface, _ time.Duration) installer.Installer {
			inst, _ := factory(nil) // clusterCfg not used in test factory

			return inst
		}
	})
}

// SetDockerClientInvokerForTests overrides the Docker client invoker for testing.
func SetDockerClientInvokerForTests(
	invoker func(*cobra.Command, func(client.APIClient) error) error,
) func() {
	dockerClientInvokerMu.Lock()

	previous := dockerClientInvoker
	dockerClientInvoker = invoker

	dockerClientInvokerMu.Unlock()

	return func() {
		dockerClientInvokerMu.Lock()

		dockerClientInvoker = previous

		dockerClientInvokerMu.Unlock()
	}
}

// SetProvisionerFactoryForTests overrides the cluster provisioner factory for testing.
func SetProvisionerFactoryForTests(factory clusterprovisioner.Factory) func() {
	clusterProvisionerFactoryMu.Lock()

	previous := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryOverride = factory

	clusterProvisionerFactoryMu.Unlock()

	return func() {
		clusterProvisionerFactoryMu.Lock()

		clusterProvisionerFactoryOverride = previous

		clusterProvisionerFactoryMu.Unlock()
	}
}

// SetLocalRegistryServiceFactoryForTests overrides the local registry service factory for testing.
func SetLocalRegistryServiceFactoryForTests(factory localregistry.ServiceFactoryFunc) func() {
	localRegistryServiceFactoryMu.Lock()

	previous := localRegistryServiceFactory
	localRegistryServiceFactory = factory

	localRegistryServiceFactoryMu.Unlock()

	return func() {
		localRegistryServiceFactoryMu.Lock()

		localRegistryServiceFactory = previous

		localRegistryServiceFactoryMu.Unlock()
	}
}

// SetSetupFluxInstanceForTests overrides the FluxInstance setup function.
func SetSetupFluxInstanceForTests(
	fn func(context.Context, string, *v1alpha1.Cluster, string, string) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.SetupFluxInstance = fn
	})
}

// SetWaitForFluxReadyForTests overrides the Flux readiness wait function.
func SetWaitForFluxReadyForTests(fn func(context.Context, string) error) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.WaitForFluxReady = fn
	})
}

// SetEnsureOCIArtifactForTests overrides the OCI artifact ensure function.
func SetEnsureOCIArtifactForTests(
	fn func(context.Context, *cobra.Command, *v1alpha1.Cluster, string, io.Writer) (bool, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureOCIArtifact = fn
	})
}

// deleteTimeout is the maximum duration for the auto-delete operation.
const deleteTimeout = 10 * time.Minute

// waitForTTLAndDelete blocks until the TTL duration elapses and then auto-deletes the cluster.
// The wait can be cancelled with SIGINT/SIGTERM, in which case the cluster is left running.
// This implements the ephemeral cluster pattern: after creation, the process stays alive
// and automatically tears down the cluster when the TTL expires.
func waitForTTLAndDelete(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	ttl time.Duration,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"cluster will auto-destroy in %s (press Ctrl+C to cancel)", ttl)

	// Create a context that is cancelled on SIGINT/SIGTERM and also respects cmd.Context().
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	timer := time.NewTimer(ttl)
	defer timer.Stop()

	select {
	case <-timer.C:
		return autoDeleteCluster(cmd, clusterName, clusterCfg)
	case <-ctx.Done():
		notify.Infof(cmd.OutOrStdout(),
			"TTL wait cancelled; cluster %q will remain running", clusterName)

		return nil
	}
}

// autoDeleteCluster performs an automatic cluster deletion after TTL expiry.
// It creates a minimal provisioner based on distribution and provider info
// from the original cluster config and deletes the cluster.
func autoDeleteCluster(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
) error {
	notify.Infof(cmd.OutOrStdout(),
		"TTL expired; auto-destroying cluster %q...", clusterName)

	info := &clusterdetector.Info{
		ClusterName:  clusterName,
		Distribution: clusterCfg.Spec.Cluster.Distribution,
		Provider:     clusterCfg.Spec.Cluster.Provider,
	}

	provisioner, err := createDeleteProvisioner(info, clusterCfg.Spec.Provider.Omni)
	if err != nil {
		return fmt.Errorf("TTL auto-delete: failed to create provisioner: %w", err)
	}

	deleteCtx, cancel := context.WithTimeout(cmd.Context(), deleteTimeout)
	defer cancel()

	err = provisioner.Delete(deleteCtx, clusterName)
	if err != nil {
		return fmt.Errorf("TTL auto-delete failed: %w", err)
	}

	// Clean up persisted state (spec + TTL).
	// Best-effort: warn on failure rather than blocking success.
	stateErr := state.DeleteClusterState(clusterName)
	if stateErr != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to clean up cluster state: %v", stateErr)
	}

	notify.Successf(cmd.OutOrStdout(),
		"cluster %q auto-destroyed after TTL expiry", clusterName)

	return nil
}

// ErrUnsupportedOutputFormat is returned when the --output flag is set to an unsupported value.
var ErrUnsupportedOutputFormat = errors.New("unsupported --output format")

// outputFormatText is the default human-readable output format.
const outputFormatText = "text"

// outputFormatJSON is the machine-readable JSON output format.
const outputFormatJSON = "json"

// ChangeJSON is the JSON representation of a single configuration change.
// It is used by DiffJSONOutput for --output json mode.
type ChangeJSON struct {
	Field    string `json:"field"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// DiffJSONOutput is the JSON representation of the diff result, emitted when
// --output json is set. It is suitable for CI/MCP consumption.
type DiffJSONOutput struct {
	TotalChanges         int          `json:"totalChanges"`
	InPlaceChanges       []ChangeJSON `json:"inPlaceChanges"`
	RebootRequired       []ChangeJSON `json:"rebootRequired"`
	RecreateRequired     []ChangeJSON `json:"recreateRequired"`
	RequiresConfirmation bool         `json:"requiresConfirmation"`
}

// getOutputFormat returns the --output flag value from the command, defaulting to "text".
// The value is normalised to lower-case so that "--output JSON" is accepted.
// Safe to call even when the flag is not registered on cmd.
func getOutputFormat(cmd *cobra.Command) string {
	if cmd == nil {
		return outputFormatText
	}

	flag := cmd.Flags().Lookup("output")
	if flag == nil {
		return outputFormatText
	}

	return strings.ToLower(flag.Value.String())
}

// validateOutputFormat returns an error when the --output flag value is
// neither "text" nor "json".
func validateOutputFormat(cmd *cobra.Command) error {
	format := getOutputFormat(cmd)
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf(
			"%w: %q (expected %q or %q)",
			ErrUnsupportedOutputFormat,
			format,
			outputFormatText,
			outputFormatJSON,
		)
	}

	return nil
}

// diffToJSON converts an UpdateResult to a DiffJSONOutput struct.
func diffToJSON(diff *clusterupdate.UpdateResult) DiffJSONOutput {
	convertChanges := func(changes []clusterupdate.Change) []ChangeJSON {
		result := make([]ChangeJSON, len(changes))

		for i, change := range changes {
			result[i] = ChangeJSON{
				Field:    change.Field,
				OldValue: change.OldValue,
				NewValue: change.NewValue,
				Category: change.Category.String(),
				Reason:   change.Reason,
			}
		}

		return result
	}

	return DiffJSONOutput{
		TotalChanges:         diff.TotalChanges(),
		InPlaceChanges:       convertChanges(diff.InPlaceChanges),
		RebootRequired:       convertChanges(diff.RebootRequired),
		RecreateRequired:     convertChanges(diff.RecreateRequired),
		RequiresConfirmation: diff.NeedsUserConfirmation(),
	}
}

// emitDiffJSON serialises diff as indented JSON and writes it to cmd's stdout.
func emitDiffJSON(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	out := diffToJSON(diff)

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		// json.MarshalIndent on a plain struct with only basic types never fails.
		notify.Errorf(cmd.OutOrStderr(), "failed to marshal diff to JSON: %v", err)

		return
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
}

// NewUpdateCmd creates the cluster update command.
// The update command applies configuration changes to a running cluster.
// It supports in-place updates where possible and falls back to recreation when necessary.
func NewUpdateCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a cluster configuration",
		Long: `Update a Kubernetes cluster to match the current configuration.

This command applies changes from your ksail.yaml configuration to a running cluster.

For Talos clusters, many configuration changes can be applied in-place without
cluster recreation (e.g., network settings, kubelet config, registry mirrors).

For Kind/K3d clusters, in-place updates are more limited. Worker node scaling
is supported for K3d, but most other changes require cluster recreation.

Changes are classified into three categories:
  - In-Place: Applied without disruption
  - Reboot-Required: Applied but may require node reboots
  - Recreate-Required: Require full cluster recreation

Use --dry-run to preview changes without applying them.
Use --output json to emit a machine-readable diff for CI/MCP consumption.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cfgManager := setupMutationCmdFlags(cmd)

	cmd.Flags().Bool("force", false,
		"Skip confirmation prompt and proceed with cluster recreation")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))

	cmd.Flags().BoolP("yes", "y", false,
		"Skip confirmation prompt (alias for --force)")

	cmd.Flags().Bool("dry-run", false,
		"Preview changes without applying them")
	_ = cfgManager.Viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	cmd.Flags().String("output", outputFormatText,
		"Output format: text (default) or json (machine-readable, for CI/MCP)")

	cmd.Flags().Bool("update-kubernetes", false,
		"Upgrade Kubernetes to the latest stable version available in the OCI registry")
	_ = cfgManager.Viper.BindPFlag("update-kubernetes", cmd.Flags().Lookup("update-kubernetes"))

	cmd.Flags().Bool("update-distribution", false,
		"Upgrade the distribution to the latest stable version available in the OCI registry")
	_ = cfgManager.Viper.BindPFlag("update-distribution", cmd.Flags().Lookup("update-distribution"))

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleUpdateRunE)

	return cmd
}

// handleUpdateRunE executes the cluster update logic.
// It computes a diff between current and desired configuration, then applies
// changes in-place where possible, falling back to cluster recreation when necessary.
//
//nolint:cyclop,funlen // orchestration function with sequential lifecycle phases
func handleUpdateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	err := validateOutputFormat(cmd)
	if err != nil {
		return err
	}

	deps.Timer.Start()

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	// Load and validate configuration using shared helper
	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	force := resolveForce(cfgManager.Viper.GetBool("force"), cmd.Flags().Lookup("yes"))

	// Create provisioner and verify cluster exists
	provisioner, err := createAndVerifyProvisioner(cmd, ctx, clusterName)
	if err != nil {
		return err
	}

	// Handle version upgrades when requested
	updateK8s := cfgManager.Viper.GetBool("update-kubernetes")
	updateDist := cfgManager.Viper.GetBool("update-distribution")

	if updateK8s || updateDist {
		recreated, err := handleVersionUpgrades(
			cmd, cfgManager, ctx, deps, provisioner,
			clusterName, updateK8s, updateDist, force,
		)
		if err != nil {
			return err
		}
		// If the cluster was recreated, skip the regular update flow —
		// recreation already started a fresh cluster at the target version.
		if recreated {
			return nil
		}
	}

	// Check if provisioner supports updates
	updater, supportsUpdate := provisioner.(clusterprovisioner.Updater)
	if !supportsUpdate {
		// Compute a spec-level diff to determine if there are actual changes
		// before falling back to recreation. No-op when nothing changed.
		specDiff := computeSpecOnlyDiff(cmd, ctx)
		if specDiff.TotalChanges() == 0 {
			notify.Infof(cmd.OutOrStdout(), "No changes detected")

			return nil
		}

		if cfgManager.Viper.GetBool("dry-run") {
			displayChangesSummary(cmd, specDiff)
			notify.Infof(
				cmd.OutOrStdout(),
				"Provisioner does not support in-place updates; "+
					"recreation would be required.\nDry run complete. No changes applied.",
			)

			return nil
		}

		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

	// Compute full diff; return error if current config cannot be retrieved
	// instead of falling back to recreation, which would be destructive.
	currentSpec, diff, diffErr := computeUpdateDiff(cmd, ctx, updater, clusterName)
	if diffErr != nil {
		return diffErr
	}

	// Display changes summary
	displayChangesSummary(cmd, diff)

	return applyOrReportChanges(cmd, cfgManager, ctx, deps, updater,
		clusterName, currentSpec, diff, outputTimer)
}

// handleVersionUpgrades orchestrates Kubernetes and/or distribution version upgrades.
// It discovers available versions from OCI registries, computes an ordered upgrade
// path (oldest→latest), and applies each step sequentially. If any step fails, the
// cluster remains at the last successful version with actionable feedback.
//
// For distributions that require recreation (Kind, K3d, VCluster), the upgrade
// skips directly to the latest available version and recreates the cluster once,
// since there is no running state to preserve between intermediate versions.
//
// When both flags are set, distribution upgrades run first (the distribution
// runtime must support the target Kubernetes version).
//
//nolint:cyclop,funlen // orchestration function with distinct sequential phases
func handleVersionUpgrades(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	provisioner clusterprovisioner.Provisioner,
	clusterName string,
	updateK8s, updateDist, force bool,
) (bool, error) {
	upgrader, ok := provisioner.(clusterupdate.Upgrader)
	if !ok {
		return false, fmt.Errorf("%w: %s",
			clustererr.ErrUpgraderNotSupported, ctx.ClusterCfg.Spec.Cluster.Distribution)
	}

	currentVersions, err := upgrader.GetCurrentVersions(cmd.Context(), clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to get current versions: %w", err)
	}

	resolver := versionresolver.NewOCIResolver()
	dryRun := cfgManager.Viper.GetBool("dry-run")
	recreated := false

	// Distribution upgrades first (runtime must support K8s version).
	if updateDist { //nolint:nestif // sequential phase with version refresh guard
		stepRecreated, err := executeVersionUpgrade(
			cmd, cfgManager, ctx, deps, upgrader, resolver, clusterName,
			"distribution", upgrader.DistributionImageRef(),
			currentVersions.DistributionVersion, upgrader.VersionSuffix(),
			upgrader.UpgradeDistribution, force, dryRun,
		)
		if err != nil {
			return false, err
		}

		if stepRecreated {
			recreated = true
		}

		// Re-fetch versions after distribution upgrade since recreation may
		// have changed the Kubernetes version too (Kind/K3d bundle both).
		if updateK8s && !dryRun && !recreated {
			currentVersions, err = upgrader.GetCurrentVersions(cmd.Context(), clusterName)
			if err != nil {
				return false, fmt.Errorf(
					"failed to refresh versions after distribution upgrade: %w", err,
				)
			}
		}
	}

	// Then Kubernetes upgrades. Skip if we already recreated the cluster
	// (recreation picks up the latest configured version for both).
	if updateK8s && !recreated {
		stepRecreated, err := executeVersionUpgrade(
			cmd, cfgManager, ctx, deps, upgrader, resolver, clusterName,
			"Kubernetes", upgrader.KubernetesImageRef(),
			currentVersions.KubernetesVersion, upgrader.VersionSuffix(),
			upgrader.UpgradeKubernetes, force, dryRun,
		)
		if err != nil {
			return false, err
		}

		if stepRecreated {
			recreated = true
		}
	}

	return recreated, nil
}

// upgradeFunc is the signature for UpgradeKubernetes / UpgradeDistribution.
type upgradeFunc func(ctx context.Context, clusterName, fromVersion, toVersion string) error

// executeVersionUpgrade discovers available versions, computes an upgrade path,
// and applies each step. For distributions requiring recreation, it jumps to the
// latest version and triggers a single recreate.
//
//nolint:cyclop,funlen // sequential upgrade logic with distinct phases
func executeVersionUpgrade(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	upgrader clusterupdate.Upgrader,
	resolver versionresolver.Resolver,
	clusterName string,
	upgradeType string,
	imageRef string,
	currentVersion string,
	suffix string,
	applyFn upgradeFunc,
	force, dryRun bool,
) (bool, error) {
	if imageRef == "" {
		notify.Infof(cmd.OutOrStdout(),
			"No separate %s image for this distribution; "+
				"use --update-kubernetes to upgrade",
			upgradeType)

		return false, nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "🔍",
		Content: fmt.Sprintf("discovering available %s versions from %s", upgradeType, imageRef),
		Writer:  cmd.OutOrStdout(),
	})

	path, err := versionresolver.ComputeUpgradePath(
		cmd.Context(), resolver, imageRef, currentVersion, suffix)
	if err != nil {
		if errors.Is(err, versionresolver.ErrNoUpgradesAvailable) {
			notify.Infof(cmd.OutOrStdout(),
				"%s is already at the latest stable version (%s)", upgradeType, currentVersion)

			return false, nil
		}

		return false, fmt.Errorf("failed to compute %s upgrade path: %w", upgradeType, err)
	}

	// Display upgrade path
	notify.WriteMessage(notify.Message{
		Type:  notify.InfoType,
		Emoji: "📋",
		Content: fmt.Sprintf("%s upgrade path: %s → %s (%d step(s))",
			upgradeType, currentVersion, path[len(path)-1].Version.Original, len(path)),
		Writer: cmd.OutOrStdout(),
	})

	if dryRun {
		for i, step := range path {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, step.Version.Original)
		}

		notify.Infof(cmd.OutOrStdout(), "Dry run complete. No %s upgrades applied.", upgradeType)

		return false, nil
	}

	// Determine upgrade mechanism by attempting the first upgrade step.
	// Recreation-based distributions (Kind/K3d/VCluster) return ErrRecreationRequired
	// immediately without modifying the cluster, so we jump to the latest version
	// and recreate once. For rolling-upgrade distributions (Talos), the first step
	// is actually applied.
	targetVersion := path[len(path)-1].Version.Original

	probeErr := applyFn(cmd.Context(), clusterName, currentVersion, path[0].Version.Original)
	if probeErr != nil && errors.Is(probeErr, clustererr.ErrUpgradeSkipped) {
		notify.Infof(cmd.OutOrStdout(), "%s upgrade skipped: %v", upgradeType, probeErr)

		return false, nil
	}

	if probeErr != nil && errors.Is(probeErr, clustererr.ErrRecreationRequired) {
		prepErr := upgrader.PrepareConfigForVersion(upgradeType, targetVersion)
		if prepErr != nil {
			return false, fmt.Errorf(
				"failed to prepare config for %s %s: %w",
				upgradeType, targetVersion, prepErr,
			)
		}

		return true, handleRecreationUpgrade(cmd, cfgManager, ctx, deps, clusterName,
			upgradeType, currentVersion, targetVersion, force)
	}

	// Rolling upgrade (Talos): first step already applied by the probe, continue with the rest.
	if probeErr != nil {
		return false, fmt.Errorf(
			"%s upgrade failed at step 1/%d (%s → %s), cluster is still running %s: %w",
			upgradeType, len(path), currentVersion, path[0].Version.Original,
			currentVersion, probeErr,
		)
	}

	notify.WriteMessage(notify.Message{
		Type:  notify.SuccessType,
		Emoji: "⬆️",
		Content: fmt.Sprintf("%s upgraded: step 1/%d → %s",
			upgradeType, len(path), path[0].Version.Original),
		Writer: cmd.OutOrStdout(),
	})

	for stepIdx := 1; stepIdx < len(path); stepIdx++ {
		step := path[stepIdx]
		prevVersion := path[stepIdx-1].Version.Original

		notify.WriteMessage(notify.Message{
			Type:  notify.ActivityType,
			Emoji: "⬆️",
			Content: fmt.Sprintf("upgrading %s: step %d/%d (%s → %s)",
				upgradeType, stepIdx+1, len(path), prevVersion, step.Version.Original),
			Writer: cmd.OutOrStdout(),
		})

		applyErr := applyFn(
			cmd.Context(), clusterName, prevVersion, step.Version.Original,
		)
		if applyErr != nil {
			notify.Warningf(cmd.OutOrStderr(),
				"%s upgrade to %s failed (cluster is at %s): %v",
				upgradeType, step.Version.Original, prevVersion, applyErr)

			return false, fmt.Errorf(
				"%s upgrade failed at step %d/%d (%s → %s), cluster is running %s: %w",
				upgradeType, stepIdx+1, len(path), prevVersion, step.Version.Original,
				prevVersion, applyErr,
			)
		}

		notify.WriteMessage(notify.Message{
			Type:  notify.SuccessType,
			Emoji: "⬆️",
			Content: fmt.Sprintf("%s upgraded: step %d/%d → %s",
				upgradeType, stepIdx+1, len(path), step.Version.Original),
			Writer: cmd.OutOrStdout(),
		})
	}

	notify.WriteMessage(notify.Message{
		Type: notify.SuccessType,
		Content: fmt.Sprintf(
			"%s upgrade complete: %s → %s",
			upgradeType, currentVersion, targetVersion,
		),
		Writer: cmd.OutOrStdout(),
	})

	return false, nil
}

// handleRecreationUpgrade handles version upgrades for distributions that require
// cluster recreation (Kind, K3d, VCluster). It confirms with the user, then
// recreates the cluster which will pick up the latest configured version.
func handleRecreationUpgrade(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	upgradeType string,
	currentVersion, targetVersion string,
	force bool,
) error {
	notify.WriteMessage(notify.Message{
		Type:  notify.InfoType,
		Emoji: "🔄",
		Content: fmt.Sprintf(
			"%s upgrade from %s to %s requires cluster recreation",
			upgradeType, currentVersion, targetVersion),
		Writer: cmd.OutOrStdout(),
	})

	return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
}

// createAndVerifyProvisioner creates a provisioner and verifies the cluster exists.
// It constructs a ComponentDetector from the cluster's kubeconfig and injects it
// into the provisioner so that GetCurrentConfig probes the live cluster.
//
// NOTE(limitation): If the user changes distribution in ksail.yaml (e.g., Kind → Talos), this
// creates a provisioner for the NEW distribution whose Exists() check won't find
// the old cluster, reporting "cluster does not exist" rather than detecting a
// distribution change. A proper fix would probe all provisioners for an existing
// cluster of any distribution. For now, users must run 'ksail cluster delete'
// before switching distributions.
func createAndVerifyProvisioner(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	clusterName string,
) (clusterprovisioner.Provisioner, error) {
	// Create provisioner without component detector first.
	// The detector requires a kubeconfig, which may not exist yet for
	// remote providers (Omni). We build the detector after refreshing
	// the kubeconfig below.
	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:     ctx.KindConfig,
			K3d:      ctx.K3dConfig,
			Talos:    ctx.TalosConfig,
			VCluster: ctx.VClusterConfig,
			KWOK:     ctx.KWOKConfig,
		},
	}

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner: %w", err)
	}

	exists, err := provisioner.Exists(cmd.Context(), clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return nil, fmt.Errorf("%w: %q", clustererr.ErrClusterDoesNotExist, clusterName)
	}

	// Refresh kubeconfig from the remote provider if supported.
	// This ensures the kubeconfig is available for component detection
	// and subsequent Helm operations (CNI, GitOps installation).
	refresher, ok := provisioner.(clusterprovisioner.KubeconfigRefresher)
	if ok {
		err = refreshAndVerifyKubeconfig(cmd.Context(), refresher, ctx.ClusterCfg, clusterName)
		if err != nil {
			return nil, err
		}
	}

	// Build a ComponentDetector scoped to the running cluster.
	// Now that kubeconfig is ensured, the detector can connect.
	componentDetector := buildComponentDetector(cmd, ctx)

	if aware, ok := provisioner.(clusterprovisioner.ComponentDetectorAware); ok {
		aware.SetComponentDetector(componentDetector)
	}

	return provisioner, nil
}

// refreshAndVerifyKubeconfig invokes the provisioner's KubeconfigRefresher and,
// for Omni providers, ensures the kubeconfig file actually exists on disk after
// the refresh. This is a defense-in-depth guard (regression #4112): if any
// upstream step silently no-ops the fetch, downstream Helm/GitOps calls would
// otherwise only surface a cryptic "stat <path>: no such file or directory"
// warning. Failing here produces a clear, actionable error.
func refreshAndVerifyKubeconfig(
	ctx context.Context,
	refresher clusterprovisioner.KubeconfigRefresher,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	err := refresher.RefreshKubeconfig(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to refresh kubeconfig: %w", err)
	}

	if clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderOmni {
		return nil
	}

	kubeconfigPath, pathErr := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if pathErr != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", pathErr)
	}

	_, statErr := os.Stat(kubeconfigPath)
	if statErr != nil {
		return fmt.Errorf(
			"kubeconfig not available after Omni refresh at %q: %w",
			kubeconfigPath, statErr,
		)
	}

	return nil
}

// kubeconfig and Docker client. Returns nil when clients cannot be created
// (the provisioner will fall back to static defaults).
func buildComponentDetector(
	cmd *cobra.Command,
	ctx *localregistry.Context,
) *detector.ComponentDetector {
	helmClient, kubeconfig, err := setup.HelmClientForCluster(ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot create Helm client for component detection, using defaults: %v", err)

		return nil
	}

	k8sContext := ctx.ClusterCfg.Spec.Cluster.Connection.Context
	if k8sContext == "" {
		clusterName := resolveClusterNameFromContext(ctx)
		k8sContext = ctx.ClusterCfg.Spec.Cluster.Distribution.ContextName(clusterName)
	}

	k8sClientset, err := k8s.NewClientset(kubeconfig, k8sContext)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot create K8s clientset for component detection, using defaults: %v", err)

		return nil
	}

	// Docker client is optional — only needed for cloud-provider-kind detection.
	dockerClient, _ := docker.GetDockerClient()

	return detector.NewComponentDetector(helmClient, k8sClientset, dockerClient)
}

// computeUpdateDiff retrieves current config and computes the full diff.
// Returns an error if current config could not be retrieved; the caller should
// surface the error rather than silently recreating the cluster.
func computeUpdateDiff(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	updater clusterprovisioner.Updater,
	clusterName string,
) (*v1alpha1.ClusterSpec, *clusterupdate.UpdateResult, error) {
	currentSpec, currentProvider, err := updater.GetCurrentConfig(cmd.Context())
	if err != nil {
		return nil, nil, fmt.Errorf(
			"could not retrieve current cluster configuration: %w", err,
		)
	}

	diffEngine := specdiff.NewEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	diff := diffEngine.ComputeDiff(
		currentSpec, &ctx.ClusterCfg.Spec.Cluster,
		currentProvider, &ctx.ClusterCfg.Spec.Provider,
	)

	provisionerDiff, diffErr := updater.DiffConfig(
		cmd.Context(), clusterName, currentSpec, &ctx.ClusterCfg.Spec.Cluster,
	)
	if diffErr == nil {
		specdiff.MergeProvisionerDiff(diff, provisionerDiff)
	}

	// Check for workload tag drift (stale GitOps sync ref)
	checkWorkloadTagDrift(cmd, ctx, diffEngine, diff)

	return currentSpec, diff, nil
}

// computeSpecOnlyDiff computes a spec-level diff using default values as
// the baseline current state. This is used for provisioners that do not
// implement the Updater interface (e.g., VCluster) to avoid blind recreation
// when there are no actual configuration changes.
func computeSpecOnlyDiff(
	cmd *cobra.Command,
	ctx *localregistry.Context,
) *clusterupdate.UpdateResult {
	currentSpec := clusterupdate.DefaultCurrentSpec(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	// Use component detection when available to get more accurate baseline.
	componentDetector := buildComponentDetector(cmd, ctx)
	if componentDetector != nil {
		detected, err := componentDetector.DetectComponents(
			cmd.Context(),
			ctx.ClusterCfg.Spec.Cluster.Distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
		)
		if err == nil {
			currentSpec.CNI = detected.CNI
			currentSpec.CSI = detected.CSI
			currentSpec.MetricsServer = detected.MetricsServer
			currentSpec.LoadBalancer = detected.LoadBalancer
			currentSpec.CertManager = detected.CertManager
			currentSpec.PolicyEngine = detected.PolicyEngine
			currentSpec.GitOpsEngine = detected.GitOpsEngine
		}
	}

	diffEngine := specdiff.NewEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	// Build the target spec, applying any distribution-specific overrides so
	// the diff reflects what the distribution will actually install rather than
	// what the user requested.  Copying avoids mutating the shared context.
	targetSpec := ctx.ClusterCfg.Spec.Cluster
	applyDistributionSpecOverrides(&targetSpec)

	diff := diffEngine.ComputeDiff(
		currentSpec,
		&targetSpec,
		nil,
		&ctx.ClusterCfg.Spec.Provider,
	)

	// Check for workload tag drift (stale GitOps sync ref)
	checkWorkloadTagDrift(cmd, ctx, diffEngine, diff)

	return diff
}

// applyDistributionSpecOverrides normalises a ClusterSpec by clearing fields
// that the given distribution will never install, so that update dry-runs do
// not report spurious "install X" changes for features that are silently skipped
// at cluster-creation time.
func applyDistributionSpecOverrides(spec *v1alpha1.ClusterSpec) {
	if spec.Distribution == v1alpha1.DistributionKWOK {
		// KWOK cannot run admission-webhook servers (simulated pods have no
		// real network), so policy engines are always skipped at creation time.
		// Treat PolicyEngine as None so the diff stays clean.
		spec.PolicyEngine = v1alpha1.PolicyEngineNone

		// The flux-operator pod is simulated and never installs Flux CRDs, so
		// Flux is always skipped at creation time (GetComponentRequirements sets
		// NeedsFlux=false for KWOK). Treat GitOpsEngine as None for Flux so that
		// update dry-runs do not report spurious "install Flux" changes.
		if spec.GitOpsEngine == v1alpha1.GitOpsEngineFlux {
			spec.GitOpsEngine = v1alpha1.GitOpsEngineNone
		}

		// NeedsLoadBalancerInstall always returns false for KWOK (no real network
		// dataplane). Normalise LoadBalancer to Disabled so that the update diff
		// sees Disabled on both sides and reports no change.
		spec.LoadBalancer = v1alpha1.LoadBalancerDisabled

		// KWOK runs simulated pods with no real network dataplane, so CNI plugins
		// (Calico, Cilium, etc.) are never installed at creation time. Normalise CNI
		// to Default so that update dry-runs do not report spurious CNI change diffs.
		spec.CNI = v1alpha1.CNIDefault

		// CSI node-plugin pods are simulated and never become Ready on KWOK.
		// Normalise CSI to Disabled so update dry-runs do not report a spurious
		// "install CSI" change for a feature that is silently skipped at creation.
		spec.CSI = v1alpha1.CSIDisabled

		// cert-manager admission webhook pods are simulated on KWOK and never run
		// real TLS logic. Normalise CertManager to Disabled for the same reason.
		spec.CertManager = v1alpha1.CertManagerDisabled
	}
}

// checkWorkloadTagDrift queries the running GitOps sync resource for its current
// tag (FluxInstance.sync.ref or ArgoCD Application.targetRevision) and compares
// it against the desired tag from configuration. If they differ, an in-place
// change is appended to the diff result. This detects stale sync refs left by
// pre-v6.7.1 cluster creation.
// Errors during cluster queries are logged as warnings and skipped — they should
// not block the rest of the update.
func checkWorkloadTagDrift(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	diffEngine *specdiff.Engine,
	diff *clusterupdate.UpdateResult,
) {
	gitOpsEngine := ctx.ClusterCfg.Spec.Cluster.GitOpsEngine
	if gitOpsEngine == v1alpha1.GitOpsEngineNone || gitOpsEngine == "" {
		return
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(ctx.ClusterCfg)
	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot resolve kubeconfig path for workload tag drift detection: %v", err)

		return
	}

	desiredTag := fluxinstaller.ResolveDesiredTag(ctx.ClusterCfg)

	var currentTag string

	switch gitOpsEngine { //nolint:exhaustive // None/empty already filtered above
	case v1alpha1.GitOpsEngineFlux:
		currentTag, err = fluxinstaller.GetCurrentSyncRef(cmd.Context(), kubeconfigPath)
	case v1alpha1.GitOpsEngineArgoCD:
		currentTag, err = getCurrentArgoCDTargetRevision(cmd.Context(), kubeconfigPath)
	default:
		return
	}

	if err != nil {
		notify.Warningf(cmd.OutOrStderr(),
			"Cannot query current GitOps sync ref for drift detection: %v", err)

		return
	}

	// Empty current tag means the resource does not exist yet — no drift to fix.
	if currentTag == "" {
		return
	}

	diffEngine.CheckWorkloadTag(currentTag, desiredTag, gitOpsEngine, diff)
}

// getCurrentArgoCDTargetRevision queries the ArgoCD Application for its current
// targetRevision. Returns empty string if the Application does not exist.
func getCurrentArgoCDTargetRevision(
	goCtx context.Context,
	kubeconfigPath string,
) (string, error) {
	mgr, err := argocdclient.NewManagerFromKubeconfig(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("create argocd manager: %w", err)
	}

	rev, err := mgr.GetCurrentTargetRevision(goCtx, "")
	if err != nil {
		return "", fmt.Errorf("get argocd target revision: %w", err)
	}

	return rev, nil
}

// applyOrReportChanges handles dry-run, recreate-required, no-changes, and
// in-place change application.
func applyOrReportChanges(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	updater clusterprovisioner.Updater,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
) error {
	dryRun := cfgManager.Viper.GetBool("dry-run")
	force := resolveForce(cfgManager.Viper.GetBool("force"), cmd.Flags().Lookup("yes"))

	if dryRun {
		return reportDryRun(cmd, diff)
	}

	if diff.HasRecreateRequired() {
		return handleRecreateRequired(cmd, cfgManager, ctx, deps, clusterName, diff, force)
	}

	if !diff.HasInPlaceChanges() && !diff.HasRebootRequired() {
		notify.Infof(cmd.OutOrStdout(), "No changes detected")

		return nil
	}

	// Reboot-required changes are disruptive — require confirmation unless --force
	if diff.HasRebootRequired() && !confirm.ShouldSkipPrompt(force) {
		var block strings.Builder

		fmt.Fprintf(&block, "%d changes require node reboots:\n", len(diff.RebootRequired))

		for _, change := range diff.RebootRequired {
			fmt.Fprintf(&block, "  ⚠ %s: %s → %s. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}

		notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"Type \"yes\" to proceed with reboot-required changes: ",
		)

		if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
			notify.Infof(cmd.OutOrStdout(), "Update cancelled")

			return nil
		}
	}

	reconciler := newComponentReconciler(cmd, ctx.ClusterCfg, clusterName)

	return applyInPlaceChanges(
		cmd, updater, reconciler, clusterName,
		currentSpec, ctx, diff, outputTimer,
	)
}

// reportDryRun prints a summary for dry-run mode and confirms no changes were applied.
// When --output json is set, emits machine-readable JSON only for the empty-diff case
// (displayChangesSummary already emits JSON when TotalChanges() > 0).
func reportDryRun(cmd *cobra.Command, diff *clusterupdate.UpdateResult) error {
	if getOutputFormat(cmd) == outputFormatJSON {
		// displayChangesSummary already emitted JSON when TotalChanges() > 0.
		// Only emit JSON here for the empty-diff case so CI/MCP still get a result.
		if diff != nil && diff.TotalChanges() == 0 {
			emitDiffJSON(cmd, diff)
		}

		return nil
	}

	if diff != nil && diff.TotalChanges() == 0 {
		notify.Infof(cmd.OutOrStdout(), "No changes detected")

		return nil
	}

	notify.Infof(cmd.OutOrStdout(), "Dry run complete. No changes applied.")

	return nil
}

// handleRecreateRequired warns about recreate-required changes and proceeds
// with recreation, prompting for confirmation unless --force is set.
func handleRecreateRequired(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	force bool,
) error {
	var block strings.Builder

	fmt.Fprintf(&block, "%d changes require cluster recreation:\n", len(diff.RecreateRequired))

	for _, change := range diff.RecreateRequired {
		fmt.Fprintf(&block, "  ✗ %s: cannot change from %s to %s in-place. %s\n",
			change.Field, change.OldValue, change.NewValue, change.Reason,
		)
	}

	notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

	return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
}

// applyInPlaceChanges applies provisioner-level and component-level changes in-place.
func applyInPlaceChanges(
	cmd *cobra.Command,
	updater clusterprovisioner.Updater,
	reconciler *componentReconciler,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	ctx *localregistry.Context,
	diff *clusterupdate.UpdateResult,
	outputTimer timer.Timer,
) error {
	updateOpts := clusterupdate.UpdateOptions{
		DryRun:        false,
		RollingReboot: true,
	}

	notify.Titlef(cmd.OutOrStdout(), "🔄", "Applying changes...")

	// Apply provisioner-level changes (node scaling, Talos config, etc.)
	result, err := updater.Update(
		cmd.Context(),
		clusterName,
		currentSpec,
		&ctx.ClusterCfg.Spec.Cluster,
		updateOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to apply updates: %w", err)
	}

	// Apply component-level changes (CNI, CSI, cert-manager, etc.)
	componentErr := reconciler.reconcileComponents(cmd.Context(), diff, result)

	// Display results
	if len(result.AppliedChanges) > 0 {
		notify.SuccessWithTimerf(cmd.OutOrStdout(), outputTimer,
			"applied %d changes successfully", len(result.AppliedChanges),
		)
	}

	if len(result.FailedChanges) > 0 {
		var failBlock strings.Builder

		fmt.Fprintf(&failBlock, "%d changes failed to apply:\n", len(result.FailedChanges))

		for _, change := range result.FailedChanges {
			fmt.Fprintf(&failBlock, "  - %s: %s\n", change.Field, change.Reason)
		}

		notify.Errorf(cmd.OutOrStderr(), strings.TrimRight(failBlock.String(), "\n"))
	}

	if componentErr != nil {
		return fmt.Errorf("some component changes failed to apply: %w", componentErr)
	}

	return nil
}

// displayChangesSummary outputs a human-readable summary of configuration changes
// as a before/after table with one row per changed field and impact icons.
// Rows are ordered by severity: recreate-required → reboot-required → in-place.
// Fields with no change are omitted.
// When --output json is set, emits machine-readable JSON instead of the table.
func displayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	totalChanges := diff.TotalChanges()

	if totalChanges == 0 {
		return
	}

	if getOutputFormat(cmd) == outputFormatJSON {
		emitDiffJSON(cmd, diff)

		return
	}

	notify.Titlef(cmd.OutOrStdout(), "🔍", "Change summary")

	notify.Infof(
		cmd.OutOrStdout(),
		formatDiffTable(diff, totalChanges),
	)
}

// diffRow holds a single row of the diff table.
type diffRow struct {
	icon   string
	field  string
	oldVal string
	newVal string
	impact string
}

// categoryIcon returns the severity icon for a change category.
func categoryIcon(cat clusterupdate.ChangeCategory) string {
	switch cat {
	case clusterupdate.ChangeCategoryRecreateRequired:
		return "🔴"
	case clusterupdate.ChangeCategoryRebootRequired:
		return "🟡"
	case clusterupdate.ChangeCategoryInPlace:
		return "🟢"
	default:
		return "⚪"
	}
}

// formatDiffTable builds the formatted diff table string.
// The table has four columns: Component, Before, After, Impact.
// Rows are ordered by severity: 🔴 recreate → 🟡 reboot → 🟢 in-place.
func formatDiffTable(
	diff *clusterupdate.UpdateResult,
	totalChanges int,
) string {
	rows := collectDiffRows(diff, totalChanges)

	// Column headers
	const (
		hdrComponent = "Component"
		hdrBefore    = "Before"
		hdrAfter     = "After"
		hdrImpact    = "Impact"
	)

	colW, colB, colA, colI := computeColumnWidths(
		rows, hdrComponent, hdrBefore, hdrAfter, hdrImpact,
	)

	var block strings.Builder

	// Pre-allocate: each row needs ~colW+colB+colA+colI bytes for data,
	// plus ~16 bytes overhead per row for spacing (6), emoji (4), newlines, padding.
	const tableOverheadRows = 4 // summary, header, separator, trailing

	const perRowPadding = 16 // spacing + emoji + newline

	block.Grow((totalChanges + tableOverheadRows) * (colW + colB + colA + colI + perRowPadding))

	writeSummaryLine(&block, totalChanges)
	writeHeaderRow(&block, colW, colB, colA, hdrComponent, hdrBefore, hdrAfter, hdrImpact)
	writeSeparatorRow(&block, colW, colB, colA, colI)
	writeDataRows(&block, rows, colW, colB, colA)

	return strings.TrimRight(block.String(), "\n")
}

// collectDiffRows builds an ordered list of diff rows.
// Order: 🔴 recreate-required → 🟡 reboot-required → 🟢 in-place.
func collectDiffRows(
	diff *clusterupdate.UpdateResult,
	totalChanges int,
) []diffRow {
	rows := make([]diffRow, 0, totalChanges)

	for _, c := range diff.RecreateRequired {
		rows = append(rows, diffRow{
			categoryIcon(c.Category), c.Field, c.OldValue, c.NewValue, c.Category.String(),
		})
	}

	for _, c := range diff.RebootRequired {
		rows = append(rows, diffRow{
			categoryIcon(c.Category), c.Field, c.OldValue, c.NewValue, c.Category.String(),
		})
	}

	for _, c := range diff.InPlaceChanges {
		rows = append(rows, diffRow{
			categoryIcon(c.Category), c.Field, c.OldValue, c.NewValue, c.Category.String(),
		})
	}

	return rows
}

// computeColumnWidths returns the max width for each table column.
func computeColumnWidths(
	rows []diffRow,
	hdrComp, hdrBefore, hdrAfter, hdrImpact string,
) (int, int, int, int) {
	widthComp := len(hdrComp)
	widthBefore := len(hdrBefore)
	widthAfter := len(hdrAfter)
	widthImpact := len(hdrImpact)

	for _, row := range rows {
		if length := len(row.field); length > widthComp {
			widthComp = length
		}

		if length := len(row.oldVal); length > widthBefore {
			widthBefore = length
		}

		if length := len(row.newVal); length > widthAfter {
			widthAfter = length
		}

		if length := len(row.impact); length > widthImpact {
			widthImpact = length
		}
	}

	return widthComp, widthBefore, widthAfter, widthImpact
}

func writeSummaryLine(block *strings.Builder, totalChanges int) {
	fmt.Fprintf(block, "Detected %d configuration changes:\n\n", totalChanges)
}

// headerIndent is the number of leading spaces in the header and separator rows.
// This visually aligns with the emoji+space prefix in data rows:
// emoji renders as 2 terminal columns + 1 trailing space = 3 visual columns.
const headerIndent = "   "

func writeHeaderRow(
	block *strings.Builder,
	colW, colB, colA int,
	hdrComp, hdrBefore, hdrAfter, hdrImpact string,
) {
	fmt.Fprintf(block, "%s%-*s  %-*s  %-*s  %s\n",
		headerIndent,
		colW, hdrComp, colB, hdrBefore, colA, hdrAfter, hdrImpact)
}

func writeSeparatorRow(
	block *strings.Builder,
	colW, colB, colA, colI int,
) {
	fmt.Fprintf(block, "%s%s  %s  %s  %s\n",
		headerIndent,
		strings.Repeat("─", colW),
		strings.Repeat("─", colB),
		strings.Repeat("─", colA),
		strings.Repeat("─", colI))
}

func writeDataRows(
	block *strings.Builder,
	rows []diffRow,
	colW, colB, colA int,
) {
	for _, r := range rows {
		fmt.Fprintf(block, "%s %-*s  %-*s  %-*s  %s\n",
			r.icon, colW, r.field,
			colB, r.oldVal,
			colA, r.newVal,
			r.impact)
	}
}

// confirmRecreate prompts the user to confirm cluster recreation unless --force is set.
// It returns true if the update should proceed (confirmed or forced), and false if the user cancels.
func confirmRecreate(cmd *cobra.Command, clusterName string, force bool) bool {
	if confirm.ShouldSkipPrompt(force) {
		return true
	}

	var prompt strings.Builder

	prompt.WriteString(
		"Update will delete and recreate the cluster.\n",
	)
	prompt.WriteString("All workloads and data will be lost.")

	notify.Warningf(cmd.OutOrStderr(), "%s", prompt.String())

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"Type \"yes\" to proceed with updating cluster %q: ", clusterName,
	)

	if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
		notify.Infof(cmd.OutOrStdout(), "Update cancelled")

		return false
	}

	return true
}

// executeRecreateFlow performs the delete + create flow with confirmation.
func executeRecreateFlow(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	force bool,
) error {
	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	if !confirmRecreate(cmd, clusterName, force) {
		return nil
	}

	// Create provisioner for delete
	factory := newProvisionerFactory(ctx)

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Disconnect registries from Docker network before deletion.
	// Required for distributions like VCluster and Talos because their provisioners
	// destroy the Docker network during deletion, which fails if containers are
	// still connected. Registries are reused on recreate, so only disconnect is needed.
	if ctx.ClusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderDocker {
		disconnectRegistriesBeforeDelete(cmd, &clusterdetector.Info{
			Distribution: ctx.ClusterCfg.Spec.Cluster.Distribution,
			ClusterName:  clusterName,
		})
	}

	// Execute delete
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "🗑️",
		Content: "deleting existing cluster",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = provisioner.Delete(cmd.Context(), clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete existing cluster: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Execute create using shared workflow
	return runClusterCreationWorkflow(cmd, cfgManager, ctx, deps)
}

// resolveForce returns true if the viper-resolved force flag is set,
// or if the --yes flag was explicitly set to true on the command line.
// This consolidates the --force/--yes alias logic into one place.
func resolveForce(viperForce bool, yesFlag *pflag.Flag) bool {
	return viperForce || (yesFlag != nil && yesFlag.Changed && yesFlag.Value.String() == "true")
}
