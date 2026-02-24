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

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
	// bytesPerMB is the number of bytes in a megabyte.
	bytesPerMB = 1024 * 1024
)

// ErrKubeconfigNotFound is returned when no kubeconfig path can be resolved.
var ErrKubeconfigNotFound = errors.New(
	"kubeconfig not found; ensure cluster is created and configured",
)

// BackupMetadata contains metadata about a backup.
type BackupMetadata struct {
	Version       string    `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
	ClusterName   string    `json:"clusterName"`
	KSailVersion  string    `json:"ksailVersion"`
	ResourceCount int       `json:"resourceCount"`
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
		includeVolumes:   true,
		compressionLevel: gzip.DefaultCompression,
	}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup cluster resources and volumes",
		Long: `Creates a backup archive containing Kubernetes resources ` +
			`and persistent volume data.

The backup is stored as a compressed tarball (.tar.gz) with resources ` +
			`organized by namespace.
Metadata about the backup is included for restore operations.

Example:
  ksail cluster backup --output ./my-backup.tar.gz
  ksail cluster backup -o ./backup.tar.gz --namespaces default,kube-system
  ksail cluster backup -o ./backup.tar.gz --exclude-types events,pods`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackup(cmd.Context(), cmd, flags)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(
		&flags.outputPath, "output", "o", "",
		"Output path for backup archive (required)",
	)
	cmd.Flags().BoolVar(
		&flags.includeVolumes, "include-volumes", true,
		"Include persistent volume data in backup",
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
		&flags.compressionLevel, "compression", gzip.DefaultCompression,
		"Compression level (0-9, default: -1 (gzip default))",
	)

	err := cmd.MarkFlagRequired("output")
	if err != nil {
		panic(fmt.Sprintf("failed to mark output flag as required: %v", err))
	}

	return cmd
}

func runBackup(_ context.Context, cmd *cobra.Command, flags *backupFlags) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	if kubeconfigPath == "" {
		return ErrKubeconfigNotFound
	}

	writer := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(writer, "Starting cluster backup...\n")
	_, _ = fmt.Fprintf(writer, "   Output: %s\n", flags.outputPath)
	_, _ = fmt.Fprintf(writer, "   Include volumes: %v\n", flags.includeVolumes)

	if len(flags.namespaces) > 0 {
		_, _ = fmt.Fprintf(writer, "   Namespaces: %v\n", flags.namespaces)
	} else {
		_, _ = fmt.Fprintf(writer, "   Namespaces: all\n")
	}

	outputDir := filepath.Dir(flags.outputPath)
	if outputDir != "." && outputDir != "" {
		err := os.MkdirAll(outputDir, dirPerm)
		if err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	err := createBackupArchive(kubeconfigPath, writer, flags)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	_, _ = fmt.Fprintf(writer, "Backup completed successfully\n")

	info, err := os.Stat(flags.outputPath)
	if err == nil {
		sizeMB := float64(info.Size()) / bytesPerMB
		_, _ = fmt.Fprintf(writer, "   Archive size: %.2f MB\n", sizeMB)
	}

	return nil
}

func createBackupArchive(
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
		KSailVersion: "5.0.0",
	}

	_, _ = fmt.Fprintf(writer, "Exporting cluster resources...\n")

	resourceCount := exportResources(
		kubeconfigPath, tmpDir, writer, flags,
	)

	metadata.ResourceCount = resourceCount

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

func exportResources(
	kubeconfigPath, outputDir string,
	writer io.Writer,
	flags *backupFlags,
) int {
	filteredTypes := filterExcludedTypes(
		backupResourceTypes(), flags.excludeTypes,
	)
	totalCount := 0

	for _, resourceType := range filteredTypes {
		count, err := exportResourceType(
			kubeconfigPath, outputDir, resourceType, flags,
		)
		if err != nil {
			_, _ = fmt.Fprintf(
				writer,
				"Warning: failed to export %s: %v\n",
				resourceType, err,
			)

			continue
		}

		if count > 0 {
			_, _ = fmt.Fprintf(
				writer, "   Exported %d %s\n", count, resourceType,
			)
			totalCount += count
		}
	}

	return totalCount
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
	kubeconfigPath, outputDir, resourceType string,
	flags *backupFlags,
) (int, error) {
	resourceDir := filepath.Join(outputDir, "resources", resourceType)

	err := os.MkdirAll(resourceDir, dirPerm)
	if err != nil {
		return 0, fmt.Errorf("failed to create resource directory: %w", err)
	}

	if len(flags.namespaces) > 0 {
		totalCount := 0

		for _, ns := range flags.namespaces {
			count, err := executeGetAndSave(
				kubeconfigPath, resourceDir, resourceType, ns,
			)
			if err != nil {
				return totalCount, err
			}

			totalCount += count
		}

		return totalCount, nil
	}

	return executeGetAndSave(
		kubeconfigPath, resourceDir, resourceType, "",
	)
}

func executeGetAndSave(
	kubeconfigPath, resourceDir, resourceType, namespace string,
) (int, error) {
	filename := resourceType + ".yaml"
	if namespace != "" {
		filename = fmt.Sprintf("%s-%s.yaml", resourceType, namespace)
	}

	outputPath := filepath.Join(resourceDir, filename)

	output, err := runKubectlGet(kubeconfigPath, resourceType, namespace)
	if err != nil {
		if strings.Contains(err.Error(), "the server doesn't have a resource type") {
			return 0, nil
		}

		return 0, fmt.Errorf("failed to get resources: %w", err)
	}

	if len(output) == 0 || strings.Contains(output, "No resources found") {
		return 0, nil
	}

	err = os.WriteFile(outputPath, []byte(output), filePerm)
	if err != nil {
		return 0, fmt.Errorf("failed to write resource file: %w", err)
	}

	count := countYAMLDocuments(output)

	return count, nil
}

func runKubectlGet(
	kubeconfigPath, resourceType, namespace string,
) (string, error) {
	var outBuf, errBuf bytes.Buffer

	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    &outBuf,
		ErrOut: &errBuf,
	})

	getCmd := client.CreateGetCommand(kubeconfigPath)

	args := []string{resourceType, "-o", "yaml"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	getCmd.SetArgs(args)
	getCmd.SilenceUsage = true
	getCmd.SilenceErrors = true

	err := getCmd.Execute()
	if err != nil {
		return errBuf.String(), fmt.Errorf(
			"kubectl get %s: %w", resourceType, err,
		)
	}

	return outBuf.String(), nil
}

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
	outFile, err := os.Create(targetPath) //nolint:gosec // path is user-controlled output
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	defer func() { _ = outFile.Close() }()

	gzipWriter, err := gzip.NewWriterLevel(outFile, compressionLevel)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}

	defer func() { _ = gzipWriter.Close() }()

	tarWriter := tar.NewWriter(gzipWriter)

	defer func() { _ = tarWriter.Close() }()

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
		return fmt.Errorf("failed to walk source directory: %w", err)
	}

	return nil
}

func addFileToTar(
	tarWriter *tar.Writer,
	sourceDir, path string,
	info os.FileInfo,
) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create tar header: %w", err)
	}

	relPath, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	header.Name = relPath

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if info.IsDir() {
		return nil
	}

	file, err := os.Open(path) //nolint:gosec // path from controlled Walk
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

func getClusterNameFromKubeconfig(kubeconfigPath string) string {
	return filepath.Base(kubeconfigPath)
}

func countYAMLDocuments(content string) int {
	count := 0

	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, "kind:") {
			count++
		}
	}

	if count == 0 {
		return 1
	}

	return count
}
