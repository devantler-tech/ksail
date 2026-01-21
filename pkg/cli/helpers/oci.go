package helpers

import (
	"context"
	"fmt"
	"os"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
)

// PushOCIArtifactOptions contains parameters for pushing OCI artifacts.
type PushOCIArtifactOptions struct {
	// Context for the operation
	Context context.Context
	// ClusterConfig for resolving registry and gitops engine
	ClusterConfig *v1alpha1.Cluster
	// ClusterName for registry resolution
	ClusterName string
	// SourceDir is the directory containing manifests to push
	SourceDir string
	// Ref is the artifact tag/version (defaults to "dev")
	Ref string
	// Validate enables manifest validation before pushing
	Validate bool
	// SkipIfMissing if true, silently skip push if source directory doesn't exist
	SkipIfMissing bool
}

// PushOCIArtifact builds and pushes an OCI artifact to the configured registry.
// This function reuses the core logic from `ksail workload push` for consistency.
//
//nolint:funlen // Encapsulates complete push workflow
func PushOCIArtifact(opts PushOCIArtifactOptions) error {
	if opts.Context == nil {
		opts.Context = context.Background()
	}

	// Resolve registry using priority-based detection
	registryInfo, err := ResolveRegistry(opts.Context, ResolveRegistryOptions{
		ClusterConfig: opts.ClusterConfig,
		ClusterName:   opts.ClusterName,
	})
	if err != nil {
		return fmt.Errorf("resolve registry: %w", err)
	}

	// Determine source directory
	sourceDir := opts.SourceDir
	if sourceDir == "" {
		if opts.ClusterConfig.Spec.Workload.SourceDirectory != "" {
			sourceDir = opts.ClusterConfig.Spec.Workload.SourceDirectory
		} else {
			sourceDir = v1alpha1.DefaultSourceDirectory
		}
	}

	// Check if source directory exists
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		if opts.SkipIfMissing {
			// Silently skip if configured to do so
			return nil
		}

		return fmt.Errorf("source directory does not exist: %s", sourceDir)
	}

	// Determine repository name from source directory if not set
	repository := registryInfo.Repository
	if repository == "" {
		repository = registry.SanitizeRepoName(sourceDir)
	}

	// Determine ref/tag
	ref := opts.Ref
	if ref == "" {
		if registryInfo.Tag != "" {
			ref = registryInfo.Tag
		} else {
			ref = registry.DefaultLocalArtifactTag
		}
	}

	// Determine GitOps engine
	gitOpsEngine := opts.ClusterConfig.Spec.Cluster.GitOpsEngine

	// Format registry endpoint
	var registryEndpoint string
	if registryInfo.Port > 0 {
		registryEndpoint = fmt.Sprintf("%s:%d", registryInfo.Host, registryInfo.Port)
	} else {
		registryEndpoint = registryInfo.Host
	}

	// Build and push the artifact
	builder := oci.NewWorkloadArtifactBuilder()

	_, err = builder.Build(opts.Context, oci.BuildOptions{
		Name:             repository,
		SourcePath:       sourceDir,
		RegistryEndpoint: registryEndpoint,
		Repository:       repository,
		Version:          ref,
		GitOpsEngine:     gitOpsEngine,
		Username:         registryInfo.Username,
		Password:         registryInfo.Password,
	})
	if err != nil {
		return fmt.Errorf("build and push oci artifact: %w", err)
	}

	return nil
}
