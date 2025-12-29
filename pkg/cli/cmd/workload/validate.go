package workload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v5/pkg/client/kustomize"
	"github.com/spf13/cobra"
)

const (
	kustomizationFileName = "kustomization.yaml"
)

// NewValidateCmd creates the workload validate command.
func NewValidateCmd() *cobra.Command {
	var (
		skipSecrets          bool
		strict               bool
		ignoreMissingSchemas bool
		verbose              bool
	)

	cmd := &cobra.Command{
		Use:   "validate [PATH]",
		Short: "Validate Kubernetes manifests and kustomizations",
		Long: `Validate Kubernetes manifest files and kustomizations using kubeconform.

This command validates individual YAML files and kustomizations in the specified path.
If no path is provided, it validates the current directory.

The validation process:
1. Validates individual YAML files
2. Validates kustomizations by building them with kustomize and validating the output

By default, Kubernetes Secrets are skipped to avoid validation failures due to SOPS fields.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidateCmd(
				cmd.Context(),
				cmd,
				args,
				skipSecrets,
				strict,
				ignoreMissingSchemas,
				verbose,
			)
		},
	}

	// Add flags
	cmd.Flags().BoolVar(&skipSecrets, "skip-secrets", true, "Skip validation of Kubernetes Secrets")
	cmd.Flags().BoolVar(&strict, "strict", true, "Enable strict validation mode")
	cmd.Flags().BoolVar(
		&ignoreMissingSchemas,
		"ignore-missing-schemas",
		true,
		"Ignore resources with missing schemas",
	)
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Enable verbose output")

	return cmd
}

func runValidateCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	skipSecrets bool,
	strict bool,
	ignoreMissingSchemas bool,
	verbose bool,
) error {
	// Default to current directory if no path provided
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	// Create kubeconform client
	kubeconformClient := kubeconform.NewClient()

	// Build validation options
	validationOpts := &kubeconform.ValidationOptions{
		Strict:               strict,
		IgnoreMissingSchemas: ignoreMissingSchemas,
		Verbose:              verbose,
	}
	if skipSecrets {
		validationOpts.SkipKinds = append(validationOpts.SkipKinds, "Secret")
	}

	// Validate the path
	err := validatePath(ctx, cmd, path, kubeconformClient, validationOpts)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "all validations passed",
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// validatePath validates all manifests in the given path.
func validatePath(
	ctx context.Context,
	cmd *cobra.Command,
	path string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("access path %s: %w", path, err)
	}

	// If it's a file, validate it directly
	if !info.IsDir() {
		return validateFile(ctx, cmd, path, kubeconformClient, opts)
	}

	// If it's a directory, walk it to find YAML files and kustomizations
	return validateDirectory(ctx, cmd, path, kubeconformClient, opts)
}

// validateFile validates a single YAML file.
func validateFile(
	ctx context.Context,
	cmd *cobra.Command,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Only validate YAML files
	if !isYAMLFile(filePath) {
		return nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "validating %s",
		Args:    []any{filePath},
		Writer:  cmd.OutOrStdout(),
	})

	err := kubeconformClient.ValidateFile(ctx, filePath, opts)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// validateDirectory validates all YAML files and kustomizations in a directory.
// Validation is performed in parallel with live progress display for better UX.
//
//nolint:funlen // Orchestrates parallel validation with progress display
func validateDirectory(
	ctx context.Context,
	cmd *cobra.Command,
	dirPath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Find all kustomizations
	kustomizations, err := findKustomizations(dirPath)
	if err != nil {
		return fmt.Errorf("find kustomizations: %w", err)
	}

	// Find all YAML files
	yamlFiles, err := findYAMLFiles(dirPath)
	if err != nil {
		return fmt.Errorf("find YAML files: %w", err)
	}

	// Validate kustomizations in parallel with progress display
	if len(kustomizations) > 0 {
		kustomizeClient := kustomize.NewClient()

		tasks := make([]notify.ProgressTask, len(kustomizations))
		for i, kustDir := range kustomizations {
			tasks[i] = notify.ProgressTask{
				Name: filepath.Base(kustDir),
				Fn: func(taskCtx context.Context) error {
					return validateKustomizationSilent(
						taskCtx,
						kustDir,
						kubeconformClient,
						kustomizeClient,
						opts,
					)
				},
			}
		}

		progressGroup := notify.NewProgressGroup(
			"Validating kustomizations",
			"✅",
			cmd.OutOrStdout(),
			notify.WithLabels(notify.ValidatingLabels()),
		)

		pgErr := progressGroup.Run(ctx, tasks...)
		if pgErr != nil {
			return fmt.Errorf("kustomization validation failed: %w", pgErr)
		}
	}

	// Validate individual YAML files in parallel with progress display
	if len(yamlFiles) > 0 {
		tasks := make([]notify.ProgressTask, len(yamlFiles))
		for i, file := range yamlFiles {
			tasks[i] = notify.ProgressTask{
				Name: filepath.Base(file),
				Fn: func(taskCtx context.Context) error {
					return validateFileSilent(taskCtx, file, kubeconformClient, opts)
				},
			}
		}

		progressGroup := notify.NewProgressGroup(
			"Validating YAML files",
			"📄",
			cmd.OutOrStdout(),
			notify.WithLabels(notify.ValidatingLabels()),
		)

		pgErr := progressGroup.Run(ctx, tasks...)
		if pgErr != nil {
			return fmt.Errorf("YAML validation failed: %w", pgErr)
		}
	}

	return nil
}

// validateKustomizationSilent validates a kustomization without output (for parallel execution).
func validateKustomizationSilent(
	ctx context.Context,
	kustDir string,
	kubeconformClient *kubeconform.Client,
	kustomizeClient *kustomize.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Build the kustomization
	output, err := kustomizeClient.Build(ctx, kustDir)
	if err != nil {
		return fmt.Errorf("build kustomization %s: %w", kustDir, err)
	}

	// Validate the output
	err = kubeconformClient.ValidateManifests(ctx, output, opts)
	if err != nil {
		return fmt.Errorf("validate kustomization %s: %w", kustDir, err)
	}

	return nil
}

// validateFileSilent validates a single YAML file without output (for parallel execution).
func validateFileSilent(
	ctx context.Context,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Only validate YAML files
	if !isYAMLFile(filePath) {
		return nil
	}

	err := kubeconformClient.ValidateFile(ctx, filePath, opts)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// findKustomizations finds all directories containing kustomization.yaml files.
func findKustomizations(rootPath string) ([]string, error) {
	var kustomizations []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Name() == kustomizationFileName {
			kustomizations = append(kustomizations, filepath.Dir(path))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", rootPath, err)
	}

	return kustomizations, nil
}

// findYAMLFiles finds all YAML files in a directory, excluding kustomization.yaml files.
func findYAMLFiles(rootPath string) ([]string, error) {
	var yamlFiles []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip kustomization.yaml files as they are validated separately via kustomize build
		if !info.IsDir() && isYAMLFile(path) && filepath.Base(path) != kustomizationFileName {
			yamlFiles = append(yamlFiles, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", rootPath, err)
	}

	return yamlFiles, nil
}

// isYAMLFile checks if a file has a YAML extension.
func isYAMLFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	return ext == ".yaml" || ext == ".yml"
}
