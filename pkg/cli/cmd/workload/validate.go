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
	"github.com/devantler-tech/ksail/v7/pkg/svc/crdschema"
	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/celrules"
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
` + sourcePathResolutionHelp + `

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
	flags := &validateFlags{}

	cmd := &cobra.Command{
		Use:   "validate [PATH]",
		Short: "Validate Kubernetes manifests and kustomizations",
		Long:  validateLongDescription,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidateCmd(cmd.Context(), cmd, args, *flags)
		},
	}

	addValidateFlags(cmd, flags)

	return cmd
}

// validateFlags carries the validate command flags. cobra binds each flag directly
// to a field here (see addValidateFlags), and runValidateCmd reads them.
type validateFlags struct {
	skipSecrets          bool
	strict               bool
	ignoreMissingSchemas bool
	skipHelmRender       bool
	includeCRDSchemas    bool
	skipKinds            []string
	schemaLocations      []string
	rules                string
}

// addValidateFlags registers the flags for the validate command, binding each to a
// field of flags.
func addValidateFlags(cmd *cobra.Command, flags *validateFlags) {
	cmd.Flags().
		BoolVar(&flags.skipSecrets, "skip-secrets", true, "Skip validation of Kubernetes Secrets")
	cmd.Flags().BoolVar(&flags.strict, "strict", false, "Enable strict validation mode")
	cmd.Flags().BoolVar(
		&flags.ignoreMissingSchemas,
		"ignore-missing-schemas",
		true,
		"Ignore resources with missing schemas",
	)
	cmd.Flags().BoolVar(
		&flags.skipHelmRender,
		"skip-helm-render",
		false,
		"Skip rendering HelmReleases before validation (validate the HelmRelease CR as-is). "+
			"By default, charts are rendered in-process and the rendered manifests are validated.",
	)
	cmd.Flags().StringSliceVar(
		&flags.skipKinds,
		"skip-kinds",
		nil,
		"Additional Kubernetes kinds to skip during validation "+
			"(merged with spec.workload.validation.skipKinds from ksail.yaml)",
	)
	cmd.Flags().StringSliceVar(
		&flags.schemaLocations,
		"schema-location",
		nil,
		"Additional kubeconform schema locations (local directory or URL/path template) for CRDs "+
			"absent from the CRDs-catalog, so they are validated against a supplied schema instead "+
			"of skipped (merged with spec.workload.validation.schemaLocations from ksail.yaml)",
	)
	cmd.Flags().BoolVar(
		&flags.includeCRDSchemas,
		"include-crd-schemas",
		false,
		"Derive kubeconform schemas from CustomResourceDefinition manifests in the path so that "+
			"custom resources whose CRD ships in the repo are validated instead of skipped "+
			"(off by default; a CRD that cannot be converted is warned and skipped)",
	)
	cmd.Flags().StringVar(
		&flags.rules,
		"rules",
		"",
		"Path to a YAML CEL rules file. Each rule's CEL expression is evaluated against every "+
			"rendered document (bound to the 'object' variable); an error-severity violation fails "+
			"validation, a warning-severity violation is reported without failing. "+
			"Overrides spec.workload.validation.rules from ksail.yaml.",
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

	// Built before buildValidationOptions so that, when --include-crd-schemas is
	// set, CRD-schema discovery can also render every kustomization and inspect
	// its output (a CRD a chart ships under templates/ is invisible to a raw-tree
	// walk). Reusing this same renderer for the real validation pass below lets
	// its chart cache (pkg/svc/gitops/render.ChartCache) serve the second render
	// of an already-seen chart from memory instead of re-templating it.
	renderer := buildValidateRenderer(cfg, configFound, flags.skipHelmRender)

	// Assemble validation options (skip-kinds, schema locations, and — when
	// --include-crd-schemas is set — schemas derived from in-tree CRDs plus, when
	// Helm rendering is enabled, from rendered kustomization output). cleanup
	// removes any temporary CRD-schema directory once validation completes.
	validationOpts, cleanup, err := buildValidationOptions(
		ctx,
		cmd,
		cfg,
		configFound,
		loadErr,
		flags,
		path,
		renderer,
	)
	if err != nil {
		return err
	}

	defer cleanup()

	// Compile the CEL rules once up front so a malformed rules file fails fast
	// (before any manifest is processed) rather than silently skipping the rules.
	// The path comes from --rules or, when the flag is empty, from
	// spec.workload.validation.rules in ksail.yaml. A nil engine means neither was
	// set (CEL validation disabled).
	rulesPath := resolveCELRulesPath(cmd, cfg, configFound, loadErr, flags.rules)

	engine, err := buildCELEngine(rulesPath)
	if err != nil {
		return err
	}

	return validatePath(ctx, cmd, path, kubeconformClient, validationOpts, renderer, engine)
}

// buildValidationOptions assembles the kubeconform validation options from the
// flags and ksail.yaml config: skip-kinds (--skip-kinds + spec.workload.validation
// .skipKinds), schema locations (--schema-location + the configured locations), and
// — when --include-crd-schemas is set — schemas derived from CustomResourceDefinition
// manifests in the path, plus (when renderer is non-nil) from every kustomization's
// rendered output, so custom resources whose CRD ships in the repo OR is only
// produced by Helm/Kustomize rendering are validated instead of skipped. It
// returns the options plus a cleanup func (always non-nil, safe to defer) that
// removes any temporary CRD-schema directory.
func buildValidationOptions(
	ctx context.Context,
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
	flags validateFlags,
	path string,
	renderer *gitopsRenderer,
) (*kubeconform.ValidationOptions, func(), error) {
	validationOpts := &kubeconform.ValidationOptions{
		Strict:               flags.strict,
		IgnoreMissingSchemas: flags.ignoreMissingSchemas,
	}

	if flags.skipSecrets {
		validationOpts.SkipKinds = append(validationOpts.SkipKinds, "Secret")
	}

	validationOpts.SkipKinds = append(validationOpts.SkipKinds, flags.skipKinds...)
	validationOpts.SkipKinds = append(
		validationOpts.SkipKinds,
		configuredSkipKinds(cmd, cfg, configFound, loadErr)...,
	)

	// Local filesystem locations are canonicalized (see canonicalizeSchemaLocations)
	// so validation reads the intended schema tree; URLs and kubeconform path
	// templates pass through.
	suppliedSchemaLocations := slices.Concat(
		flags.schemaLocations,
		configuredSchemaLocations(cmd, cfg, configFound, loadErr),
	)
	validationOpts.SchemaLocations = append(
		validationOpts.SchemaLocations,
		canonicalizeSchemaLocations(suppliedSchemaLocations)...,
	)

	cleanup := func() {}

	if flags.includeCRDSchemas {
		crdCleanup, err := addCRDSchemas(ctx, cmd, path, renderer, validationOpts)
		if err != nil {
			return nil, nil, err
		}

		cleanup = crdCleanup
	}

	return validationOpts, cleanup, nil
}

// addCRDSchemas derives kubeconform schemas from CustomResourceDefinition
// manifests under path into a temporary directory: first from the raw source
// tree (crdschema.Materialize), then — when renderer is non-nil — from every
// kustomization's rendered output (addRenderedCRDSchemas), so a CRD that only
// exists after Helm/Kustomize rendering (e.g. shipped under a chart's
// templates/) is discovered too. It appends that directory as a schema location
// on opts and warns about any CRD (or kustomization render) that could not be
// converted. It returns a cleanup function (always non-nil, safe to defer) that
// removes the temporary directory once validation has completed.
func addCRDSchemas(
	ctx context.Context,
	cmd *cobra.Command,
	path string,
	renderer *gitopsRenderer,
	opts *kubeconform.ValidationOptions,
) (func(), error) {
	tempDir, err := os.MkdirTemp("", "ksail-crd-schemas-")
	if err != nil {
		return nil, fmt.Errorf("create CRD schema directory: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(tempDir) }

	result, err := crdschema.Materialize(path, tempDir)
	if err != nil {
		cleanup()

		return nil, fmt.Errorf("derive CRD schemas: %w", err)
	}

	if renderer != nil {
		renderedResult, err := addRenderedCRDSchemas(ctx, path, renderer, tempDir)
		if err != nil {
			cleanup()

			return nil, err
		}

		result.Written += renderedResult.Written
		result.Warnings = append(result.Warnings, renderedResult.Warnings...)
	}

	for _, warning := range result.Warnings {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "skipped CRD schema: %s",
			Args:    []any{warning.String()},
			Writer:  cmd.ErrOrStderr(),
		})
	}

	if result.Written > 0 {
		opts.SchemaLocations = append(opts.SchemaLocations, crdschema.SchemaLocation(tempDir))
	}

	return cleanup, nil
}

// addRenderedCRDSchemas renders every kustomization under path and extracts CRD
// schemas from each rendered manifest stream into destDir, discovering CRDs that
// only exist after Helm/Kustomize rendering (invisible to a raw source-tree
// walk). A kustomization that fails to render is skipped with a warning rather
// than failing the run — --include-crd-schemas degrades gracefully, matching
// crdschema.Materialize's own per-CRD warning behaviour. Reusing renderer (the
// same instance the real validation pass below uses) lets its chart cache serve
// a chart already rendered here from memory instead of re-templating it.
func addRenderedCRDSchemas(
	ctx context.Context,
	path string,
	renderer *gitopsRenderer,
	destDir string,
) (crdschema.Result, error) {
	var result crdschema.Result

	kustomizations, err := findKustomizations(path)
	if err != nil {
		return result, fmt.Errorf("find kustomizations for CRD schema discovery: %w", err)
	}

	for _, kustDir := range kustomizations {
		rendered, err := renderer.expand(ctx, kustDir)
		if err != nil {
			result.Warnings = append(result.Warnings, crdschema.Warning{
				Source: kustDir,
				Reason: "render: " + err.Error(),
			})

			continue
		}

		docResult, err := crdschema.MaterializeBytes(
			rendered.Bytes(),
			kustDir+" (rendered)",
			destDir,
		)
		if err != nil {
			return result, fmt.Errorf("derive rendered CRD schemas for %s: %w", kustDir, err)
		}

		result.Written += docResult.Written
		result.Warnings = append(result.Warnings, docResult.Warnings...)
	}

	return result, nil
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

// configuredRules returns the configured spec.workload.validation.rules from the
// single config load, or "" when no config file is found or the field is unset. A
// config file that exists but failed to load does not fail validation but emits a
// warning so the omission is not silent (mirrors configuredSkipKinds).
func configuredRules(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
) string {
	if loadErr != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "could not read spec.workload.validation.rules from ksail.yaml: %v",
			Args:    []any{loadErr},
			Writer:  cmd.ErrOrStderr(),
		})

		return ""
	}

	if !configFound {
		return ""
	}

	return cfg.Spec.Workload.Validation.Rules
}

// resolveCELRulesPath returns the effective CEL rules file path: the --rules flag
// takes precedence (single-file semantics, unlike the merge used for skipKinds and
// schemaLocations), falling back to spec.workload.validation.rules from ksail.yaml
// when the flag is empty.
func resolveCELRulesPath(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	configFound bool,
	loadErr error,
	rulesFlag string,
) string {
	if rulesFlag != "" {
		return rulesFlag
	}

	return configuredRules(cmd, cfg, configFound, loadErr)
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
	engine *celrules.Engine,
) error {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("access path %s: %w", path, err)
	}

	// If it's a file, validate it directly
	if !info.IsDir() {
		rootDir := filepath.Dir(path)

		return validateFile(ctx, cmd, rootDir, path, kubeconformClient, opts, engine)
	}

	// If it's a directory, walk it to find YAML files and kustomizations
	return validateDirectory(ctx, cmd, path, kubeconformClient, opts, renderer, engine)
}

// validateFile validates a single YAML file, then applies any CEL rules. A
// single-file input is not part of a parallel progress group, so its own CEL
// warning sink is reported inline once validation completes.
func validateFile(
	ctx context.Context,
	cmd *cobra.Command,
	rootDir string,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
	engine *celrules.Engine,
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

	sink := &celViolationSink{}

	err := validateFileSilent(ctx, rootDir, filePath, kubeconformClient, opts, engine, sink)

	sink.report(cmd)

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
	engine *celrules.Engine,
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

	// One CEL warning sink for the whole directory: both progress groups feed it,
	// and it is reported once after they complete so warnings don't interleave
	// with the ANSI progress display.
	celSink := &celViolationSink{}
	defer celSink.report(cmd)

	if len(kustomizations) > 0 {
		validator := &kustomizationValidator{
			dirPath:     dirPath,
			kubeconform: kubeconformClient,
			kustomize:   kustomize.NewClient(),
			renderer:    renderer,
			sink:        &degradationSink{},
			opts:        opts,
			engine:      engine,
			celSink:     celSink,
		}

		err := validator.run(ctx, cmd, kustomizations, progressOpts)
		if err != nil {
			return err
		}
	}

	// Validate individual YAML files in parallel with progress display
	err = validateYAMLFiles(
		ctx, cmd, dirPath, yamlFiles, kubeconformClient, opts, engine, celSink, progressOpts,
	)
	if err != nil {
		return err
	}

	return nil
}

// validateYAMLFiles validates the loose YAML files in a directory in parallel,
// applying kubeconform and any CEL rules to each. It is a no-op when there are
// no files. Split from validateDirectory to keep both readably short.
func validateYAMLFiles(
	ctx context.Context,
	cmd *cobra.Command,
	dirPath string,
	yamlFiles []string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
	engine *celrules.Engine,
	celSink *celViolationSink,
	progressOpts []notify.ProgressOption,
) error {
	if len(yamlFiles) == 0 {
		return nil
	}

	err := runParallelValidation(
		ctx, cmd, yamlFiles, dirPath, "Validating YAML files", "📄",
		func(taskCtx context.Context, file string) error {
			return validateFileSilent(
				taskCtx, dirPath, file, kubeconformClient, opts, engine, celSink,
			)
		},
		append(progressOpts, notify.WithCountLabel("files"))...,
	)
	if err != nil {
		return fmt.Errorf("yaml validation failed: %w", err)
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
	engine      *celrules.Engine  // nil → CEL rule validation disabled
	celSink     *celViolationSink // collects warning-severity CEL violations
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

	// Apply CEL rules to the same rendered/built manifests kubeconform validated,
	// honoring the same kind exclusions (--skip-kinds / --skip-secrets) and the
	// same render-provenance attribution, so a skipped kind cannot surface a CEL
	// failure and a rendered document's violation is traced to its HelmRelease.
	err = evaluateCELDocuments(v.engine, data, kustDir, opts.SkipKinds, v.celSink, opts.Attribution)
	if err != nil {
		return err
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

// validateFileSilent validates a single YAML file without output (for parallel
// execution), then applies any CEL rules to the same Flux-substituted content.
func validateFileSilent(
	ctx context.Context,
	rootDir string,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
	engine *celrules.Engine,
	celSink *celViolationSink,
) error {
	// Only validate YAML files
	if !isYAMLFile(filePath) {
		return nil
	}

	data, err := fsutil.ReadFileSafe(rootDir, filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	expanded := fluxsubst.ExpandFluxSubstitutions(data)

	err = kubeconformClient.ValidateBytes(ctx, filePath, expanded, opts)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	// Apply CEL rules to the same content kubeconform validated, honoring the same
	// kind exclusions (--skip-kinds / --skip-secrets) so a skipped kind cannot
	// surface a CEL failure. Loose files carry no render provenance, so
	// opts.Attribution is nil here and descriptions are unchanged.
	err = evaluateCELDocuments(
		engine, expanded, filePath, opts.SkipKinds, celSink, opts.Attribution,
	)
	if err != nil {
		return err
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
