package cluster

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	backupsvc "github.com/devantler-tech/ksail/v7/pkg/svc/backup"
	"github.com/spf13/cobra"
)

// BackupMetadata is a re-export of [backupsvc.BackupMetadata]; the archive
// contract lives in pkg/svc/backup. Kept here for backwards compatibility with
// callers and tests that reference the cmd-package name.
type BackupMetadata = backupsvc.BackupMetadata

type backupFlags struct {
	outputPath       string
	namespaces       []string
	excludeTypes     []string
	compressionLevel int
	name             string
}

// NewBackupCmd creates the cluster backup command.
func NewBackupCmd() *cobra.Command {
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
			annotations.AnnotationPermission: permissionWrite,
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
	// --name is long-only here: backup keeps -n for --namespaces (the -n=--name
	// reservation across the cluster group is a separate breaking change).
	cmd.Flags().StringVar(
		&flags.name, "name", "",
		"Name of the cluster to back up (resolves the kubeconfig like the other cluster "+
			"commands; defaults to the current kubeconfig context when unset)",
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

	//nolint:contextcheck // resolveTarget→ResolveClusterInfo derives ctx from cmd (cluster-cmd convention)
	kubeconfigPath, kubeContext, err := resolveTarget(cmd, flags.name)
	if err != nil {
		return err
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

	backupper := backupsvc.NewBackupper(backupsvc.BackupOptions{
		KubeconfigPath:   kubeconfigPath,
		Context:          kubeContext,
		OutputPath:       flags.outputPath,
		Namespaces:       flags.namespaces,
		ExcludeTypes:     flags.excludeTypes,
		CompressionLevel: flags.compressionLevel,
	})

	err = backupper.Backup(ctx, writer)
	if err != nil {
		return err //nolint:wrapcheck // engine already wraps with "failed to create backup"
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

// defaultCompressionLevel uses gzip.DefaultCompression so the constant stays
// co-located with the gzip import and avoids a magic number in backup.go.
const defaultCompressionLevel = gzip.DefaultCompression
