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
}

// PushOCIArtifactResult contains the result of a push operation.
type PushOCIArtifactResult struct {
	// Pushed indicates if an artifact was actually pushed.
	Pushed bool
	// Empty indicates if an empty artifact was pushed (source directory was missing).
	Empty bool
}

// PushOCIArtifact builds and pushes an OCI artifact to the configured registry.
// This function reuses the core logic from `ksail workload push` for consistency.
// The ctx parameter must be non-nil.
// If the source directory doesn't exist, an empty OCI artifact is pushed instead.
// Returns a result indicating whether an artifact was actually pushed.
func PushOCIArtifact(
	ctx context.Context,
	opts PushOCIArtifactOptions,
) (*PushOCIArtifactResult, error) {
	// Resolve registry using priority-based detection
	registryInfo, err := ResolveRegistry(ctx, ResolveRegistryOptions{
		ClusterConfig: opts.ClusterConfig,
		ClusterName:   opts.ClusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve registry: %w", err)
	}

	// Determine source directory
	sourceDir := resolveSourceDirectory(opts.SourceDir, opts.ClusterConfig)

	// Check if source directory exists
	exists, err := checkSourceDirectoryExists(sourceDir)
	if err != nil {
		return nil, err
	}

	builder := oci.NewWorkloadArtifactBuilder()

	if !exists {
		// Push an empty OCI artifact when source directory doesn't exist
		emptyOpts := buildEmptyPushOptions(registryInfo, opts, sourceDir)

		_, err = builder.BuildEmpty(ctx, emptyOpts)
		if err != nil {
			return nil, fmt.Errorf("build and push empty oci artifact: %w", err)
		}

		return &PushOCIArtifactResult{Pushed: true, Empty: true}, nil
	}

	// Build options and push
	buildOpts := buildPushOptions(registryInfo, opts, sourceDir)

	_, err = builder.Build(ctx, buildOpts)
	if err != nil {
		return nil, fmt.Errorf("build and push oci artifact: %w", err)
	}

	return &PushOCIArtifactResult{Pushed: true, Empty: false}, nil
}

// resolveSourceDirectory determines the source directory from options or config.
func resolveSourceDirectory(sourceDir string, clusterCfg *v1alpha1.Cluster) string {
	if sourceDir != "" {
		return sourceDir
	}

	if clusterCfg.Spec.Workload.SourceDirectory != "" {
		return clusterCfg.Spec.Workload.SourceDirectory
	}

	return v1alpha1.DefaultSourceDirectory
}

// checkSourceDirectoryExists checks if the source directory exists.
func checkSourceDirectoryExists(sourceDir string) (bool, error) {
	_, err := os.Stat(sourceDir)
	if os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("check source directory: %w", err)
	}

	return true, nil
}

// buildPushOptions creates the OCI build options from registry info and push options.
func buildPushOptions(
	registryInfo *RegistryInfo,
	opts PushOCIArtifactOptions,
	sourceDir string,
) oci.BuildOptions {
	repository := registryInfo.Repository
	if repository == "" {
		repository = registry.SanitizeRepoName(sourceDir)
	}

	ref := opts.Ref
	if ref == "" {
		if registryInfo.Tag != "" {
			ref = registryInfo.Tag
		} else {
			ref = registry.DefaultLocalArtifactTag
		}
	}

	var registryEndpoint string
	if registryInfo.Port > 0 {
		registryEndpoint = fmt.Sprintf("%s:%d", registryInfo.Host, registryInfo.Port)
	} else {
		registryEndpoint = registryInfo.Host
	}

	return oci.BuildOptions{
		Name:             repository,
		SourcePath:       sourceDir,
		RegistryEndpoint: registryEndpoint,
		Repository:       repository,
		Version:          ref,
		GitOpsEngine:     opts.ClusterConfig.Spec.Cluster.GitOpsEngine,
		Username:         registryInfo.Username,
		Password:         registryInfo.Password,
	}
}

// buildEmptyPushOptions creates the OCI empty build options from registry info and push options.
func buildEmptyPushOptions(
	registryInfo *RegistryInfo,
	opts PushOCIArtifactOptions,
	sourceDir string,
) oci.EmptyBuildOptions {
	repository := registryInfo.Repository
	if repository == "" {
		repository = registry.SanitizeRepoName(sourceDir)
	}

	ref := opts.Ref
	if ref == "" {
		if registryInfo.Tag != "" {
			ref = registryInfo.Tag
		} else {
			ref = registry.DefaultLocalArtifactTag
		}
	}

	var registryEndpoint string
	if registryInfo.Port > 0 {
		registryEndpoint = fmt.Sprintf("%s:%d", registryInfo.Host, registryInfo.Port)
	} else {
		registryEndpoint = registryInfo.Host
	}

	return oci.EmptyBuildOptions{
		Name:             repository,
		RegistryEndpoint: registryEndpoint,
		Repository:       repository,
		Version:          ref,
		GitOpsEngine:     opts.ClusterConfig.Spec.Cluster.GitOpsEngine,
		Username:         registryInfo.Username,
		Password:         registryInfo.Password,
	}
}
