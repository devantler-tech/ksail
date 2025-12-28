package workload

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/spf13/cobra"
)

// NewPushCmd creates the workload push command.
//
//nolint:funlen,cyclop // Cobra command RunE functions typically combine setup, validation, and execution
func NewPushCmd(_ *runtime.Runtime) *cobra.Command {
	var validate bool

	cmd := &cobra.Command{
		Use:          "push [source-directory]",
		Short:        "Package and push an OCI artifact to the local registry",
		Long:         "Build and push local workloads as an OCI artifact to the local registry.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx, err := initCommandContext(cmd)
		if err != nil {
			return err
		}

		clusterCfg := ctx.ClusterCfg
		outputTimer := ctx.OutputTimer
		tmr := ctx.Timer

		localRegistryEnabled := clusterCfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled
		gitOpsEngineConfigured := clusterCfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineNone

		if !localRegistryEnabled || !gitOpsEngineConfigured {
			return errLocalRegistryRequired
		}

		// Determine the configured source directory for the repo name.
		// The repo name is always derived from the config's sourceDirectory,
		// ensuring artifacts are pushed to the repository Flux is watching.
		configSourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
		if configSourceDir == "" {
			configSourceDir = v1alpha1.DefaultSourceDirectory
		}

		repoName := registry.SanitizeRepoName(configSourceDir)

		// Use positional arg if provided to specify which directory to package,
		// otherwise fall back to the configured source directory.
		var sourceDir string
		if len(args) > 0 {
			sourceDir = args[0]
		} else {
			sourceDir = configSourceDir
		}

		artifactVersion := registry.DefaultLocalArtifactTag

		registryPort := clusterCfg.Spec.Cluster.LocalRegistryOpts.HostPort
		if registryPort == 0 {
			registryPort = v1alpha1.DefaultLocalRegistryPort
		}

		builder := oci.NewWorkloadArtifactBuilder()

		cmd.Println()
		notify.WriteMessage(notify.Message{
			Type:    notify.TitleType,
			Emoji:   "📦",
			Content: "Build and Push OCI Artifact...",
			Writer:  cmd.OutOrStdout(),
		})

		tmr.NewStage()

		// Validate if flag is set or config option is enabled
		shouldValidate := validate || clusterCfg.Spec.Workload.ValidateOnPush
		if shouldValidate {
			notify.WriteMessage(notify.Message{
				Type:    notify.ActivityType,
				Content: "validating manifests",
				Timer:   outputTimer,
				Writer:  cmd.OutOrStdout(),
			})

			err = runValidateCmd(
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
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "building oci artifact",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "pushing oci artifact",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		_, err = builder.Build(cmd.Context(), oci.BuildOptions{
			Name:             repoName,
			SourcePath:       sourceDir,
			RegistryEndpoint: fmt.Sprintf("localhost:%d", registryPort),
			Repository:       repoName,
			Version:          artifactVersion,
			GitOpsEngine:     clusterCfg.Spec.Cluster.GitOpsEngine,
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

	cmd.Flags().BoolVar(&validate, "validate", false, "Validate manifests before pushing")

	return cmd
}
