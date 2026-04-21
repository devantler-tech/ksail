package registryresolver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

// ErrExternalRegistryCredentialsIncomplete is returned when an external registry
// has a username but no password (e.g. GITHUB_ACTOR set but GITHUB_TOKEN missing).
var ErrExternalRegistryCredentialsIncomplete = errors.New(
	"external registry credentials are incomplete: username is set but password is empty\n" +
		"  - ensure the token environment variable (e.g. GITHUB_TOKEN) is exported in the current environment\n" +
		"  - configure the external registry in your cluster config (spec.cluster.localRegistry.registry in ksail.yaml),\n" +
		"    for example via: ksail cluster init --local-registry 'user:token@host/repo'",
)

// External-registry push retry configuration.
// GHCR and similar external registries can experience transient blips
// (rate limits, 5xx errors, redirect storms) that outlast the low-level
// push retry in pkg/client/oci. This higher-level retry wraps the entire
// build+push cycle with a longer back-off window.
//
// The default* values capture the production defaults, while the package-level
// variables are used by the implementation and may be overridden in tests
// to avoid long wall-clock delays.
//
//nolint:gochecknoglobals // package-level vars allow test overrides via export_test.go
var (
	externalPushMaxAttempts   = 5
	externalPushRetryBaseWait = 5 * time.Second
	externalPushRetryMaxWait  = 30 * time.Second
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
// For external registries (e.g. GHCR) the entire build+push cycle is wrapped
// with higher-level retry logic to tolerate transient blips.
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

	pushFn := func() (*PushOCIArtifactResult, error) {
		return executePush(ctx, opts, registryInfo, sourceDir, exists)
	}

	// For external registries (GHCR, etc.) wrap with higher-level retry
	if registryInfo.IsExternal {
		return retryExternalPush(ctx, pushFn)
	}

	return pushFn()
}

// executePush performs the actual build and push of the OCI artifact.
func executePush(
	ctx context.Context,
	opts PushOCIArtifactOptions,
	registryInfo *Info,
	sourceDir string,
	exists bool,
) (*PushOCIArtifactResult, error) {
	builder := oci.NewWorkloadArtifactBuilder()

	// Fail fast when external registry credentials are partially configured.
	// A username with an empty password (e.g. GITHUB_ACTOR set but GITHUB_TOKEN
	// unset) causes the OCI push to receive a write-less anonymous token from GHCR,
	// which surfaces as a confusing 403 rather than a clear auth error.
	if registryInfo.IsExternal && registryInfo.Username != "" && registryInfo.Password == "" {
		return nil, ErrExternalRegistryCredentialsIncomplete
	}

	if !exists {
		// Push an empty OCI artifact when source directory doesn't exist
		emptyOpts := buildEmptyPushOptions(registryInfo, opts, sourceDir)

		_, err := builder.BuildEmpty(ctx, emptyOpts)
		if err != nil {
			return nil, fmt.Errorf("build and push empty oci artifact: %w", err)
		}

		return &PushOCIArtifactResult{Pushed: true, Empty: true}, nil
	}

	// Build options and push
	buildOpts := buildPushOptions(registryInfo, opts, sourceDir)

	_, err := builder.Build(ctx, buildOpts)
	if err != nil {
		return nil, fmt.Errorf("build and push oci artifact: %w", err)
	}

	return &PushOCIArtifactResult{Pushed: true, Empty: false}, nil
}

// retryExternalPush wraps a push operation with retry logic for external registries.
// GHCR and similar registries can experience transient blips (rate limits,
// 5xx errors, redirect storms) that outlast the low-level push retry in
// pkg/client/oci. This function provides a higher-level retry with longer
// back-off windows.
// Returns the result of the first successful attempt, or the last error
// if all attempts are exhausted.
func retryExternalPush(
	ctx context.Context,
	fn func() (*PushOCIArtifactResult, error),
) (*PushOCIArtifactResult, error) {
	var lastErr error

	for attempt := 1; attempt <= externalPushMaxAttempts; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		if !netretry.IsRetryable(lastErr) || attempt == externalPushMaxAttempts {
			break
		}

		delay := netretry.ExponentialDelay(
			attempt, externalPushRetryBaseWait, externalPushRetryMaxWait,
		)

		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return nil, fmt.Errorf("push to external registry cancelled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	if !netretry.IsRetryable(lastErr) {
		return nil, fmt.Errorf("push to external registry failed (non-retryable): %w", lastErr)
	}

	return nil, fmt.Errorf(
		"push to external registry failed after %d attempts: %w",
		externalPushMaxAttempts, lastErr,
	)
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

// ociPushParams holds the common parameters for building OCI push options.
type ociPushParams struct {
	repository       string
	registryEndpoint string
	version          string
	gitOpsEngine     v1alpha1.GitOpsEngine
	username         string
	password         string
}

// resolveOCIPushParams extracts common parameters from registry info and push options.
func resolveOCIPushParams(
	registryInfo *Info,
	opts PushOCIArtifactOptions,
	sourceDir string,
) ociPushParams {
	repository := registryInfo.Repository
	if repository == "" {
		repository = registry.SanitizeRepoName(sourceDir)
	}

	ref := opts.Ref
	if ref == "" {
		switch {
		case opts.ClusterConfig.Spec.Workload.Tag != "":
			ref = opts.ClusterConfig.Spec.Workload.Tag
		case registryInfo.Tag != "":
			ref = registryInfo.Tag
		default:
			ref = registry.DefaultLocalArtifactTag
		}
	}

	var registryEndpoint string
	if registryInfo.Port > 0 {
		registryEndpoint = fmt.Sprintf("%s:%d", registryInfo.Host, registryInfo.Port)
	} else {
		registryEndpoint = registryInfo.Host
	}

	return ociPushParams{
		repository:       repository,
		registryEndpoint: registryEndpoint,
		version:          ref,
		gitOpsEngine:     opts.ClusterConfig.Spec.Cluster.GitOpsEngine,
		username:         registryInfo.Username,
		password:         registryInfo.Password,
	}
}

// buildPushOptions creates the OCI build options from registry info and push options.
func buildPushOptions(
	registryInfo *Info,
	opts PushOCIArtifactOptions,
	sourceDir string,
) oci.BuildOptions {
	params := resolveOCIPushParams(registryInfo, opts, sourceDir)

	return oci.BuildOptions{
		Name:             params.repository,
		SourcePath:       sourceDir,
		RegistryEndpoint: params.registryEndpoint,
		Repository:       params.repository,
		Version:          params.version,
		GitOpsEngine:     params.gitOpsEngine,
		Username:         params.username,
		Password:         params.password,
	}
}

// buildEmptyPushOptions creates the OCI empty build options from registry info and push options.
func buildEmptyPushOptions(
	registryInfo *Info,
	opts PushOCIArtifactOptions,
	sourceDir string,
) oci.EmptyBuildOptions {
	params := resolveOCIPushParams(registryInfo, opts, sourceDir)

	return oci.EmptyBuildOptions{
		Name:             params.repository,
		RegistryEndpoint: params.registryEndpoint,
		Repository:       params.repository,
		Version:          params.version,
		GitOpsEngine:     params.gitOpsEngine,
		Username:         params.username,
		Password:         params.password,
	}
}
