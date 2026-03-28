package workload

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v5/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/spf13/cobra"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
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
If no path is provided, the path is resolved in order:
  1. spec.workload.sourceDirectory from ksail.yaml (if a config file is found and the field is set)
  2. The default source directory when spec.workload.sourceDirectory is unset ("k8s" directory)
  3. The current directory (fallback when no ksail.yaml config file is found)

The validation process:
1. Validates individual YAML files (patch files referenced in kustomization.yaml via patches,
   patchesStrategicMerge, or patchesJson6902 are excluded — they are not valid standalone
   Kubernetes resources and are validated as part of the kustomize build output instead)
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
	cmd.Flags().BoolVar(&strict, "strict", false, "Enable strict validation mode")
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
	path, err := resolveValidatePath(cmd, args)
	if err != nil {
		return err
	}

	// Canonicalize user-supplied path (resolve symlinks + absolute) so that
	// validation targets the real directory and symlink-escape attacks are
	// prevented in CI pipelines processing external manifests.
	canonPath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}

	path = canonPath

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
	err = validatePath(ctx, cmd, path, kubeconformClient, validationOpts)
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

// resolveValidatePath determines which path to validate.
// If an explicit argument is given, it is returned directly.
// Otherwise, it loads ksail.yaml (honoring --config) and returns the
// configured sourceDirectory. Falls back to "." when no config file is found.
func resolveValidatePath(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	// Resolve --config flag without registering additional flags on cmd.
	// This avoids "flag redefined" panics that NewCommandConfigManager would cause.
	var configFile string

	cfgPath, err := flags.GetConfigPath(cmd)
	if err == nil {
		configFile = cfgPath
	}

	cfgManager := ksailconfigmanager.NewConfigManager(io.Discard, configFile)

	cfg, loadErr := cfgManager.Load(
		configmanager.LoadOptions{Silent: true, SkipValidation: true},
	)

	switch {
	case loadErr != nil && cfgManager.IsConfigFileFound():
		return "", fmt.Errorf("load config: %w", loadErr)
	case loadErr == nil && cfgManager.IsConfigFileFound():
		return resolveSourceDir(cfg, ""), nil
	default:
		return ".", nil
	}
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

	// Exclude files referenced as kustomize patches — they are validated
	// as part of the kustomize build output and are not valid standalone resources.
	patchPaths := collectPatchPaths(kustomizations)
	if len(patchPaths) > 0 {
		filtered := yamlFiles[:0]
		for _, f := range yamlFiles {
			if _, ok := patchPaths[f]; !ok {
				filtered = append(filtered, f)
			}
		}

		yamlFiles = filtered
	}

	// Validate kustomizations in parallel with progress display
	if len(kustomizations) > 0 {
		kustomizeClient := kustomize.NewClient()

		kustErr := runParallelValidation(
			ctx, cmd, kustomizations, dirPath, "Validating kustomizations", "✅",
			func(taskCtx context.Context, kustDir string) error {
				return validateKustomizationSilent(
					taskCtx, kustDir, kubeconformClient, kustomizeClient, opts,
				)
			},
		)
		if kustErr != nil {
			return fmt.Errorf("kustomization validation failed: %w", kustErr)
		}
	}

	// Validate individual YAML files in parallel with progress display
	if len(yamlFiles) > 0 {
		filesErr := runParallelValidation(ctx, cmd, yamlFiles, dirPath, "Validating YAML files", "📄",
			func(taskCtx context.Context, file string) error {
				return validateFileSilent(taskCtx, file, kubeconformClient, opts)
			},
			notify.WithMaxVisible(5),
			notify.WithConcurrency(5),
			notify.WithContinueOnError(),
		)
		if filesErr != nil {
			return fmt.Errorf("yaml validation failed: %w", filesErr)
		}
	}

	return nil
}

// runParallelValidation runs a set of validation tasks in parallel with progress display.
func runParallelValidation(
	ctx context.Context,
	cmd *cobra.Command,
	items []string,
	basePath string,
	title string,
	emoji string,
	validateFn func(ctx context.Context, item string) error,
	extraOpts ...notify.ProgressOption,
) error {
	tasks := make([]notify.ProgressTask, len(items))
	for taskIdx, item := range items {
		name := filepath.Base(item)
		if rel, relErr := filepath.Rel(basePath, item); relErr == nil {
			name = rel
		}

		tasks[taskIdx] = notify.ProgressTask{
			Name: name,
			Fn: func(taskCtx context.Context) error {
				return validateFn(taskCtx, item)
			},
		}
	}

	opts := []notify.ProgressOption{notify.WithLabels(notify.ValidatingLabels())}
	opts = append(opts, extraOpts...)

	progressGroup := notify.NewProgressGroup(
		title,
		emoji,
		cmd.OutOrStdout(),
		opts...,
	)

	pgErr := progressGroup.Run(ctx, tasks...)
	if pgErr != nil {
		return fmt.Errorf("parallel validation: %w", pgErr)
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

// collectPatchPaths parses each kustomization.yaml and returns the absolute paths
// of files referenced as patches. These files are not valid standalone K8s resources
// and should be excluded from individual file validation (they are already validated
// as part of the kustomize build output).
func collectPatchPaths(kustomizationDirs []string) map[string]struct{} {
	patchPaths := make(map[string]struct{})

	for _, kustDir := range kustomizationDirs {
		collectPatchPathsFromDir(kustDir, patchPaths)
	}

	return patchPaths
}

// collectPatchPathsFromDir parses a single kustomization.yaml and adds the absolute
// paths of referenced patch files to the provided set.
func collectPatchPathsFromDir(kustDir string, patchPaths map[string]struct{}) {
	kustFile := filepath.Join(kustDir, kustomizationFileName)

	data, err := os.ReadFile(kustFile) //nolint:gosec // kustFile built from walked dirs
	if err != nil {
		return
	}

	var kust kustomizeTypes.Kustomization

	err = kust.Unmarshal(data)
	if err != nil {
		return
	}

	// Modern patches field
	for _, p := range kust.Patches {
		addPatchPath(kustDir, p.Path, patchPaths)
	}

	// Deprecated patchesStrategicMerge (file paths only, skip inline YAML)
	for _, psm := range kust.PatchesStrategicMerge { //nolint:staticcheck // must handle legacy kustomization files
		s := string(psm)
		if !strings.Contains(s, "\n") {
			addPatchPath(kustDir, s, patchPaths)
		}
	}

	// Deprecated patchesJson6902
	for _, p := range kust.PatchesJson6902 { //nolint:staticcheck // must handle legacy kustomization files
		addPatchPath(kustDir, p.Path, patchPaths)
	}
}

// addPatchPath resolves a relative patch file path against a kustomization directory
// and adds the absolute path to the set. Empty paths are ignored.
func addPatchPath(kustDir, relPath string, patchPaths map[string]struct{}) {
	if relPath == "" {
		return
	}

	abs := filepath.Join(kustDir, relPath)

	resolved, err := filepath.Abs(abs)
	if err != nil {
		return
	}

	patchPaths[resolved] = struct{}{}
}
