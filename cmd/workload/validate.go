package workload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/pkg/client/kustomize"
	"github.com/spf13/cobra"
)

const (
	kustomizationFileName = "kustomization.yaml"
	maxLineLength         = 120
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
		Use:   "validate [PATH]...",
		Short: "Validate Kubernetes manifests and kustomizations",
		Long: `Validate Kubernetes manifest files and kustomizations using kubeconform.

This command validates individual YAML files and kustomizations in the specified paths.
If no path is provided, it validates the current directory.

The validation process:
1. Validates individual YAML files
2. Validates kustomizations by building them with kustomize and validating the output

By default, Kubernetes Secrets are skipped to avoid validation failures due to SOPS fields.`,
		Args: cobra.ArbitraryArgs,
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
	cmd.Flags().BoolVar(&ignoreMissingSchemas, "ignore-missing-schemas", true, "Ignore resources with missing schemas")
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
	// Default to current directory if no paths provided
	if len(args) == 0 {
		args = []string{"."}
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

	// Validate each path
	for _, path := range args {
		err := validatePath(ctx, cmd, path, kubeconformClient, validationOpts)
		if err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "✅ All validations passed")

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

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "INFO - Validating %s\n", filePath)

	err := kubeconformClient.ValidateFile(ctx, filePath, opts)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// validateDirectory validates all YAML files and kustomizations in a directory.
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

	// Validate kustomizations
	if len(kustomizations) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "INFO - Validating kustomize overlays")

		kustomizeClient := kustomize.NewClient()

		for _, kustDir := range kustomizations {
			err := validateKustomization(ctx, cmd, kustDir, kubeconformClient, kustomizeClient, opts)
			if err != nil {
				return err
			}
		}
	}

	// Find and validate individual YAML files
	yamlFiles, err := findYAMLFiles(dirPath)
	if err != nil {
		return fmt.Errorf("find YAML files: %w", err)
	}

	for _, file := range yamlFiles {
		err := validateFile(ctx, cmd, file, kubeconformClient, opts)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateKustomization validates a kustomization by building it and validating the output.
func validateKustomization(
	ctx context.Context,
	cmd *cobra.Command,
	kustDir string,
	kubeconformClient *kubeconform.Client,
	kustomizeClient *kustomize.Client,
	opts *kubeconform.ValidationOptions,
) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "INFO - Validating kustomization %s\n", kustDir)

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

// findYAMLFiles finds all YAML files in a directory.
func findYAMLFiles(rootPath string) ([]string, error) {
	var yamlFiles []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && isYAMLFile(path) {
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
