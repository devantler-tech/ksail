package workload

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v5/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v5/pkg/envvar"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/spf13/cobra"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/yaml"
)

const (
	kustomizationFileName = "kustomization.yaml"
	validationConcurrency = 5
)

// ErrBuildFailed is returned when a kustomize build or manifest validation fails.
var ErrBuildFailed = errors.New("build failed")

type fluxSubstituteSourceRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type fluxKustomizationManifest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		PostBuild struct {
			SubstituteFrom []fluxSubstituteSourceRef `json:"substituteFrom"`
		} `json:"postBuild"`
	} `json:"spec"`
}

type variableResourceManifest struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Data       map[string]string `json:"data"`
	StringData map[string]string `json:"stringData"`
}

type validationSubstitutions map[string]string

type substituteSourceSet map[string]struct{}

func (s substituteSourceSet) add(kind, name, namespace string) {
	if kind == "" || name == "" {
		return
	}

	s[strings.ToLower(kind)+"/"+namespace+"/"+name] = struct{}{}
}

func (s substituteSourceSet) contains(kind, name, namespace string) bool {
	_, ok := s[strings.ToLower(kind)+"/"+namespace+"/"+name]

	return ok
}

// NewValidateCmd creates the workload validate command.
func NewValidateCmd() *cobra.Command {
	var (
		skipSecrets          bool
		strict               bool
		ignoreMissingSchemas bool
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

	return cmd
}

func runValidateCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	skipSecrets bool,
	strict bool,
	ignoreMissingSchemas bool,
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
	}

	if skipSecrets {
		validationOpts.SkipKinds = append(validationOpts.SkipKinds, "Secret")
	}

	return validatePath(ctx, cmd, path, kubeconformClient, validationOpts)
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
		rootDir := filepath.Dir(path)

		substitutions, loadErr := loadValidationSubstitutions(rootDir)
		if loadErr != nil {
			return loadErr
		}

		return validateFile(ctx, cmd, rootDir, path, kubeconformClient, opts, substitutions)
	}

	// If it's a directory, walk it to find YAML files and kustomizations
	return validateDirectory(ctx, cmd, path, kubeconformClient, opts)
}

// validateFile validates a single YAML file.
func validateFile(
	ctx context.Context,
	cmd *cobra.Command,
	rootDir string,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
	substitutions validationSubstitutions,
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

	err := validateFileSilent(ctx, rootDir, filePath, kubeconformClient, opts, substitutions)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// validateDirectory validates all YAML files and kustomizations in a directory.
// Validation is performed in parallel with live progress display for better UX.
func validateDirectory( //nolint:funlen // orchestrates two parallel validation pipelines with shared setup
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

	// Exclude patch files — already validated as part of kustomize build output.
	patchPaths := collectPatchPaths(dirPath, kustomizations)
	yamlFiles = filterPatchFiles(yamlFiles, patchPaths)

	substitutions, err := loadValidationSubstitutions(dirPath)
	if err != nil {
		return err
	}

	progressOpts := []notify.ProgressOption{
		notify.WithAppendOnly(),
		notify.WithConcurrency(validationConcurrency),
		notify.WithContinueOnError(),
	}

	if len(kustomizations) > 0 {
		kustomizeClient := kustomize.NewClient()

		err := runParallelValidation(
			ctx, cmd, kustomizations, dirPath, "Validating kustomizations", "✅",
			buildKustomizationValidator(
				dirPath,
				kubeconformClient,
				kustomizeClient,
				opts,
				substitutions,
			),
			append(progressOpts, notify.WithCountLabel("kustomizations"))...,
		)
		if err != nil {
			return fmt.Errorf("kustomization validation failed: %w", err)
		}
	}

	// Validate individual YAML files in parallel with progress display
	if len(yamlFiles) > 0 {
		err := runParallelValidation(
			ctx, cmd, yamlFiles, dirPath, "Validating YAML files", "📄",
			func(taskCtx context.Context, file string) error {
				return validateFileSilent(
					taskCtx, dirPath, file, kubeconformClient, opts, substitutions,
				)
			},
			append(progressOpts, notify.WithCountLabel("files"))...,
		)
		if err != nil {
			return fmt.Errorf("yaml validation failed: %w", err)
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
	slices.Sort(items)

	tasks := make([]notify.ProgressTask, len(items))
	for taskIdx, item := range items {
		name := filepath.Base(item)

		rel, relErr := filepath.Rel(basePath, item)
		if relErr == nil && rel != "." {
			name = rel
		}

		tasks[taskIdx] = notify.ProgressTask{
			Name: name,
			Fn: func(taskCtx context.Context) error {
				return validateFn(taskCtx, item)
			},
		}
	}

	opts := append(
		[]notify.ProgressOption{notify.WithLabels(notify.ValidatingLabels())},
		extraOpts...)

	err := notify.NewProgressGroup(title, emoji, cmd.OutOrStdout(), opts...).Run(ctx, tasks...)
	if err != nil {
		return fmt.Errorf("run validation group: %w", err)
	}

	return nil
}

// validateKustomizationSilent validates a kustomization without output (for parallel execution).
// Build errors are returned unwrapped so that simplifyBuildError in the caller can strip the
// kustomize client's verbose "kustomize build <path>:" prefix correctly.
func validateKustomizationSilent(
	ctx context.Context,
	kustDir string,
	kubeconformClient *kubeconform.Client,
	kustomizeClient *kustomize.Client,
	opts *kubeconform.ValidationOptions,
	substitutions validationSubstitutions,
) error {
	// Build the kustomization — return the raw error so simplifyBuildError can strip its prefix.
	output, err := kustomizeClient.Build(ctx, kustDir)
	if err != nil {
		return err //nolint:wrapcheck // intentionally unwrapped: simplifyBuildError in the caller strips the kustomize prefix
	}

	// Validate the output
	err = kubeconformClient.ValidateBytes(
		ctx,
		kustDir,
		applyValidationSubstitutions(output.Bytes(), substitutions),
		opts,
	)
	if err != nil {
		return fmt.Errorf("validate manifests: %w", err)
	}

	return nil
}

// buildKustomizationValidator returns a task function that validates a kustomization directory.
// Errors are simplified for readability by stripping verbose kustomize output.
func buildKustomizationValidator(
	dirPath string,
	kubeconformClient *kubeconform.Client,
	kustomizeClient *kustomize.Client,
	opts *kubeconform.ValidationOptions,
	substitutions validationSubstitutions,
) func(context.Context, string) error {
	return func(taskCtx context.Context, kustDir string) error {
		err := validateKustomizationSilent(
			taskCtx,
			kustDir,
			kubeconformClient,
			kustomizeClient,
			opts,
			substitutions,
		)
		if err != nil {
			return simplifyBuildError(err, dirPath)
		}

		return nil
	}
}

// validateFileSilent validates a single YAML file without output (for parallel execution).
func validateFileSilent(
	ctx context.Context,
	rootDir string,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
	substitutions validationSubstitutions,
) error {
	// Only validate YAML files
	if !isYAMLFile(filePath) {
		return nil
	}

	data, err := fsutil.ReadFileSafe(rootDir, filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	err = kubeconformClient.ValidateBytes(
		ctx,
		filePath,
		applyValidationSubstitutions(data, substitutions),
		opts,
	)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// simplifyBuildError extracts an actionable error message from a kustomize build error.
// It strips the internal "kustomize build <path>:" wrapper, replaces absolute paths
// with paths relative to basePath, and for deeply nested accumulation chains extracts
// the root cause (e.g. "invalid Kustomization: ...").
func simplifyBuildError(err error, basePath string) error {
	msg := err.Error()

	// Remove "kustomize build <path>: " prefix added by the kustomize client.
	if strings.HasPrefix(msg, "kustomize build ") {
		if i := strings.Index(msg, ": "); i > 0 {
			msg = msg[i+2:]
		}
	}

	// For deeply nested kustomize accumulation errors, extract the root cause.
	if strings.Contains(msg, "accumulating resources") {
		for _, pattern := range []string{
			"invalid Kustomization: ",
			"missing metadata",
		} {
			if idx := strings.LastIndex(msg, pattern); idx >= 0 {
				msg = msg[idx:]

				break
			}
		}
	}

	// Strip absolute paths: replace basePath prefix with relative notation.
	if basePath != "" {
		msg = strings.ReplaceAll(msg, basePath+string(filepath.Separator), "")
		msg = strings.ReplaceAll(msg, basePath, ".")
	}

	return fmt.Errorf("%w: %s", ErrBuildFailed, msg)
}

// walkFiles collects file paths under rootPath that satisfy match.
// match receives the full path and os.FileInfo for each non-directory entry
// and returns the value to collect (empty string means skip).
func walkFiles(rootPath string, match func(string, os.FileInfo) string) ([]string, error) {
	var results []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			if v := match(path, info); v != "" {
				results = append(results, v)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", rootPath, err)
	}

	return results, nil
}

// findKustomizations finds all directories containing kustomization.yaml files.
func findKustomizations(rootPath string) ([]string, error) {
	return walkFiles(rootPath, func(path string, info os.FileInfo) string {
		if info.Name() == kustomizationFileName {
			return filepath.Dir(path)
		}

		return ""
	})
}

// findYAMLFiles finds all YAML files in a directory, excluding kustomization.yaml files.
func findYAMLFiles(rootPath string) ([]string, error) {
	return walkFiles(rootPath, func(path string, _ os.FileInfo) string {
		if isYAMLFile(path) && filepath.Base(path) != kustomizationFileName {
			return path
		}

		return ""
	})
}

// isYAMLFile checks if a file has a YAML extension.
func isYAMLFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	return ext == ".yaml" || ext == ".yml"
}

// filterPatchFiles removes from yamlFiles any path present in patchPaths.
// Patch files are not valid standalone resources; they are validated as part of
// the kustomize build output.
func filterPatchFiles(yamlFiles []string, patchPaths map[string]struct{}) []string {
	if len(patchPaths) == 0 {
		return yamlFiles
	}

	filtered := yamlFiles[:0]
	for _, f := range yamlFiles {
		if _, ok := patchPaths[f]; !ok {
			filtered = append(filtered, f)
		}
	}

	return filtered
}

// collectPatchPaths parses each kustomization.yaml and returns the absolute paths
// of files referenced as patches. These files are not valid standalone K8s resources
// and should be excluded from individual file validation (they are already validated
// as part of the kustomize build output).
func collectPatchPaths(rootDir string, kustomizationDirs []string) map[string]struct{} {
	patchPaths := make(map[string]struct{})

	for _, kustDir := range kustomizationDirs {
		collectPatchPathsFromDir(rootDir, kustDir, patchPaths)
	}

	return patchPaths
}

// collectPatchPathsFromDir parses a single kustomization.yaml and adds the absolute
// paths of referenced patch files to the provided set.
func collectPatchPathsFromDir(rootDir, kustDir string, patchPaths map[string]struct{}) {
	kustFile := filepath.Join(kustDir, kustomizationFileName)

	data, err := fsutil.ReadFileSafe(rootDir, kustFile)
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

func loadValidationSubstitutions(rootPath string) (validationSubstitutions, error) {
	refs, err := collectFluxSubstituteSources(rootPath)
	if err != nil {
		return nil, fmt.Errorf("collect flux substitute sources: %w", err)
	}

	if len(refs) == 0 {
		return validationSubstitutions{}, nil
	}

	files, err := walkFiles(rootPath, func(path string, _ os.FileInfo) string {
		if isYAMLFile(path) {
			return path
		}

		return ""
	})
	if err != nil {
		return nil, fmt.Errorf("find substitution manifests: %w", err)
	}

	slices.Sort(files)

	values := make(validationSubstitutions)
	for _, filePath := range files {
		err = collectSubstitutionValuesFromFile(rootPath, filePath, refs, values)
		if err != nil {
			return nil, err
		}
	}

	return values, nil
}

func collectFluxSubstituteSources(rootPath string) (substituteSourceSet, error) {
	files, err := walkFiles(rootPath, func(path string, _ os.FileInfo) string {
		if isYAMLFile(path) {
			return path
		}

		return ""
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)

	refs := make(substituteSourceSet)
	for _, filePath := range files {
		err = collectFluxSubstituteSourcesFromFile(rootPath, filePath, refs)
		if err != nil {
			return nil, err
		}
	}

	return refs, nil
}

func collectFluxSubstituteSourcesFromFile(
	rootPath, filePath string,
	refs substituteSourceSet,
) error {
	docs, err := readYAMLDocuments(rootPath, filePath)
	if err != nil {
		return err
	}

	for _, doc := range docs {
		var manifest fluxKustomizationManifest

		err = yaml.Unmarshal(doc, &manifest)
		if err != nil {
			continue
		}

		if manifest.Kind != "Kustomization" ||
			!strings.HasPrefix(manifest.APIVersion, "kustomize.toolkit.fluxcd.io/") {
			continue
		}

		kustomizationNS := manifest.Metadata.Namespace

		for _, ref := range manifest.Spec.PostBuild.SubstituteFrom {
			ns := ref.Namespace
			if ns == "" {
				ns = kustomizationNS
			}

			refs.add(ref.Kind, ref.Name, ns)
		}
	}

	return nil
}

func collectSubstitutionValuesFromFile(
	rootPath, filePath string,
	refs substituteSourceSet,
	values validationSubstitutions,
) error {
	docs, err := readYAMLDocuments(rootPath, filePath)
	if err != nil {
		return err
	}

	for _, doc := range docs {
		var manifest variableResourceManifest

		err = yaml.Unmarshal(doc, &manifest)
		if err != nil {
			continue
		}

		if !refs.contains(manifest.Kind, manifest.Metadata.Name, manifest.Metadata.Namespace) {
			continue
		}

		for key, value := range manifest.StringData {
			if _, exists := values[key]; !exists {
				values[key] = value
			}
		}

		for key, value := range manifest.Data {
			if _, exists := values[key]; exists {
				continue
			}

			if manifest.Kind == "Secret" {
				values[key] = decodeSecretValue(value)

				continue
			}

			values[key] = value
		}
	}

	return nil
}

func readYAMLDocuments(rootPath, filePath string) ([][]byte, error) {
	data, err := fsutil.ReadFileSafe(rootPath, filePath)
	if err != nil {
		return nil, fmt.Errorf("read yaml file %s: %w", filePath, err)
	}

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	var docs [][]byte

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read yaml document %s: %w", filePath, err)
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		docs = append(docs, doc)
	}

	return docs, nil
}

func decodeSecretValue(value string) string {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return value
	}

	return string(decoded)
}

func applyValidationSubstitutions(data []byte, substitutions validationSubstitutions) []byte {
	if len(substitutions) == 0 {
		return envvar.ExpandBytes(data)
	}

	return envvar.ExpandBytesWithLookup(data, func(name string) (string, bool) {
		// Prefer explicit validation substitutions when provided.
		if value, ok := substitutions[name]; ok {
			return value, true
		}

		// Fall back to process environment.
		if value, ok := os.LookupEnv(name); ok {
			return value, true
		}

		// Unknown variables are reported as unset so that shell-style default
		// syntax (${VAR:-default}, ${VAR:=default}) continues to work even
		// when validation substitutions are provided.
		return "", false
	})
}
