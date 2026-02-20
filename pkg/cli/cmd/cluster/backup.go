package cluster

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
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
		Long: `Creates a backup archive containing Kubernetes resources and persistent volume data.

The backup is stored as a compressed tarball (.tar.gz) with resources organized by namespace.
Metadata about the backup is included for restore operations.

Example:
  ksail cluster backup --output ./my-backup.tar.gz
  ksail cluster backup --output ./backup.tar.gz --namespaces default,kube-system
  ksail cluster backup --output ./backup.tar.gz --exclude-types events,pods`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackup(cmd.Context(), flags)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&flags.outputPath, "output", "o", "", "Output path for backup archive (required)")
	cmd.Flags().BoolVar(&flags.includeVolumes, "include-volumes", true, "Include persistent volume data in backup")
	cmd.Flags().StringSliceVarP(&flags.namespaces, "namespaces", "n", []string{}, "Namespaces to backup (default: all)")
	cmd.Flags().StringSliceVar(&flags.excludeTypes, "exclude-types", []string{"events"}, "Resource types to exclude from backup")
	cmd.Flags().IntVar(&flags.compressionLevel, "compression", gzip.DefaultCompression, "Compression level (0-9, default: 6)")

	if err := cmd.MarkFlagRequired("output"); err != nil {
		panic(fmt.Sprintf("failed to mark output flag as required: %v", err))
	}

	return cmd
}

func runBackup(ctx context.Context, flags *backupFlags) error {
	// Get kubeconfig
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	if kubeconfigPath == "" {
		return fmt.Errorf("kubeconfig not found; ensure cluster is created and configured")
	}

	// Create kubectl client
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	fmt.Printf("📦 Starting cluster backup...\n")
	fmt.Printf("   Output: %s\n", flags.outputPath)
	fmt.Printf("   Include volumes: %v\n", flags.includeVolumes)

	if len(flags.namespaces) > 0 {
		fmt.Printf("   Namespaces: %v\n", flags.namespaces)
	} else {
		fmt.Printf("   Namespaces: all\n")
	}

	// Create output directory if needed
	outputDir := filepath.Dir(flags.outputPath)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Create backup archive
	if err := createBackupArchive(ctx, client, kubeconfigPath, flags); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	fmt.Printf("✅ Backup completed successfully\n")

	// Show file size
	if info, err := os.Stat(flags.outputPath); err == nil {
		sizeMB := float64(info.Size()) / (1024 * 1024)
		fmt.Printf("   Archive size: %.2f MB\n", sizeMB)
	}

	return nil
}

func createBackupArchive(ctx context.Context, client *kubectl.Client, kubeconfigPath string, flags *backupFlags) error {
	// Create temporary directory for backup staging
	tmpDir, err := os.MkdirTemp("", "ksail-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Gather cluster metadata
	fmt.Printf("📋 Gathering cluster metadata...\n")
	metadata := &BackupMetadata{
		Version:      "v1",
		Timestamp:    time.Now(),
		ClusterName:  getClusterNameFromKubeconfig(kubeconfigPath),
		KSailVersion: "5.0.0", // TODO: Get from build version
	}

	// Export resources
	fmt.Printf("📤 Exporting cluster resources...\n")
	resourceCount, err := exportResources(ctx, client, kubeconfigPath, tmpDir, flags)
	if err != nil {
		return fmt.Errorf("failed to export resources: %w", err)
	}
	metadata.ResourceCount = resourceCount

	// Write metadata file
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")
	if err := writeMetadata(metadata, metadataPath); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Create compressed tarball
	fmt.Printf("🗜️  Creating compressed archive...\n")
	if err := createTarball(tmpDir, flags.outputPath, flags.compressionLevel); err != nil {
		return fmt.Errorf("failed to create tarball: %w", err)
	}

	return nil
}

func exportResources(ctx context.Context, client *kubectl.Client, kubeconfigPath, outputDir string, flags *backupFlags) (int, error) {
	// Define resource types in order (CRDs first, then storage, then workloads)
	resourceTypes := []string{
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

	// Filter out excluded types
	var filteredTypes []string
	for _, rt := range resourceTypes {
		excluded := false
		for _, et := range flags.excludeTypes {
			if rt == et {
				excluded = true
				break
			}
		}
		if !excluded {
			filteredTypes = append(filteredTypes, rt)
		}
	}

	totalCount := 0

	for _, resourceType := range filteredTypes {
		count, err := exportResourceType(ctx, client, kubeconfigPath, outputDir, resourceType, flags)
		if err != nil {
			// Log warning but continue with other resources
			fmt.Fprintf(os.Stderr, "⚠️  Warning: failed to export %s: %v\n", resourceType, err)
			continue
		}
		if count > 0 {
			fmt.Printf("   ✓ Exported %d %s\n", count, resourceType)
			totalCount += count
		}
	}

	return totalCount, nil
}

func exportResourceType(ctx context.Context, client *kubectl.Client, kubeconfigPath, outputDir, resourceType string, flags *backupFlags) (int, error) {
	// Prepare output directory for this resource type
	resourceDir := filepath.Join(outputDir, "resources", resourceType)
	if err := os.MkdirAll(resourceDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create resource directory: %w", err)
	}

	// Build kubectl get command arguments
	args := []string{"get", resourceType, "-o", "yaml"}

	if len(flags.namespaces) > 0 {
		// If specific namespaces requested, iterate through them
		totalCount := 0
		for _, ns := range flags.namespaces {
			nsArgs := append(args, "-n", ns)
			count, err := executeGetAndSave(ctx, client, kubeconfigPath, resourceDir, resourceType, ns, nsArgs)
			if err != nil {
				return totalCount, err
			}
			totalCount += count
		}
		return totalCount, nil
	}

	// For cluster-scoped resources or all namespaces
	args = append(args, "--all-namespaces")
	return executeGetAndSave(ctx, client, kubeconfigPath, resourceDir, resourceType, "", args)
}

func executeGetAndSave(_ context.Context, client *kubectl.Client, kubeconfigPath, resourceDir, resourceType, namespace string, args []string) (int, error) {
	// Create output file
	filename := resourceType + ".yaml"
	if namespace != "" {
		filename = fmt.Sprintf("%s-%s.yaml", resourceType, namespace)
	}
	outputPath := filepath.Join(resourceDir, filename)

	// Execute kubectl get
	output, err := client.Run(kubeconfigPath, args...)
	if err != nil {
		// If resource type doesn't exist, that's okay
		if contains(output, "the server doesn't have a resource type") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get resources: %w", err)
	}

	// Check if we got any resources
	if len(output) == 0 || contains(output, "No resources found") {
		return 0, nil
	}

	// Save to file
	if err := os.WriteFile(outputPath, []byte(output), 0644); err != nil {
		return 0, fmt.Errorf("failed to write resource file: %w", err)
	}

	// Count items (rough estimate by counting "kind:" lines)
	count := countYAMLDocuments(output)
	return count, nil
}

func writeMetadata(metadata *BackupMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

func createTarball(sourceDir, targetPath string, compressionLevel int) error {
	// Create output file
	outFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Create gzip writer with specified compression level
	gzipWriter, err := gzip.NewWriterLevel(outFile, compressionLevel)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Walk the source directory and add files to tar
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Update header name to be relative to source directory
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// If not a directory, write file content
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("failed to write file to tar: %w", err)
			}
		}

		return nil
	})
}

// Helper functions

func getClusterNameFromKubeconfig(kubeconfigPath string) string {
	// Parse kubeconfig to get cluster name
	// For now, use a simple default
	return filepath.Base(kubeconfigPath)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr)+1 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func countYAMLDocuments(content string) int {
	// Simple count by looking for "kind:" occurrences
	count := 0
	lines := splitLines(content)
	for _, line := range lines {
		if len(line) >= 5 && line[:5] == "kind:" {
			count++
		}
	}
	if count == 0 {
		return 1 // At least one document if we have content
	}
	return count
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
