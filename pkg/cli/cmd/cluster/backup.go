package cluster

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	backupsvc "github.com/devantler-tech/ksail/v7/pkg/svc/backup"
	"github.com/spf13/cobra"
)

// ErrArchivePathRequired is returned when neither the archive-path positional
// argument nor its deprecated path flag (--output/--input) is provided.
var ErrArchivePathRequired = errors.New("archive path is required")

// resolveArchivePath returns the archive path for backup/restore, preferring the
// positional argument and falling back to the deprecated path flag value. It
// centralizes the positional-or-deprecated-flag resolution so backup and restore
// stay consistent (and so the deprecation window has one code path to remove).
// deprecatedFlagDisplay is the user-facing flag name (e.g. "--output") shown in
// the error message.
func resolveArchivePath(
	args []string,
	deprecatedFlagValue string,
	deprecatedFlagDisplay string,
) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	if deprecatedFlagValue != "" {
		return deprecatedFlagValue, nil
	}

	return "", fmt.Errorf(
		"%w: pass it as the first positional argument (deprecated alias: %s)",
		ErrArchivePathRequired, deprecatedFlagDisplay,
	)
}

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
		Use:   "backup [<output>]",
		Short: "Backup cluster resources",
		Long: `Creates a backup archive containing Kubernetes resource manifests.

The backup is stored as a compressed tarball (.tar.gz) with resources ` +
			`organized by type.
Metadata about the backup is included for restore operations.

Note: This backs up resource manifests (YAML) only. Persistent volume
contents are not included in the current implementation.

Example:
  ksail cluster backup ./my-backup.tar.gz
  ksail cluster backup ./backup.tar.gz --namespaces default,kube-system
  ksail cluster backup ./backup.tar.gz --exclude-types events,pods`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackup(cmd.Context(), cmd, flags, args)
		},
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	registerBackupFlags(cmd, flags)

	return cmd
}

// registerBackupFlags wires backup's flags, including the deprecation paths: the
// archive path is now a positional argument, so --output/-o becomes a hidden
// deprecated alias; and -n is reserved for --name across the cluster group, so
// the -n shorthand on --namespaces is deprecated (the long --namespaces stays).
func registerBackupFlags(cmd *cobra.Command, flags *backupFlags) {
	// Deprecated: the archive path is now the first positional argument.
	cmd.Flags().StringVarP(
		&flags.outputPath, "output", "o", "",
		"Deprecated: pass the archive path as the first positional argument instead",
	)
	_ = cmd.Flags().MarkDeprecated("output",
		"pass the archive path as the first positional argument (ksail cluster backup <path>)")

	// --namespaces keeps its long form; the legacy -n shorthand is registered but
	// deprecated for one release while -n is reserved for --name. It must be
	// registered via StringSliceVarP so pflag records the shorthand in its lookup
	// map (setting Flag.Shorthand after AddFlag does not), then marked deprecated.
	cmd.Flags().StringSliceVarP(
		&flags.namespaces, "namespaces", "n", []string{},
		"Namespaces to backup (default: all)",
	)
	_ = cmd.Flags().MarkShorthandDeprecated("namespaces",
		"-n is reserved for --name across the cluster group; use the long --namespaces flag")

	cmd.Flags().StringSliceVar(
		&flags.excludeTypes, "exclude-types", []string{"events"},
		"Resource types to exclude from backup",
	)
	cmd.Flags().IntVar(
		&flags.compressionLevel, "compression", defaultCompressionLevel,
		"Compression level (-1..9, -1 = gzip default)",
	)
	// --name is long-only this release: -n still maps to --namespaces (deprecated)
	// until the deprecation window closes, after which -n becomes --name.
	cmd.Flags().StringVar(
		&flags.name, "name", "",
		"Name of the cluster to back up (resolves the kubeconfig like the other cluster "+
			"commands; defaults to the current kubeconfig context when unset)",
	)
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

func runBackup(
	ctx context.Context,
	cmd *cobra.Command,
	flags *backupFlags,
	args []string,
) error {
	outputPath, err := resolveArchivePath(args, flags.outputPath, "--output")
	if err != nil {
		return err
	}

	flags.outputPath = outputPath

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
