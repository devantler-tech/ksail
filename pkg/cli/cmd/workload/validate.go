package workload

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/spf13/cobra"
	yamlio "k8s.io/apimachinery/pkg/util/yaml"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/yaml"
)

const (
	kustomizationFileName = "kustomization.yaml"
	validationConcurrency = 5
	dirPerm               = 0o750
)

// kustomizationFileNames lists all kustomization filenames recognized by kubectl.
// Used by hasKustomizationFile, findKustomizationDir, findKustomizations,
// findYAMLFiles, and collectPatchPathsFromDir for consistent detection.
//
//nolint:gochecknoglobals // package-level constant slice; Go does not support const slices
var kustomizationFileNames = []string{kustomizationFileName, "kustomization.yml", "Kustomization"}

// ErrBuildFailed is returned when a kustomize build or manifest validation fails.
var ErrBuildFailed = errors.New("build failed")

// NewValidateCmd creates the workload validate command.
func NewValidateCmd() *cobra.Command {
	var (
		skipSecrets          bool
		strict               bool
		ignoreMissingSchemas bool
		skipKinds            []string
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
1. Validates individual YAML files (patch files referenced in a kustomization file via patches,
   patchesStrategicMerge, or patchesJson6902 are excluded — they are not valid standalone
   Kubernetes resources and are validated as part of the kustomize build output instead)
2. Validates kustomizations by building them with kustomize and validating the output

	Flux variable substitutions are resolved before validation using type-aware placeholders:
  - ${VAR} (bare, no default): when a JSON schema type is available, substitutes a typed
    placeholder derived from the schema for the field ("placeholder" for strings, 0 for
    integers, true for booleans); when no schema type is available, it falls back to the
    string value "placeholder"
  - ${VAR:-default} / ${VAR:=default}: when a schema type is available, uses the default
    value parsed according to the field schema type (e.g., "3" → int 3 for integer fields);
    when no schema type is available, the default is parsed using YAML-native type inference
  - Mixed text (e.g., "prefix.${VAR}"): substitutes "placeholder" in string context

Schema lookups use a local disk cache and require no network access. When no cached
JSON schema is available, placeholders fall back to strings with YAML-native parsing.

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
				skipKinds,
			)
		},
	}

	addValidateFlags(cmd, &skipSecrets, &strict, &ignoreMissingSchemas, &skipKinds)

	return cmd
}

// addValidateFlags registers the flags for the validate command.
func addValidateFlags(
	cmd *cobra.Command,
	skipSecrets, strict, ignoreMissingSchemas *bool,
	skipKinds *[]string,
) {
	cmd.Flags().BoolVar(skipSecrets, "skip-secrets", true, "Skip validation of Kubernetes Secrets")
	cmd.Flags().BoolVar(strict, "strict", false, "Enable strict validation mode")
	cmd.Flags().BoolVar(
		ignoreMissingSchemas,
		"ignore-missing-schemas",
		true,
		"Ignore resources with missing schemas",
	)
	cmd.Flags().StringSliceVar(
		skipKinds,
		"skip-kinds",
		nil,
		"Additional Kubernetes kinds to skip during validation "+
			"(merged with spec.workload.validation.skipKinds from ksail.yaml)",
	)
}

func runValidateCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	skipSecrets bool,
	strict bool,
	ignoreMissingSchemas bool,
	skipKinds []string,
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

	// Additional kinds to skip come from the --skip-kinds flag and from
	// spec.workload.validation.skipKinds in ksail.yaml. This lets a repo opt out
	// of validating CRDs whose CRDs-catalog schema is stale or missing (which
	// kubeconform would otherwise reject as "additional properties not allowed").
	validationOpts.SkipKinds = append(validationOpts.SkipKinds, skipKinds...)
	validationOpts.SkipKinds = append(validationOpts.SkipKinds, configuredSkipKinds(cmd)...)

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

	cfgManager := configmanager.NewConfigManager(io.Discard, configFile)

	cfg, loadErr := cfgManager.Load(
		configmanagerinterface.LoadOptions{Silent: true, SkipValidation: true},
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

// configuredSkipKinds loads ksail.yaml (honoring --config) and returns the
// configured spec.workload.validation.skipKinds. It returns nil when no config
// file is found or the field is unset, and never fails validation on a config
// load error — the built-in skips still apply.
func configuredSkipKinds(cmd *cobra.Command) []string {
	var configFile string

	cfgPath, pathErr := flags.GetConfigPath(cmd)
	if pathErr == nil {
		configFile = cfgPath
	}

	cfgManager := configmanager.NewConfigManager(io.Discard, configFile)

	cfg, err := cfgManager.Load(
		// SkipDistributionConfig avoids loading distribution-specific config
		// (e.g. Talos PKI generation) on every validate run; only the workload
		// config is read here.
		configmanagerinterface.LoadOptions{
			Silent:                 true,
			SkipValidation:         true,
			SkipDistributionConfig: true,
		},
	)
	if err != nil || !cfgManager.IsConfigFileFound() {
		return nil
	}

	return cfg.Spec.Workload.Validation.SkipKinds
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

		return validateFile(ctx, cmd, rootDir, path, kubeconformClient, opts)
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

	err := validateFileSilent(ctx, rootDir, filePath, kubeconformClient, opts)
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

	// Exclude patch files — already validated as part of kustomize build output.
	patchPaths := collectPatchPaths(dirPath, kustomizations)
	yamlFiles = filterPatchFiles(yamlFiles, patchPaths)

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
					taskCtx, dirPath, file, kubeconformClient, opts,
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
		extraOpts...,
	)

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
		expandFluxSubstitutions(output.Bytes()),
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
) func(context.Context, string) error {
	return func(taskCtx context.Context, kustDir string) error {
		err := validateKustomizationSilent(
			taskCtx,
			kustDir,
			kubeconformClient,
			kustomizeClient,
			opts,
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
		expandFluxSubstitutions(data),
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

// findKustomizations finds all directories containing a kustomization file
// recognized by kubectl (kustomization.yaml, kustomization.yml, or Kustomization).
func findKustomizations(rootPath string) ([]string, error) {
	return walkFiles(rootPath, func(path string, info os.FileInfo) string {
		if slices.Contains(kustomizationFileNames, info.Name()) {
			return filepath.Dir(path)
		}

		return ""
	})
}

// findYAMLFiles finds all YAML files in a directory, excluding kustomization files
// recognized by kubectl (kustomization.yaml, kustomization.yml, or Kustomization).
func findYAMLFiles(rootPath string) ([]string, error) {
	return walkFiles(rootPath, func(path string, _ os.FileInfo) string {
		base := filepath.Base(path)

		if slices.Contains(kustomizationFileNames, base) {
			return ""
		}

		if isYAMLFile(path) {
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

// collectPatchPathsFromDir parses a kustomization file (trying all recognized names)
// and adds the absolute paths of referenced patch files to the provided set.
func collectPatchPathsFromDir(rootDir, kustDir string, patchPaths map[string]struct{}) {
	var data []byte

	var err error

	for _, name := range kustomizationFileNames {
		kustFile := filepath.Join(kustDir, name)

		data, err = fsutil.ReadFileSafe(rootDir, kustFile)
		if err == nil {
			break
		}
	}

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
	//nolint:staticcheck // SA1019: PatchesStrategicMerge is deprecated; kept to support legacy kustomization files
	for _, psm := range kust.PatchesStrategicMerge {
		s := string(psm)
		if !strings.Contains(s, "\n") {
			addPatchPath(kustDir, s, patchPaths)
		}
	}

	// Deprecated patchesJson6902
	//nolint:staticcheck // SA1019: PatchesJson6902 is deprecated; kept to support legacy kustomization files
	for _, p := range kust.PatchesJson6902 {
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

// fluxVarPattern matches Flux postBuild variable references:
// ${VAR}, ${VAR:-default}, and ${VAR:=default}.
// Groups: 1 = variable name, 2 = operator (:- or :=), 3 = default value.
var fluxVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?:(:-|:=)([^}]*))?\}`)

const (
	schemaCacheFileMaxChars = 200

	placeholderString = "placeholder"
)

// schemaRegistry provides thread-safe caching of parsed JSON schemas keyed by "apiVersion/kind".
type schemaRegistry struct {
	cache sync.Map
}

var schemas = &schemaRegistry{} //nolint:gochecknoglobals // singleton schema cache for validation lifecycle

// expandFluxSubstitutions expands Flux postBuild variable references in YAML
// data using type-aware placeholders derived from JSON schemas.
//
// For each YAML document:
//   - ${VAR:-default} / ${VAR:=default} → uses the default value
//   - ${VAR} as entire scalar value → looks up the expected JSON schema type
//     and substitutes a typed placeholder ("placeholder", 0, or true)
//   - ${VAR} mixed with other text → substitutes "placeholder" (string context)
//
// Falls back to regex-based string placeholder expansion when YAML parsing fails.
func expandFluxSubstitutions(data []byte) []byte {
	if !fluxVarPattern.Match(data) {
		return data
	}

	docs := splitYAMLDocuments(data)
	if len(docs) == 0 {
		return data
	}

	expanded := make([][]byte, 0, len(docs))
	for _, doc := range docs {
		expanded = append(expanded, expandDocument(doc))
	}

	return bytes.Join(expanded, []byte("\n---\n"))
}

// splitYAMLDocuments splits multi-document YAML using a YAML-aware reader
// that correctly handles document separators ("---") regardless of position,
// trailing whitespace, or carriage returns.
func splitYAMLDocuments(data []byte) [][]byte {
	reader := yamlio.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	var docs [][]byte

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return [][]byte{data}
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return [][]byte{data}
	}

	return docs
}

// expandDocument expands variable references in a single YAML document.
func expandDocument(doc []byte) []byte {
	if !fluxVarPattern.Match(doc) {
		return doc
	}

	var obj any

	err := yaml.Unmarshal(doc, &obj)
	if err != nil {
		return expandFallback(doc)
	}

	switch typedObj := obj.(type) {
	case map[string]any:
		return expandMapDocument(typedObj, doc)
	case []any:
		return expandListDocument(typedObj, doc)
	default:
		return expandFallback(doc)
	}
}

// expandMapDocument expands variable references in a YAML document with a map root.
func expandMapDocument(obj map[string]any, doc []byte) []byte {
	apiVersion, _ := obj["apiVersion"].(string)
	kind, _ := obj["kind"].(string)
	schema := schemas.load(apiVersion, kind)

	walkAndExpand(obj, "", schema)

	out, err := yaml.Marshal(obj)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandListDocument expands variable references in a YAML document with a list root
// (e.g., JSON6902 patch list). There is no single apiVersion/kind,
// so map elements are walked with a nil schema.
func expandListDocument(list []any, doc []byte) []byte {
	for idx, elem := range list {
		if mapElem, isMap := elem.(map[string]any); isMap {
			walkAndExpand(mapElem, "", nil)
			list[idx] = mapElem
		}
	}

	out, err := yaml.Marshal(list)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandFallback performs simple regex-based expansion when YAML parsing fails.
// Variable references are expanded using defaults with fallback to placeholder values.
func expandFallback(data []byte) []byte {
	return fluxVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := fluxVarPattern.FindSubmatch(match)
		if len(groups) < 4 { //nolint:mnd // regex groups: full, name, op, default
			return match
		}

		return []byte(resolveInlineVar(string(groups[1]), string(groups[2]), string(groups[3])))
	})
}

// walkAndExpand recursively walks the parsed YAML structure and expands variable references.
func walkAndExpand(obj any, path string, schema map[string]any) any {
	switch val := obj.(type) {
	case map[string]any:
		for key, child := range val {
			val[key] = walkAndExpand(child, path+"/"+key, schema)
		}

		return val
	case []any:
		for idx, item := range val {
			val[idx] = walkAndExpand(item, fmt.Sprintf("%s/%d", path, idx), schema)
		}

		return val
	case string:
		return expandStringValue(val, path, schema)
	default:
		return obj
	}
}

// expandStringValue expands Flux variable references in a string value.
func expandStringValue(val, path string, schema map[string]any) any {
	if !fluxVarPattern.MatchString(val) {
		return val
	}

	// Check if the entire value is a single substitution (bare or with default)
	match := fluxVarPattern.FindStringSubmatch(val)
	if match != nil && match[0] == val {
		return expandSingleVar(match, path, schema)
	}

	// Mixed text — expand inline (always string context)
	return expandMixedText(val)
}

// expandSingleVar expands a value that consists entirely of a single variable reference.
func expandSingleVar(match []string, path string, schema map[string]any) any {
	varName := match[1]
	operator := match[2]
	defaultVal := match[3]
	schemaType := getSchemaTypeAtPath(schema, path)

	if operator == "" {
		return expandBareVar(varName, schemaType)
	}

	return expandVarWithDefault(varName, defaultVal, operator, schemaType)
}

// expandBareVar expands a bare ${VAR} reference using typed placeholders.
func expandBareVar(_, schemaType string) any {
	return typedPlaceholderValue(schemaType)
}

// expandVarWithDefault expands ${VAR:=default} or ${VAR:-default} references.
func expandVarWithDefault(_, defaultVal, _, schemaType string) any {
	return parseTypedDefault(defaultVal, schemaType)
}

// expandMixedText expands variable references embedded within other text (always string context).
func expandMixedText(val string) string {
	return fluxVarPattern.ReplaceAllStringFunc(val, func(match string) string {
		groups := fluxVarPattern.FindStringSubmatch(match)
		if len(groups) < 4 { //nolint:mnd // regex groups: full, name, op, default
			return match
		}

		return resolveInlineVar(groups[1], groups[2], groups[3])
	})
}

// resolveInlineVar resolves a single variable reference in a mixed-text context to a string.
func resolveInlineVar(_, operator, defaultVal string) string {
	switch operator {
	case "":
		return placeholderString
	case ":=", ":-":
		return defaultVal
	default:
		return placeholderString
	}
}

// typedPlaceholderValue returns a Go value matching the schema type.
// When marshaled by sigs.k8s.io/yaml, these produce correctly typed YAML scalars.
func typedPlaceholderValue(schemaType string) any {
	switch schemaType {
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return true
	default:
		return placeholderString
	}
}

// parseTypedDefault parses a default value string into the appropriate Go type
// based on the schema type, so that sigs.k8s.io/yaml marshals it without quotes.
// When the schema type is unknown (empty string), YAML-native type inference is
// used, matching Flux's behavior where substitution occurs at the text level.
func parseTypedDefault(defaultVal, schemaType string) any {
	trimmed := strings.TrimSpace(defaultVal)

	switch schemaType {
	case "integer":
		return parseInteger(trimmed, defaultVal)
	case "number":
		return parseNumber(trimmed, defaultVal)
	case "boolean":
		return parseBoolean(trimmed, defaultVal)
	case typeString:
		return defaultVal
	default:
		return inferYAMLType(trimmed, defaultVal)
	}
}

func parseInteger(trimmed, defaultVal string) any {
	var intVal int64

	_, err := fmt.Sscanf(trimmed, "%d", &intVal)
	if err == nil {
		return intVal
	}

	return defaultVal
}

func parseNumber(trimmed, defaultVal string) any {
	var floatVal float64

	_, err := fmt.Sscanf(trimmed, "%f", &floatVal)
	if err == nil {
		return floatVal
	}

	return defaultVal
}

func parseBoolean(trimmed, defaultVal string) any {
	if trimmed == "true" {
		return true
	}

	if trimmed == "false" {
		return false
	}

	return defaultVal
}

// inferYAMLType uses YAML-native type inference so that values like "2" become
// integers and "true" becomes a boolean, matching how YAML would parse the
// substituted text.
func inferYAMLType(trimmed, defaultVal string) any {
	var typedVal any

	err := yaml.Unmarshal([]byte(trimmed), &typedVal)
	if err == nil && typedVal != nil {
		return typedVal
	}

	return defaultVal
}

// load returns the JSON schema for a Kubernetes resource from disk cache, or nil if unavailable.
// Network fetching is intentionally omitted to keep validation fast, deterministic, and
// offline-friendly. Schemas are available if kubeconform has previously cached them on disk.
func (reg *schemaRegistry) load(apiVersion, kind string) map[string]any {
	if apiVersion == "" || kind == "" {
		return nil
	}

	cacheKey := apiVersion + "/" + kind

	if cached, ok := reg.cache.Load(cacheKey); ok {
		schema, _ := cached.(map[string]any)

		return schema
	}

	schema := fetchSchemaFromDisk(apiVersion, kind)

	reg.cache.Store(cacheKey, schema)

	return schema
}

// fetchSchemaFromDisk tries to load a schema from the disk cache.
func fetchSchemaFromDisk(apiVersion, kind string) map[string]any {
	cacheDir := schemaCacheDir()

	for _, schemaURL := range schemaURLs(apiVersion, kind) {
		cachedPath := filepath.Join(cacheDir, schemaCacheFileName(schemaURL))

		data, err := os.ReadFile(cachedPath) //nolint:gosec // controlled cache directory
		if err != nil {
			continue
		}

		schema := parseJSONSchema(data)
		if schema != nil {
			return schema
		}
	}

	return nil
}

// schemaCacheDir returns the schema cache directory.
func schemaCacheDir() string {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ksail", "kubeconform")
	}

	return filepath.Join(userCacheDir, "ksail", "kubeconform")
}

// schemaCacheFileName produces a deterministic filename for caching a schema URL.
func schemaCacheFileName(schemaURL string) string {
	safe := strings.NewReplacer(
		"://", "_",
		"/", "_",
		".", "_",
	).Replace(schemaURL) + ".json"

	if len(safe) > schemaCacheFileMaxChars {
		safe = safe[len(safe)-schemaCacheFileMaxChars:]
	}

	return safe
}

// schemaURLs returns the candidate schema URLs for a given apiVersion/kind.
func schemaURLs(apiVersion, kind string) []string {
	kindLower := strings.ToLower(kind)
	group, version := splitAPIVersion(apiVersion)

	if group != "" {
		// Try Kubernetes JSON schema first (for core API groups like apps, batch, etc.),
		// then fall back to CRDs catalog for custom resources.
		return []string{
			fmt.Sprintf(
				"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/master-standalone/%s-%s-%s.json",
				kindLower,
				group,
				version,
			),
			fmt.Sprintf(
				"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/%s/%s_%s.json",
				group, kindLower, version,
			),
		}
	}

	return []string{
		fmt.Sprintf(
			"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/master-standalone/%s-%s.json",
			kindLower,
			version,
		),
	}
}

// splitAPIVersion splits "apps/v1" into ("apps", "v1") or "v1" into ("", "v1").
func splitAPIVersion(apiVersion string) (string, string) {
	parts := strings.SplitN(apiVersion, "/", 2) //nolint:mnd // splitting group/version
	if len(parts) == 2 {                        //nolint:mnd // group/version pair
		return parts[0], parts[1]
	}

	return "", parts[0]
}

// parseJSONSchema parses raw JSON bytes into a schema map.
func parseJSONSchema(data []byte) map[string]any {
	var schema map[string]any

	err := json.Unmarshal(data, &schema)
	if err != nil {
		return nil
	}

	return schema
}

// getSchemaTypeAtPath walks a JSON schema following a path like "/spec/replicas"
// and returns the type of the field ("string", "integer", "number", "boolean").
// Returns empty string when the schema is nil, path is empty, or type cannot be resolved.
func getSchemaTypeAtPath(schema map[string]any, path string) string {
	if schema == nil || path == "" {
		return ""
	}

	trimmed := strings.TrimPrefix(path, "/")
	segments := strings.Split(trimmed, "/")
	current := schema

	for _, seg := range segments {
		current = resolveSchemaNode(current, seg)
		if current == nil {
			return ""
		}
	}

	return schemaNodeType(current)
}

const typeString = "string"

// resolveSchemaNode navigates one level deeper into a JSON schema for a given key.
func resolveSchemaNode(schema map[string]any, key string) map[string]any {
	if result := resolveFromProperties(schema, key); result != nil {
		return result
	}

	if result := resolveFromItems(schema, key); result != nil {
		return result
	}

	return resolveFromCombiners(schema, key)
}

func resolveFromProperties(schema map[string]any, key string) map[string]any {
	props, found := schema["properties"].(map[string]any)
	if !found {
		return nil
	}

	child, childFound := props[key].(map[string]any)
	if !childFound {
		return nil
	}

	return child
}

func resolveFromItems(schema map[string]any, key string) map[string]any {
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return nil
	}

	if isNumericIndex(key) {
		return items
	}

	return nil
}

func resolveFromCombiners(schema map[string]any, key string) map[string]any {
	for _, combinerKey := range []string{"allOf", "anyOf", "oneOf"} {
		arr, ok := schema[combinerKey].([]any)
		if !ok {
			continue
		}

		for _, entry := range arr {
			sub, ok := entry.(map[string]any)
			if !ok {
				continue
			}

			if result := resolveSchemaNode(sub, key); result != nil {
				return result
			}
		}
	}

	return nil
}

// schemaNodeType extracts the type from a JSON schema node.
func schemaNodeType(schema map[string]any) string {
	if typeVal, ok := schema["type"].(string); ok {
		return typeVal
	}

	if arr, ok := schema["type"].([]any); ok {
		for _, item := range arr {
			if typeVal, ok := item.(string); ok && typeVal != "null" {
				return typeVal
			}
		}
	}

	return ""
}

// isNumericIndex checks if a string represents a numeric array index.
func isNumericIndex(str string) bool {
	if len(str) == 0 {
		return false
	}

	for _, char := range str {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}
