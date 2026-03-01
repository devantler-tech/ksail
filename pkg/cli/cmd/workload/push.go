package workload

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	registryhelpers "github.com/devantler-tech/ksail/v5/pkg/svc/registryresolver"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewPushCmd creates the workload push command.
func NewPushCmd(_ *di.Runtime) *cobra.Command {
	var (
		validate bool
		pathFlag string
	)

	// Create viper instance for registry flag/env binding (local to closure)
	viperInstance := viper.New()
	viperInstance.SetEnvPrefix(configmanager.EnvPrefix)
	viperInstance.AutomaticEnv()

	cmd := &cobra.Command{
		Use:          "push [oci://<host>:<port>/<repository>[/<variant>]:<ref>]",
		Short:        "Package and push an OCI artifact to a registry",
		Long:         pushCommandLongDescription(),
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runPushCommand(cmd, args, pathFlag, validate, viperInstance)
	}

	configurePushFlags(cmd, &validate, &pathFlag, viperInstance)

	return cmd
}

// pushCommandLongDescription returns the long description for the push command.
func pushCommandLongDescription() string {
	return `Build and push local workloads as an OCI artifact to a registry.

The OCI reference format is: oci://<host>:<port>/<repository>[/<variant>]:<ref>

Examples:
  # Push to auto-detected local registry with defaults
  ksail workload push

  # Push specific directory to auto-detected registry
  ksail workload push --path=./manifests

  # Push to explicit registry endpoint
  ksail workload push oci://localhost:5050/k8s:dev

  # Push to external registry with credentials
  ksail workload push --registry='$USER:$TOKEN@ghcr.io/org/repo'

  # Push using KSAIL_REGISTRY environment variable
  KSAIL_REGISTRY='ghcr.io/org/repo' ksail workload push

  # Push with variant (subdirectory in repository)
  ksail workload push oci://localhost:5050/my-app/base:v1.0.0 --path=./k8s

All parts of the OCI reference are optional and will be inferred:
  - host:port: Auto-detected from running local-registry container
  - repository: Derived from source directory name
  - ref: Defaults to "dev"`
}

// configurePushFlags configures flags for the push command.
func configurePushFlags(
	cmd *cobra.Command,
	validate *bool,
	pathFlag *string,
	viperInstance *viper.Viper,
) {
	cmd.Flags().BoolVar(validate, "validate", false, "Validate manifests before pushing")
	cmd.Flags().StringVar(pathFlag, "path", "", "Source directory containing manifests to push")
	cmd.Flags().String(
		"registry",
		"",
		"Registry to push to (format: [user:pass@]host[:port][/path]), can also be set via KSAIL_REGISTRY env var",
	)

	// Bind registry flag to viper for env var support (KSAIL_REGISTRY)
	_ = viperInstance.BindPFlag(registryhelpers.ViperRegistryKey, cmd.Flags().Lookup("registry"))
}

// runPushCommand executes the push logic with the provided parameters.
func runPushCommand(
	cmd *cobra.Command,
	args []string,
	pathFlag string,
	validate bool,
	viperInstance *viper.Viper,
) error {
	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	clusterCfg := cmdCtx.ClusterCfg
	outputTimer := cmdCtx.OutputTimer
	tmr := cmdCtx.Timer

	// Parse OCI reference if provided
	var ociRef *oci.Reference
	if len(args) > 0 {
		ociRef, err = oci.ParseReference(args[0])
		if err != nil {
			return fmt.Errorf("parse OCI reference: %w", err)
		}
	}

	// Resolve all parameters: host, port, repository, ref, source directory
	params, err := resolvePushParams(
		cmd, clusterCfg, ociRef, pathFlag, viperInstance, tmr, outputTimer,
	)
	if err != nil {
		return err
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📦",
		Content: "Build and Push OCI Artifact...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	// Validate if flag is set or config option is enabled
	if validate || clusterCfg.Spec.Workload.ValidateOnPush {
		validateErr := validateManifests(cmd, params.SourceDir, outputTimer)
		if validateErr != nil {
			return validateErr
		}
	}

	return buildAndPushArtifact(cmd, params, outputTimer)
}

// validateManifests runs manifest validation and reports progress.
func validateManifests(cmd *cobra.Command, sourceDir string, outputTimer timer.Timer) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "validating manifests",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err := runValidateCmd(
		cmd.Context(),
		cmd,
		[]string{sourceDir},
		true,  // skipSecrets
		true,  // strict
		true,  // ignoreMissingSchemas
		false, // verbose
	)
	if err != nil {
		return fmt.Errorf("validate manifests: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "manifests validated",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// buildAndPushArtifact builds an OCI artifact from the source directory and pushes it.
func buildAndPushArtifact(
	cmd *cobra.Command,
	params *pushParams,
	outputTimer timer.Timer,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "building oci artifact",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	registryDisplay, registryEndpoint := formatRegistryEndpoints(params)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing to %s",
		Args:    []any{registryDisplay},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	builder := oci.NewWorkloadArtifactBuilder()

	_, err := builder.Build(cmd.Context(), oci.BuildOptions{
		Name:             params.Repository,
		SourcePath:       params.SourceDir,
		RegistryEndpoint: registryEndpoint,
		Repository:       params.Repository,
		Version:          params.Ref,
		GitOpsEngine:     params.GitOpsEngine,
		Username:         params.Username,
		Password:         params.Password,
	})
	if err != nil {
		return fmt.Errorf("build and push oci artifact: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "oci artifact pushed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// formatRegistryEndpoints returns display and endpoint strings for a registry.
func formatRegistryEndpoints(params *pushParams) (string, string) {
	if params.Port > 0 {
		return fmt.Sprintf(
				"%s:%d/%s:%s", params.Host, params.Port, params.Repository, params.Ref,
			),
			fmt.Sprintf("%s:%d", params.Host, params.Port)
	}

	// External registry - no port (HTTPS implicit)
	return fmt.Sprintf("%s/%s:%s", params.Host, params.Repository, params.Ref),
		params.Host
}

// pushParams holds all resolved parameters for the push operation.
type pushParams struct {
	Host         string
	Port         int32
	Repository   string
	Ref          string
	SourceDir    string
	GitOpsEngine v1alpha1.GitOpsEngine
	Username     string
	Password     string //nolint:gosec // G117: field name required by API schema
	IsExternal   bool   // True if this is an external registry (no auto-detection needed)
}

// resolvePushParams resolves all push parameters using priority-based detection.
//
// Priority order for registry resolution:
// 1. CLI flag or env var via Viper (--registry / KSAIL_REGISTRY)
// 2. Config file (ksail.yaml localRegistry)
// 3. Cluster GitOps resources (FluxInstance or ArgoCD Application)
// 4. Docker containers (matching cluster name)
// 5. Error (no registry found).
func resolvePushParams(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	ociRef *oci.Reference,
	pathFlag string,
	viperInstance *viper.Viper,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (*pushParams, error) {
	// If OCI reference is fully specified, use it directly without detection
	if ociRef != nil && ociRef.Host != "" && ociRef.Port > 0 {
		return newPushParamsFromOCIRef(cfg, ociRef, pathFlag), nil
	}

	registryInfo, err := detectRegistry(cmd, cfg, viperInstance, tmr, outputTimer)
	if err != nil {
		return nil, err
	}

	// Build params from detected registry info
	params := &pushParams{
		Host:       registryInfo.Host,
		Port:       registryInfo.Port,
		Repository: registryInfo.Repository,
		Username:   registryInfo.Username,
		Password:   registryInfo.Password,
		IsExternal: registryInfo.IsExternal,
		SourceDir:  resolveSourceDir(cfg, pathFlag),
		Ref:        resolveRef(ociRef, registryInfo.Tag),
	}

	// Override with OCI reference values if provided
	applyOCIRefOverrides(params, ociRef)

	// Fallback repository from source directory if not set
	if params.Repository == "" {
		params.Repository = registry.SanitizeRepoName(params.SourceDir)
	}

	// Resolve GitOps engine
	params.GitOpsEngine = resolveGitOpsEngine(cfg)

	// Show success message with source using proper host:port formatting
	displayURL := registryhelpers.FormatRegistryURL(params.Host, params.Port, params.Repository)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "%s (from %s)",
		Args:    []any{displayURL, registryInfo.Source},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return params, nil
}

// detectRegistry shows the detection UI and resolves registry information.
func detectRegistry(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	viperInstance *viper.Viper,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (*registryhelpers.Info, error) {
	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔎",
		Content: "Get registry details...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "resolving registry configuration",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	registryInfo, err := registryhelpers.ResolveRegistry(
		cmd.Context(),
		registryhelpers.ResolveRegistryOptions{
			Viper:         viperInstance,
			ClusterConfig: cfg,
			ClusterName:   cfg.Spec.Cluster.Connection.Context,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resolve registry: %w", err)
	}

	return registryInfo, nil
}

// applyOCIRefOverrides applies OCI reference values to params if provided.
func applyOCIRefOverrides(params *pushParams, ociRef *oci.Reference) {
	if ociRef == nil {
		return
	}

	if ociRef.Host != "" {
		params.Host = ociRef.Host
	}

	if ociRef.Port > 0 {
		params.Port = ociRef.Port
	}

	if ociRef.FullRepository() != "" {
		params.Repository = ociRef.FullRepository()
	}

	if ociRef.Ref != "" {
		params.Ref = ociRef.Ref
	}
}

// newPushParamsFromOCIRef creates push params when a complete OCI reference is provided.
func newPushParamsFromOCIRef(
	cfg *v1alpha1.Cluster,
	ociRef *oci.Reference,
	pathFlag string,
) *pushParams {
	return &pushParams{
		Host:         ociRef.Host,
		Port:         ociRef.Port,
		Repository:   ociRef.FullRepository(),
		Ref:          ociRef.Ref,
		SourceDir:    resolveSourceDir(cfg, pathFlag),
		GitOpsEngine: resolveGitOpsEngine(cfg),
		IsExternal:   false,
	}
}

// resolveSourceDir determines the source directory from flag, config, or default.
func resolveSourceDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	if dir := strings.TrimSpace(pathFlag); dir != "" {
		return dir
	}

	if dir := strings.TrimSpace(cfg.Spec.Workload.SourceDirectory); dir != "" {
		return dir
	}

	return v1alpha1.DefaultSourceDirectory
}

// resolveRef determines the artifact ref/tag from OCI ref, config tag, or default.
// Priority: OCI ref > config tag > default.
func resolveRef(ociRef *oci.Reference, configTag string) string {
	if ociRef != nil && ociRef.Ref != "" {
		return ociRef.Ref
	}

	if configTag != "" {
		return configTag
	}

	return registry.DefaultLocalArtifactTag
}

// resolveGitOpsEngine determines GitOps engine from config.
func resolveGitOpsEngine(cfg *v1alpha1.Cluster) v1alpha1.GitOpsEngine {
	if cfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineNone {
		return cfg.Spec.Cluster.GitOpsEngine
	}

	return v1alpha1.GitOpsEngineNone
}
