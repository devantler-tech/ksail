package workload

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	iopkg "github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// NewPushCmd creates the workload push command.
func NewPushCmd(_ *runtime.Runtime) *cobra.Command {
	var (
		validate bool
		pathFlag string
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
  ksail workload push oci://localhost:5111/k8s:dev

  # Push with variant (subdirectory in repository)
  ksail workload push oci://localhost:5111/my-app/base:v1.0.0 --path=./k8s

All parts of the OCI reference are optional and will be inferred:
  - host:port: Auto-detected from running local-registry container
  - repository: Derived from source directory name
  - ref: Defaults to "dev"`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runPushCommand(cmd, args, pathFlag, validate)
	}

	cmd.Flags().BoolVar(&validate, "validate", false, "Validate manifests before pushing")
	cmd.Flags().StringVar(&pathFlag, "path", "", "Source directory containing manifests to push")

	return cmd
}

// runPushCommand executes the push logic with the provided parameters.
//
//nolint:funlen // Command execution logic with multiple stages
func runPushCommand(cmd *cobra.Command, args []string, pathFlag string, validate bool) error {
	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	clusterCfg := cmdCtx.ClusterCfg
	outputTimer := cmdCtx.OutputTimer
	tmr := cmdCtx.Timer

	// Parse OCI reference if provided
	var ociRef *iopkg.OCIReference
	if len(args) > 0 {
		ociRef, err = iopkg.ParseOCIReference(args[0])
		if err != nil {
			return fmt.Errorf("parse OCI reference: %w", err)
		}
	}

	// Resolve all parameters: host, port, repository, ref, source directory
	params, err := resolvePushParams(cmd, clusterCfg, ociRef, pathFlag, outputTimer)
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

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing to %s:%d/%s:%s",
		Args:    []any{params.Host, params.Port, params.Repository, params.Ref},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	builder := oci.NewWorkloadArtifactBuilder()

	_, err = builder.Build(cmd.Context(), oci.BuildOptions{
		Name:             params.Repository,
		SourcePath:       params.SourceDir,
		RegistryEndpoint: fmt.Sprintf("%s:%d", params.Host, params.Port),
		Repository:       params.Repository,
		Version:          params.Ref,
		GitOpsEngine:     params.GitOpsEngine,
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
}

// resolvePushParams resolves all push parameters from OCI reference, flags, config, or auto-detection.
func resolvePushParams(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	ociRef *iopkg.OCIReference,
	pathFlag string,
	outputTimer timer.Timer,
) (*pushParams, error) {
	params := newPushParamsFromSources(cfg, ociRef, pathFlag)

	needsDetection := ociRef == nil || ociRef.Port == 0
	if needsDetection && params.Port == 0 {
		err := autoDetectMissingParams(cmd, params, outputTimer)
		if err != nil {
			return nil, err
		}
	}

	return params, nil
}

// newPushParamsFromSources creates push params from OCI ref, config, and path flag.
func newPushParamsFromSources(
	cfg *v1alpha1.Cluster,
	ociRef *iopkg.OCIReference,
	pathFlag string,
) *pushParams {
	params := &pushParams{Host: "localhost"}

	params.SourceDir = resolveSourceDir(cfg, pathFlag)
	params.Host = resolveHost(ociRef)
	params.Port = resolvePort(cfg, ociRef)
	params.Repository = resolveRepository(ociRef, params.SourceDir)
	params.Ref = resolveRef(ociRef)
	params.GitOpsEngine = resolveGitOpsEngine(cfg)

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

// resolveHost extracts host from OCI ref or returns default.
func resolveHost(ociRef *iopkg.OCIReference) string {
	if ociRef != nil && ociRef.Host != "" {
		return ociRef.Host
	}

	return "localhost"
}

// resolvePort determines port from OCI ref, config, or returns 0 for auto-detection.
func resolvePort(cfg *v1alpha1.Cluster, ociRef *iopkg.OCIReference) int32 {
	if ociRef != nil && ociRef.Port > 0 {
		return ociRef.Port
	}

	localRegistryEnabled := cfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled
	if !localRegistryEnabled {
		return 0 // Will trigger auto-detection
	}

	if cfg.Spec.Cluster.LocalRegistryOpts.HostPort > 0 {
		return cfg.Spec.Cluster.LocalRegistryOpts.HostPort
	}

	return v1alpha1.DefaultLocalRegistryPort
}

// resolveRepository determines repository name from OCI ref or source directory.
func resolveRepository(ociRef *iopkg.OCIReference, sourceDir string) string {
	if ociRef != nil && ociRef.FullRepository() != "" {
		return ociRef.FullRepository()
	}

	return registry.SanitizeRepoName(sourceDir)
}

// resolveRef determines the artifact ref/tag from OCI ref or default.
func resolveRef(ociRef *iopkg.OCIReference) string {
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
	outputTimer timer.Timer,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "detecting push environment",
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
		Content: "detected registry port %d, GitOps engine %s",
		Args:    []any{params.Port, params.GitOpsEngine},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
