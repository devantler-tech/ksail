package workload

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// NewPushCmd creates the workload push command.
func NewPushCmd(_ *runtime.Runtime) *cobra.Command {
	var (
		validate     bool
		pathFlag     string
		registryFlag string
	)

	cmd := &cobra.Command{
		Use:   "push [oci://<host>:<port>/<repository>[/<variant>]:<ref>]",
		Short: "Package and push an OCI artifact to a registry",
		Long: `Build and push local workloads as an OCI artifact to a registry.

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

  # Push with variant (subdirectory in repository)
  ksail workload push oci://localhost:5050/my-app/base:v1.0.0 --path=./k8s

All parts of the OCI reference are optional and will be inferred:
  - host:port: Auto-detected from running local-registry container
  - repository: Derived from source directory name
  - ref: Defaults to "dev"`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runPushCommand(cmd, args, pathFlag, registryFlag, validate)
	}

	cmd.Flags().BoolVar(&validate, "validate", false, "Validate manifests before pushing")
	cmd.Flags().StringVar(&pathFlag, "path", "", "Source directory containing manifests to push")
	cmd.Flags().StringVar(
		&registryFlag,
		"registry",
		"",
		"Registry to push to (format: [user:pass@]host[:port][/path])",
	)

	return cmd
}

// runPushCommand executes the push logic with the provided parameters.
//
//nolint:funlen // Command execution logic with multiple stages
func runPushCommand(
	cmd *cobra.Command,
	args []string,
	pathFlag, registryFlag string,
	validate bool,
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
		cmd, clusterCfg, ociRef, pathFlag, registryFlag, tmr, outputTimer,
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
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "validating manifests",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		err = runValidateCmd(
			cmd.Context(),
			cmd,
			[]string{params.SourceDir},
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
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "building oci artifact",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Format registry reference for display and endpoint
	var registryDisplay, registryEndpoint string
	if params.Port > 0 {
		registryDisplay = fmt.Sprintf(
			"%s:%d/%s:%s", params.Host, params.Port, params.Repository, params.Ref,
		)
		registryEndpoint = fmt.Sprintf("%s:%d", params.Host, params.Port)
	} else {
		// External registry - no port (HTTPS implicit)
		registryDisplay = fmt.Sprintf("%s/%s:%s", params.Host, params.Repository, params.Ref)
		registryEndpoint = params.Host
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing to %s",
		Args:    []any{registryDisplay},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	builder := oci.NewWorkloadArtifactBuilder()

	_, err = builder.Build(cmd.Context(), oci.BuildOptions{
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

// pushParams holds all resolved parameters for the push operation.
type pushParams struct {
	Host         string
	Port         int32
	Repository   string
	Ref          string
	SourceDir    string
	GitOpsEngine v1alpha1.GitOpsEngine
	Username     string
	Password     string
	IsExternal   bool // True if this is an external registry (no auto-detection needed)
}

// resolvePushParams resolves all push parameters from OCI reference, flags, config, or auto-detection.
func resolvePushParams(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	ociRef *oci.Reference,
	pathFlag string,
	registryFlag string,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (*pushParams, error) {
	params := newPushParamsFromSources(cfg, ociRef, pathFlag, registryFlag)

	// Only auto-detect for local registries without explicit port
	// External registries with port 0 are valid (HTTPS implicit)
	needsDetection := ociRef == nil || ociRef.Port == 0
	if needsDetection && params.Port == 0 && !params.IsExternal {
		err := autoDetectMissingParams(cmd, params, tmr, outputTimer)
		if err != nil {
			return nil, err
		}
	}

	return params, nil
}

// newPushParamsFromSources creates push params from OCI ref, config, and path flag.
// Priority: OCI ref > --registry flag > config.
func newPushParamsFromSources(
	cfg *v1alpha1.Cluster,
	ociRef *oci.Reference,
	pathFlag string,
	registryFlag string,
) *pushParams {
	params := &pushParams{Host: "localhost"}

	// If --registry flag is provided, use it to create a temporary LocalRegistry for parsing
	var registrySpec v1alpha1.LocalRegistry
	if strings.TrimSpace(registryFlag) != "" {
		registrySpec = v1alpha1.LocalRegistry{Registry: registryFlag}
	} else {
		registrySpec = cfg.Spec.Cluster.LocalRegistry
	}

	params.SourceDir = resolveSourceDir(cfg, pathFlag)
	params.Host = resolveHostFromRegistry(ociRef, registrySpec)
	params.Port = resolvePortFromRegistry(registrySpec, ociRef)
	params.Repository = resolveRepositoryFromRegistry(registrySpec, ociRef, params.SourceDir)
	params.Ref = resolveRef(ociRef)
	params.GitOpsEngine = resolveGitOpsEngine(cfg)
	params.IsExternal = registrySpec.IsExternal()

	// Resolve credentials from registry spec (with environment variable expansion)
	username, password := registrySpec.ResolveCredentials()
	params.Username = username
	params.Password = password

	return params
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

// resolveHostFromRegistry extracts host from OCI ref, registry spec, or returns default.
func resolveHostFromRegistry(ociRef *oci.Reference, reg v1alpha1.LocalRegistry) string {
	if ociRef != nil && ociRef.Host != "" {
		return ociRef.Host
	}

	// Check if registry is configured
	if reg.Enabled() {
		return reg.ResolvedHost()
	}

	return "localhost"
}

// resolvePortFromRegistry determines port from OCI ref, registry spec, or returns 0 for auto-detection.
// For external registries (non-localhost), returns 0 to indicate HTTPS with implicit port.
func resolvePortFromRegistry(reg v1alpha1.LocalRegistry, ociRef *oci.Reference) int32 {
	if ociRef != nil && ociRef.Port > 0 {
		return ociRef.Port
	}

	if !reg.Enabled() {
		return 0 // Will trigger auto-detection
	}

	// For external registries, return 0 (HTTPS with implicit port)
	if reg.IsExternal() {
		return reg.ResolvedPort() // Returns 0 for external without explicit port
	}

	return reg.ResolvedPort()
}

// resolveRepositoryFromRegistry determines repository name from OCI ref, registry path, or source directory.
// For external registries, the registry path is used as the repository prefix.
func resolveRepositoryFromRegistry(
	reg v1alpha1.LocalRegistry,
	ociRef *oci.Reference,
	sourceDir string,
) string {
	if ociRef != nil && ociRef.FullRepository() != "" {
		return ociRef.FullRepository()
	}

	// For external registries, use the path from registry spec as the repository
	if reg.IsExternal() {
		path := reg.ResolvedPath()
		if path != "" {
			return path
		}
	}

	return registry.SanitizeRepoName(sourceDir)
}

// resolveRef determines the artifact ref/tag from OCI ref or default.
func resolveRef(ociRef *oci.Reference) string {
	if ociRef != nil && ociRef.Ref != "" {
		return ociRef.Ref
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

// autoDetectMissingParams fills in missing params from environment detection.
func autoDetectMissingParams(
	cmd *cobra.Command,
	params *pushParams,
	tmr timer.Timer,
	outputTimer timer.Timer,
) error {
	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔎",
		Content: "Auto-detect registry...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "detecting oci registry to push to",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	env, err := helpers.DetectClusterEnvironment(cmd.Context())
	if err != nil {
		return fmt.Errorf("detect cluster environment: %w", err)
	}

	if params.Port == 0 {
		params.Port = env.RegistryPort
	}

	if params.GitOpsEngine == v1alpha1.GitOpsEngineNone {
		params.GitOpsEngine = env.GitOpsEngine
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "oci://%s:%d detected",
		Args:    []any{params.Host, params.Port},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
