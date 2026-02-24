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

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
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

	err := cmd.MarkFlagRequired("input")
	if err != nil {
		panic(fmt.Sprintf("failed to mark input flag as required: %v", err))
	}

	return cmd
}

func runRestore(
	_ context.Context,
	cmd *cobra.Command,
	flags *restoreFlags,
) error {
	if flags.existingResourcePolicy != resourcePolicyNone &&
		flags.existingResourcePolicy != resourcePolicyUpdate {
		return ErrInvalidResourcePolicy
	}

	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	if kubeconfigPath == "" {
		return ErrKubeconfigNotFound
	}

	writer := cmd.OutOrStdout()
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

	tmpDir, metadata, err := extractBackupArchive(flags.inputPath)
	if err != nil {
		return fmt.Errorf("failed to extract backup: %w", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	printRestoreMetadata(writer, metadata)

	_, _ = fmt.Fprintf(writer, "Restoring cluster resources...\n")

	err = restoreResources(kubeconfigPath, tmpDir, writer, flags)
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

func printRestoreMetadata(writer io.Writer, metadata *BackupMetadata) {
	_, _ = fmt.Fprintf(writer, "Backup metadata:\n")
	_, _ = fmt.Fprintf(writer, "   Version: %s\n", metadata.Version)
	_, _ = fmt.Fprintf(
		writer, "   Timestamp: %s\n",
		metadata.Timestamp.Format("2006-01-02 15:04:05"),
	)
	_, _ = fmt.Fprintf(writer, "   Cluster: %s\n", metadata.ClusterName)
	_, _ = fmt.Fprintf(
		writer, "   Resources: %d\n", metadata.ResourceCount,
	)
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
			err = os.MkdirAll(targetPath, dirPerm)
			if err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			continue
		}

		err = os.MkdirAll(filepath.Dir(targetPath), dirPerm)
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

	destPrefix := destDir + string(filepath.Separator)
	targetPrefix := targetPath + string(filepath.Separator)

	if !strings.HasPrefix(targetPrefix, destPrefix) {
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
	kubeconfigPath, tmpDir string,
	writer io.Writer,
	flags *restoreFlags,
) error {
	resourcesDir := filepath.Join(tmpDir, "resources")

	var restoreErrors []string

	for _, resourceType := range backupResourceTypes() {
		resourceDir := filepath.Join(resourcesDir, resourceType)

		_, statErr := os.Stat(resourceDir)
		if os.IsNotExist(statErr) {
			continue
		}

		files, err := filepath.Glob(
			filepath.Join(resourceDir, "*.yaml"),
		)
		if err != nil {
			return fmt.Errorf(
				"failed to list files for %s: %w", resourceType, err,
			)
		}

		if len(files) == 0 {
			continue
		}

		for _, file := range files {
			err = restoreResourceFile(kubeconfigPath, file, flags)
			if err != nil {
				msg := fmt.Sprintf("%s: %v", filepath.Base(file), err)
				restoreErrors = append(restoreErrors, msg)

				_, _ = fmt.Fprintf(
					writer,
					"Warning: failed to restore %s: %v\n",
					filepath.Base(file), err,
				)

				continue
			}
		}

		_, _ = fmt.Fprintf(writer, "   Restored %s\n", resourceType)
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

func restoreResourceFile(
	kubeconfigPath, filePath string,
	flags *restoreFlags,
) error {
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

	args := []string{"-f", filePath}
	if flags.dryRun {
		args = append(args, "--dry-run=client")
	}

	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err != nil {
		stderr := errBuf.String()

		if flags.existingResourcePolicy == resourcePolicyNone &&
			allLinesContain(stderr, "already exists") {
			return nil
		}

		return fmt.Errorf(
			"kubectl failed: %w (output: %s)",
			err, stderr,
		)
	}

	return nil
}

func allLinesContain(output, substr string) bool {
	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if !strings.Contains(trimmed, substr) {
			return false
		}
	}

	return true
}
