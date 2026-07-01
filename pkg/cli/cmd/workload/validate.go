package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/spf13/cobra"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
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

// validateLongDescription is the long help text for the validate command.
const validateLongDescription = `Validate Kubernetes manifest files and kustomizations using kubeconform.

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

By default, Kubernetes Secrets are skipped to avoid validation failures due to SOPS fields.`

// NewValidateCmd creates the workload validate command.
func NewValidateCmd() *cobra.Command {
	var (
		skipSecrets          bool
		strict               bool
		ignoreMissingSchemas bool
		skipHelmRender       bool
		skipKinds            []string
		schemaLocations      []string
	)

	cmd := &cobra.Command{
		Use:   "validate [PATH]",
		Short: "Validate Kubernetes manifests and kustomizations",
		Long:  validateLongDescription,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidateCmd(
				cmd.Context(),
				cmd,
				args,
				validateFlags{
					skipSecrets:          skipSecrets,
					strict:               strict,
					ignoreMissingSchemas: ignoreMissingSchemas,
					skipHelmRender:       skipHelmRender,
					skipKinds:            skipKinds,
					schemaLocations:      schemaLocations,
				},
			)
		},
	}

	addValidateFlags(
		cmd,
		&skipSecrets,
		&strict,
		&ignoreMissingSchemas,
		&skipHelmRender,
		&skipKinds,
		&schemaLocations,
	)

	return cmd
}

// validateFlags carries the resolved validate command flags.
type validateFlags struct {
	skipSecrets          bool
	strict               bool
	ignoreMissingSchemas bool
	skipHelmRender       bool
	skipKinds            []string
	schemaLocations      []string
}

// addValidateFlags registers the flags for the validate command.
func addValidateFlags(
	cmd *cobra.Command,
	skipSecrets, strict, ignoreMissingSchemas, skipHelmRender *bool,
	skipKinds, schemaLocations *[]string,
) {
	cmd.Flags().BoolVar(skipSecrets, "skip-secrets", true, "Skip validation of Kubernetes Secrets")
	cmd.Flags().BoolVar(strict, "strict", false, "Enable strict validation mode")
	cmd.Flags().BoolVar(
		ignoreMissingSchemas,
		"ignore-missing-schemas",
		true,
		"Ignore resources with missing schemas",
	)
	cmd.Flags().BoolVar(
		skipHelmRender,
		"skip-helm-render",
		false,
		"Skip rendering HelmReleases before validation (validate the HelmRelease CR as-is). "+
			"By default, charts are rendered in-process and the rendered manifests are validated.",
	)
	cmd.Flags().StringSliceVar(
		skipKinds,
		"skip-kinds",
		nil,
		"Additional Kubernetes kinds to skip during validation "+
			"(merged with spec.workload.validation.skipKinds from ksail.yaml)",
	)
	cmd.Flags().StringSliceVar(
		schemaLocations,
		"schema-location",
		nil,
		"Additional kubeconform schema locations (local directory or URL/path template) for CRDs "+
			"absent from the CRDs-catalog, so they are validated against a supplied schema instead "+
			"of skipped (merged with spec.workload.validation.schemaLocations from ksail.yaml)",
	)
}

func runValidateCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	flags validateFlags,
) error {
	// Load ksail.yaml exactly once. Both the source-directory and the
	// configured skip-kinds are derived from this single load, with
	// SkipDistributionConfig set so a Talos PKI bundle is not generated on
	// every validate run (the workload fields read here are distribution-
	// independent).
	cfg, configFound, loadErr := loadValidateConfigSilently(cmd)

	path, err := resolveValidatePath(args, cfg, configFound, loadErr)
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
		Strict:               flags.strict,
		IgnoreMissingSchemas: flags.ignoreMissingSchemas,
	}

	if flags.skipSecrets {
		validationOpts.SkipKinds = append(validationOpts.SkipKinds, "Secret")
	}

	// Additional kinds to skip come from the --skip-kinds flag and from
	// spec.workload.validation.skipKinds in ksail.yaml. This lets a repo opt out
	// of validating CRDs whose CRDs-catalog schema is stale or missing (which
	// kubeconform would otherwise reject as "additional properties not allowed").
	validationOpts.SkipKinds = append(validationOpts.SkipKinds, flags.skipKinds...)
	validationOpts.SkipKinds = append(
		validationOpts.SkipKinds,
		configuredSkipKinds(cmd, cfg, configFound, loadErr)...,
	)

	// Additional schema locations come from the --schema-location flag and from
	// spec.workload.validation.schemaLocations in ksail.yaml. They let a repo
	// validate CRDs absent from the CRDs-catalog against a supplied schema rather
	// than skipping the kind via --skip-kinds. Local filesystem locations are
	// canonicalized (see canonicalizeSchemaLocations) so validation reads the
	// intended schema tree; URLs and kubeconform path templates pass through.
	suppliedSchemaLocations := slices.Concat(
		flags.schemaLocations,
		configuredSchemaLocations(cmd, cfg, configFound, loadErr),
	)
	validationOpts.SchemaLocations = append(
		validationOpts.SchemaLocations,
		canonicalizeSchemaLocations(suppliedSchemaLocations)...,
	)

	renderer := buildValidateRenderer(cfg, configFound, flags.skipHelmRender)

	return validatePath(ctx, cmd, path, kubeconformClient, validationOpts, renderer)
}

// buildValidateRenderer returns a gitopsRenderer when Helm rendering is enabled,
// or nil when it is disabled (so validate falls back to validating the
// HelmRelease CR as-is). Rendering is on by default and disabled by
// --skip-helm-render or spec.workload.validation.helmRender: false.
func buildValidateRenderer(
	cfg *v1alpha1.Cluster,
	configFound, skipHelmRender bool,
) *gitopsRenderer {
	if !helmRenderEnabled(cfg, configFound, skipHelmRender) {
		return nil
	}

	return newGitOpsRenderer()
}

// helmRenderEnabled resolves whether validate renders HelmReleases: the
// --skip-helm-render flag forces it off; otherwise
// spec.workload.validation.helmRender governs, defaulting to true when unset.
func helmRenderEnabled(cfg *v1alpha1.Cluster, configFound, skipHelmRender bool) bool {
	if skipHelmRender {
		return false
	}

	if configFound && cfg != nil && cfg.Spec.Workload.Validation.HelmRender != nil {
		return *cfg.Spec.Workload.Validation.HelmRender
	}

	return true
}

// loadValidateConfigSilently loads ksail.yaml once (honoring --config) for the
// validate command. The returned cfg is nil when the load fails or no config
// file is found; configFound reports whether a config file was found and
// loadErr carries any load failure so callers can apply their own (path: fail,
// skip-kinds: warn) error policy.
//
// SkipDistributionConfig avoids loading distribution-specific config (e.g.
// Talos PKI generation) on every validate run; only the distribution-
// independent workload fields are consumed by the callers. The --config flag is
// resolved without registering additional flags on cmd to avoid the "flag
// redefined" panic that NewCommandConfigManager would cause.
func loadValidateConfigSilently(cmd *cobra.Command) (*v1alpha1.Cluster, bool, error) {
	var configFile string

	cfgPath, pathErr := flags.GetConfigPath(cmd)
	if pathErr == nil {
		configFile = cfgPath
	}

	cfgManager := configmanager.NewConfigManager(io.Discard, configFile)

	cfg, loadErr := cfgManager.Load(
		configmanagerinterface.LoadOptions{
			Silent:                 true,
			SkipValidation:         true,
			SkipDistributionConfig: true,
		},
	)

	found := cfgManager.IsConfigFileFound()
	if loadErr != nil {
		//nolint:wrapcheck // callers apply their own (fail vs warn) policy and wrap as needed
		return nil, found, loadErr
	}

	return cfg, found, nil
}

// resolveValidatePath determines which path to validate from the single config
// load. If an explicit argument is given, it is returned directly. Otherwise
// the configured sourceDirectory is used; a config file that exists but failed
// to load is an error (fail policy), and the absence of a config file falls
// back to ".".
func resolveValidatePath(
	args []string,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	switch {
	case loadErr != nil && configFound:
		return "", fmt.Errorf("load config: %w", loadErr)
	case configFound:
		return resolveSourceDir(cfg, ""), nil
	default:
		return ".", nil
	}
}

// configuredSkipKinds returns the configured spec.workload.validation.skipKinds
// from the single config load. It returns nil when no config file is found or
// the field is unset. A config file that exists but failed to load does not
// fail validation (the built-in skips still apply) but emits a warning so the
// omission is not silent.
func configuredSkipKinds(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
) []string {
	if loadErr != nil {
		// A config file exists but couldn't be read/parsed. Don't fail
		// validation, but warn so a typo or unreadable ksail.yaml doesn't
		// silently validate kinds that were meant to be skipped.
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "could not read spec.workload.validation.skipKinds from ksail.yaml: %v",
			Args:    []any{loadErr},
			Writer:  cmd.ErrOrStderr(),
		})

		return nil
	}

	if !configFound {
		return nil
	}

	return cfg.Spec.Workload.Validation.SkipKinds
}

// configuredSchemaLocations returns the configured
// spec.workload.validation.schemaLocations from the single config load. It
// returns nil when no config file is found or the field is unset. A config file
// that exists but failed to load does not fail validation but emits a warning so
// the omission is not silent.
func configuredSchemaLocations(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
) []string {
	if loadErr != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "could not read spec.workload.validation.schemaLocations from ksail.yaml: %v",
			Args:    []any{loadErr},
			Writer:  cmd.ErrOrStderr(),
		})

		return nil
	}

	if !configFound {
		return nil
	}

	return cfg.Spec.Workload.Validation.SchemaLocations
}

// canonicalizeSchemaLocations resolves local filesystem schema locations to real,
// absolute paths (expanding ~ and following symlinks) so validation reads the
// intended schema tree and symlink-escape is prevented in CI — matching how the
// validate target path and --config are canonicalized.
//
// A kubeconform schema location may also be a URL (contains "://") or a path
// template carrying kubeconform placeholders (contains "{{", e.g.
// "schemas/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"); those are
// not local paths and pass through verbatim. A local path that cannot be resolved
// (e.g. it does not exist) is left as supplied so kubeconform surfaces a clear error.
func canonicalizeSchemaLocations(locations []string) []string {
	canonical := make([]string, 0, len(locations))

	for _, loc := range locations {
		if strings.Contains(loc, "://") || strings.Contains(loc, "{{") {
			canonical = append(canonical, loc)

			continue
		}

		expanded, err := fsutil.ExpandHomePath(loc)
		if err != nil {
			canonical = append(canonical, loc)

			continue
		}

		resolved, err := fsutil.EvalCanonicalPath(expanded)
		if err != nil {
			canonical = append(canonical, expanded)

			continue
		}

		canonical = append(canonical, resolved)
	}

	return canonical
}

// validatePath validates all manifests in the given path. Helm rendering applies
// only to kustomization builds (directory inputs); a single-file input is
// validated as-is so a lone HelmRelease file remains a CR-schema check.
func validatePath(
	ctx context.Context,
	cmd *cobra.Command,
	path string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
	renderer *gitopsRenderer,
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
	return validateDirectory(ctx, cmd, path, kubeconformClient, opts, renderer)
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
	renderer *gitopsRenderer,
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
		validator := &kustomizationValidator{
			dirPath:     dirPath,
			kubeconform: kubeconformClient,
			kustomize:   kustomize.NewClient(),
			renderer:    renderer,
			sink:        &degradationSink{},
			opts:        opts,
		}

		err := validator.run(ctx, cmd, kustomizations, progressOpts)
		if err != nil {
			return err
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

// kustomizationValidator validates one kustomization directory, optionally
// Helm-rendering it first. Its validate method is the task function passed to
// the parallel progress group.
type kustomizationValidator struct {
	dirPath     string
	kubeconform *kubeconform.Client
	kustomize   *kustomize.Client
	renderer    *gitopsRenderer // nil → Helm rendering disabled
	sink        *degradationSink
	opts        *kubeconform.ValidationOptions
}

// run validates every kustomization directory in parallel and then reports any
// render degradations. Degradations are reported after the progress group so the
// warnings do not interleave with its ANSI output.
func (v *kustomizationValidator) run(
	ctx context.Context,
	cmd *cobra.Command,
	kustomizations []string,
	progressOpts []notify.ProgressOption,
) error {
	err := runParallelValidation(
		ctx, cmd, kustomizations, v.dirPath, "Validating kustomizations", "✅",
		v.validate,
		append(progressOpts, notify.WithCountLabel("kustomizations"))...,
	)

	v.sink.report(cmd)

	if err != nil {
		return fmt.Errorf("kustomization validation failed: %w", err)
	}

	return nil
}

// validate builds (and optionally renders) a kustomization and validates the
// output, simplifying any kustomize build error for readability.
func (v *kustomizationValidator) validate(ctx context.Context, kustDir string) error {
	err := v.validateSilent(ctx, kustDir)
	if err != nil {
		return simplifyBuildError(err, v.dirPath)
	}

	return nil
}

// validateSilent produces the validation input for a kustomization and validates
// it without output (for parallel execution). The kustomize build error is
// returned unwrapped so simplifyBuildError can strip its verbose prefix.
func (v *kustomizationValidator) validateSilent(ctx context.Context, kustDir string) error {
	data, attribution, err := v.manifests(ctx, kustDir)
	if err != nil {
		return err
	}

	// Attach per-call attribution without mutating the shared v.opts, which
	// validate fans out concurrently across kustomizations (see the -race guard
	// TestValidateRendersConcurrentlyNoRace). A shallow copy is enough: the other
	// fields are read-only during validation.
	opts := v.opts

	if attribution != nil {
		clone := kubeconform.ValidationOptions{}
		if v.opts != nil {
			clone = *v.opts
		}

		clone.Attribution = attribution
		opts = &clone
	}

	err = v.kubeconform.ValidateBytes(ctx, kustDir, data, opts)
	if err != nil {
		return fmt.Errorf("validate manifests: %w", err)
	}

	return nil
}

// manifests returns the validation input — the Helm-rendered output when rendering
// is enabled, otherwise the Flux-substituted kustomize build output — together with
// a resource-identity→source attribution map derived from the render provenance
// (nil when rendering is disabled or nothing is attributable).
func (v *kustomizationValidator) manifests(
	ctx context.Context,
	kustDir string,
) ([]byte, map[string]string, error) {
	if v.renderer != nil {
		result, err := v.renderer.expand(ctx, kustDir)
		if err != nil {
			return nil, nil, err
		}

		v.sink.add(result.Degradations)

		return result.Bytes(), attributionFromDocuments(result.Documents), nil
	}

	output, err := v.kustomize.Build(ctx, kustDir)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // unwrapped so simplifyBuildError strips the kustomize prefix
	}

	return fluxsubst.ExpandFluxSubstitutions(output.Bytes()), nil, nil
}

// attributionFromDocuments maps each rendered document's identity to a source
// descriptor ("HelmRelease <namespace>/<name>") so kubeconform validation failures
// can be traced to the originating HelmRelease layer. Documents that came verbatim
// from the input stream (OriginStream) carry no source and are omitted. When two
// distinct sources render the same identity the attribution is ambiguous, so that
// identity is dropped rather than mis-attributed. Returns nil when nothing is
// attributable, which leaves failure messages unchanged downstream.
func attributionFromDocuments(docs []render.Document) map[string]string {
	sources := make(map[string]string, len(docs))
	ambiguous := make(map[string]struct{})

	for _, doc := range docs {
		if doc.Provenance.Origin != render.OriginRendered ||
			doc.Provenance.SourceHelmRelease == "" {
			continue
		}

		identity := documentIdentity(doc)
		if identity == "" {
			continue
		}

		source := "HelmRelease " + doc.Provenance.SourceHelmRelease
		if existing, ok := sources[identity]; ok && existing != source {
			ambiguous[identity] = struct{}{}

			continue
		}

		sources[identity] = source
	}

	for identity := range ambiguous {
		delete(sources, identity)
	}

	if len(sources) == 0 {
		return nil
	}

	return sources
}

// documentIdentity builds the "Kind/Namespace/Name" (or "Kind/Name" for cluster-scoped
// resources) key used to match a rendered document against the resource identity
// kubeconform reports for a validation failure. It returns "" when the document lacks a
// Kind or Name, mirroring the kubeconform formatter's fallback so unkeyable documents
// are simply left unattributed.
func documentIdentity(doc render.Document) string {
	if doc.Kind == "" || doc.Name == "" {
		return ""
	}

	if doc.Namespace != "" {
		return doc.Kind + "/" + doc.Namespace + "/" + doc.Name
	}

	return doc.Kind + "/" + doc.Name
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
		fluxsubst.ExpandFluxSubstitutions(data),
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
