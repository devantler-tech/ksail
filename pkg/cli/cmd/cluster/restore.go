package cluster

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	backupsvc "github.com/devantler-tech/ksail/v7/pkg/svc/backup"
	"github.com/spf13/cobra"
)

// Restore engine sentinels are re-exported from pkg/svc/backup so errors.Is
// checks against the cmd-package names continue to work.
var (
	// ErrInvalidResourcePolicy is returned when an unsupported
	// existing-resource-policy value is provided.
	ErrInvalidResourcePolicy = backupsvc.ErrInvalidResourcePolicy
	// ErrInvalidTarPath is returned when a tar entry contains a path
	// traversal attempt.
	ErrInvalidTarPath = backupsvc.ErrInvalidTarPath
	// ErrSymlinkInArchive is returned when a tar archive contains
	// symbolic or hard links, which are not supported.
	ErrSymlinkInArchive = backupsvc.ErrSymlinkInArchive
	// ErrRestoreFailed is returned when one or more resources fail to restore.
	ErrRestoreFailed = backupsvc.ErrRestoreFailed
)

const (
	// resourcePolicyNone skips resources that already exist in the cluster.
	resourcePolicyNone = backupsvc.PolicyNone
	// resourcePolicyUpdate updates resources that already exist in the cluster.
	resourcePolicyUpdate = backupsvc.PolicyUpdate
)

type restoreFlags struct {
	inputPath              string
	existingResourcePolicy string
	dryRun                 bool
	name                   string
}

// NewRestoreCmd creates the cluster restore command.
func NewRestoreCmd() *cobra.Command {
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
	cmd.Flags().StringVar(
		&flags.name, "name", "",
		"Name of the cluster to restore into (resolves the kubeconfig like the other "+
			"cluster commands; defaults to the current kubeconfig context when unset)",
	)

	cobra.CheckErr(cmd.MarkFlagRequired("input"))

	return cmd
}

func runRestore(
	ctx context.Context,
	cmd *cobra.Command,
	flags *restoreFlags,
) error {
	//nolint:contextcheck // buildRestorer→resolveTargetKubeconfig derives ctx from cmd (cluster-cmd convention)
	restorer, err := buildRestorer(cmd, flags)
	if err != nil {
		return err
	}

	writer := cmd.OutOrStdout()

	printRestoreHeader(writer, flags)

	archive, err := restorer.Extract()
	if err != nil {
		return err //nolint:wrapcheck // engine already wraps with "failed to extract backup"
	}

	defer archive.Cleanup()

	printRestoreMetadata(writer, archive.Metadata)

	_, _ = fmt.Fprintf(writer, "Restoring cluster resources...\n")

	err = restorer.RestoreExtracted(ctx, archive, writer)
	if err != nil {
		return err //nolint:wrapcheck // engine already wraps with "failed to restore resources"
	}

	printRestoreFooter(writer, flags)

	return nil
}

// buildRestorer validates the restore flags, canonicalizes the input path,
// resolves the kubeconfig, and constructs the restore engine.
func buildRestorer(
	cmd *cobra.Command,
	flags *restoreFlags,
) (*backupsvc.Restorer, error) {
	if flags.existingResourcePolicy != resourcePolicyNone &&
		flags.existingResourcePolicy != resourcePolicyUpdate {
		return nil, ErrInvalidResourcePolicy
	}

	// Canonicalize user-supplied input path (resolve symlinks + absolute)
	// so that the actual file being read is predictable and symlink-escape
	// attacks are prevented in CI pipelines.
	canonInput, err := fsutil.EvalCanonicalPath(flags.inputPath)
	if err != nil {
		return nil, fmt.Errorf("resolve input path %q: %w", flags.inputPath, err)
	}

	flags.inputPath = canonInput

	kubeconfigPath, err := resolveTargetKubeconfig(cmd, flags.name)
	if err != nil {
		return nil, err
	}

	return backupsvc.NewRestorer(backupsvc.RestoreOptions{
		KubeconfigPath:         kubeconfigPath,
		InputPath:              flags.inputPath,
		ExistingResourcePolicy: flags.existingResourcePolicy,
		DryRun:                 flags.dryRun,
	}), nil
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

// printRestoreFooter writes the terminal success/dry-run line.
func printRestoreFooter(writer io.Writer, flags *restoreFlags) {
	if flags.dryRun {
		_, _ = fmt.Fprintf(
			writer,
			"Dry-run completed successfully (no changes applied)\n",
		)
	} else {
		_, _ = fmt.Fprintf(writer, "Restore completed successfully\n")
	}
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
