package helpers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	registrypkg "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// RegistryInfo contains detected registry information.
type RegistryInfo struct {
	Host       string
	Port       int32
	Repository string
	Username   string
	Password   string
	// IsExternal is true if the registry is external (e.g., ghcr.io) vs local Docker registry
	IsExternal bool
	// Source describes where the registry info was detected from
	Source string
}

// Static errors for registry detection.
var (
	// ErrNoRegistryFound is returned when no registry can be detected from any source.
	ErrNoRegistryFound = errors.New(
		"unable to detect registry; provide --registry flag, set KSAIL_REGISTRY, or configure local-registry",
	)
	// ErrViperNil is returned when a nil viper instance is provided.
	ErrViperNil = errors.New("viper instance is nil")
	// ErrRegistryNotSet is returned when registry is not set via flag or environment.
	ErrRegistryNotSet = errors.New("registry not set via flag or environment variable")
	// ErrLocalRegistryNotConfigured is returned when local registry is not in config.
	ErrLocalRegistryNotConfigured = errors.New("local registry not configured in ksail.yaml")
	// ErrFluxNoSyncURL is returned when FluxInstance has no sync.url.
	ErrFluxNoSyncURL = errors.New("FluxInstance has no sync.url configured")
	// ErrArgoCDNoRepoURL is returned when ArgoCD Application has no source.repoURL.
	ErrArgoCDNoRepoURL = errors.New("ArgoCD Application has no source.repoURL configured")
	// ErrEmptyOCIURL is returned when an empty OCI URL is provided.
	ErrEmptyOCIURL = errors.New("empty OCI URL")
)

// ViperRegistryKey is the viper key for the registry flag/env var.
const ViperRegistryKey = "registry"

// DetectRegistryFromViper checks for registry configuration from a Viper instance.
// This handles both --registry flag and KSAIL_REGISTRY environment variable since
// Viper binds them together.
func DetectRegistryFromViper(v *viper.Viper) (*RegistryInfo, error) {
	if v == nil {
		return nil, ErrViperNil
	}

	registry := v.GetString(ViperRegistryKey)
	if registry == "" {
		return nil, ErrRegistryNotSet
	}

	info := parseRegistryFlag(registry)
	info.Source = "flag/env:registry"

	return info, nil
}

// DetectRegistryFromConfig extracts registry info from ksail cluster configuration.
func DetectRegistryFromConfig(cfg *v1alpha1.Cluster) (*RegistryInfo, error) {
	reg := cfg.Spec.Cluster.LocalRegistry
	if !reg.Enabled() {
		return nil, ErrLocalRegistryNotConfigured
	}

	info := &RegistryInfo{
		Host:       reg.ResolvedHost(),
		Port:       reg.ResolvedPort(),
		Repository: reg.ResolvedPath(),
		IsExternal: reg.IsExternal(),
		Source:     "config:ksail.yaml",
	}

	// Resolve credentials with env var expansion
	username, password := reg.ResolveCredentials()
	info.Username = username
	info.Password = password

	return info, nil
}

// gitOpsResourceSpec defines how to fetch a GitOps resource URL.
type gitOpsResourceSpec struct {
	gvr        schema.GroupVersionResource
	namespace  string
	name       string
	urlPath    []string
	errNoURL   error
	sourceName string
}

// detectRegistryFromGitOps fetches registry info from a GitOps resource.
func detectRegistryFromGitOps(ctx context.Context, spec gitOpsResourceSpec) (*RegistryInfo, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	obj, err := dynClient.Resource(spec.gvr).
		Namespace(spec.namespace).
		Get(ctx, spec.name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", spec.sourceName, err)
	}

	url, found, err := unstructured.NestedString(obj.Object, spec.urlPath...)
	if err != nil || !found || url == "" {
		return nil, spec.errNoURL
	}

	info, err := parseOCIURL(url)
	if err != nil {
		return nil, err
	}

	info.Source = "cluster:" + spec.sourceName

	return info, nil
}

// DetectRegistryFromFlux tries to get registry URL from FluxInstance sync configuration.
func DetectRegistryFromFlux(ctx context.Context) (*RegistryInfo, error) {
	return detectRegistryFromGitOps(ctx, gitOpsResourceSpec{
		gvr: schema.GroupVersionResource{
			Group:    "fluxcd.controlplane.io",
			Version:  "v1",
			Resource: "fluxinstances",
		},
		namespace:  "flux-system",
		name:       "flux",
		urlPath:    []string{"spec", "sync", "url"},
		errNoURL:   ErrFluxNoSyncURL,
		sourceName: "FluxInstance",
	})
}

// DetectRegistryFromArgoCD tries to get registry URL from ArgoCD Application source.
func DetectRegistryFromArgoCD(ctx context.Context) (*RegistryInfo, error) {
	return detectRegistryFromGitOps(ctx, gitOpsResourceSpec{
		gvr: schema.GroupVersionResource{
			Group:    "argoproj.io",
			Version:  "v1alpha1",
			Resource: "applications",
		},
		namespace:  "argocd",
		name:       "ksail",
		urlPath:    []string{"spec", "source", "repoURL"},
		errNoURL:   ErrArgoCDNoRepoURL,
		sourceName: "ArgoCD",
	})
}

// DetectRegistryFromDocker tries to find a local registry Docker container.
//
//nolint:cyclop // Detection logic requires multiple fallback paths
func DetectRegistryFromDocker(ctx context.Context, clusterName string) (*RegistryInfo, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	registryManager, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		return nil, fmt.Errorf("create registry manager: %w", err)
	}

	// Try cluster-specific registry first (e.g., "mycluster-local-registry")
	registryName := clusterName + "-" + registrypkg.LocalRegistryBaseName

	running, err := registryManager.IsContainerRunning(ctx, registryName)
	if err == nil && running {
		port, portErr := registryManager.GetContainerPort(
			ctx,
			registryName,
			dockerclient.DefaultRegistryPort,
		)
		if portErr == nil {
			return &RegistryInfo{
				Host:       "localhost",
				Port:       int32(port), //nolint:gosec // port validated by Docker API
				Repository: "",          // Will be derived from source directory
				IsExternal: false,
				Source:     "docker:" + registryName,
			}, nil
		}
	}

	// Fallback: search for any container ending with "-local-registry"
	registrySuffix := "-" + registrypkg.LocalRegistryBaseName

	foundName, err := registryManager.FindContainerBySuffix(ctx, registrySuffix)
	if err != nil || foundName == "" {
		return nil, ErrNoLocalRegistry
	}

	// Check if it's running
	running, err = registryManager.IsContainerRunning(ctx, foundName)
	if err != nil || !running {
		return nil, ErrNoLocalRegistry
	}

	port, err := registryManager.GetContainerPort(ctx, foundName, dockerclient.DefaultRegistryPort)
	if err != nil {
		return nil, fmt.Errorf("get registry port: %w", err)
	}

	return &RegistryInfo{
		Host:       "localhost",
		Port:       int32(port), //nolint:gosec // port validated by Docker API
		Repository: "",          // Will be derived from source directory
		IsExternal: false,
		Source:     "docker:" + foundName,
	}, nil
}

// parseOCIURL parses an OCI URL like "oci://host:port/repo" or "oci://host/repo" into RegistryInfo.
func parseOCIURL(url string) (*RegistryInfo, error) {
	// Remove oci:// prefix
	url = strings.TrimPrefix(url, "oci://")

	if url == "" {
		return nil, ErrEmptyOCIURL
	}

	info := &RegistryInfo{}

	// Split host:port from path
	before, after, ok := strings.Cut(url, "/")

	var hostPort, path string

	if !ok {
		hostPort = url
		path = ""
	} else {
		hostPort = before
		path = after
	}

	// Parse host and port
	colonIdx := strings.LastIndex(hostPort, ":")
	if colonIdx != -1 {
		info.Host = hostPort[:colonIdx]
		portStr := hostPort[colonIdx+1:]

		var port int

		_, err := fmt.Sscanf(portStr, "%d", &port)

		if err == nil && port > 0 {
			info.Port = int32(port) //nolint:gosec // port is validated
		} else {
			// Not a port number, likely part of the host (e.g., ghcr.io)
			info.Host = hostPort
		}
	} else {
		info.Host = hostPort
	}

	info.Repository = path

	// Determine if external (not localhost/127.0.0.1)
	info.IsExternal = !strings.HasPrefix(info.Host, "localhost") &&
		!strings.HasPrefix(info.Host, "127.0.0.1") &&
		!strings.HasSuffix(info.Host, ".localhost")

	return info, nil
}

// ResolveRegistryOptions contains configuration for registry resolution.
type ResolveRegistryOptions struct {
	// Viper is the viper instance with bound flags and env vars.
	// If provided, it's used to resolve registry from --registry flag or KSAIL_REGISTRY env var.
	Viper *viper.Viper
	// ClusterConfig is the parsed ksail.yaml configuration
	ClusterConfig *v1alpha1.Cluster
	// ClusterName is the name of the cluster (used for Docker container lookup)
	ClusterName string
}

// ResolveRegistry resolves registry configuration using a priority-based approach.
// Priority order:
// 1. CLI flag or env var via Viper (--registry / KSAIL_REGISTRY).
// 2. Config file (ksail.yaml localRegistry).
// 3. Cluster GitOps resources (FluxInstance or ArgoCD Application).
// 4. Docker containers (matching cluster name).
// 5. Error (no registry found).
func ResolveRegistry(ctx context.Context, opts ResolveRegistryOptions) (*RegistryInfo, error) {
	// Priority 1: CLI flag or env var via Viper (--registry / KSAIL_REGISTRY)
	info, err := resolveFromViper(opts.Viper)
	if err == nil {
		return info, nil
	}

	// Priority 2: Config file (ksail.yaml localRegistry)
	info, err = resolveFromConfig(opts.ClusterConfig)
	if err == nil {
		return info, nil
	}

	// Priority 3: Cluster GitOps resources (FluxInstance or ArgoCD Application)
	info, err = resolveFromGitOps(ctx, opts.ClusterConfig)
	if err == nil {
		return info, nil
	}

	// Priority 4: Docker containers (matching cluster name)
	info, err = resolveFromDocker(ctx, opts.ClusterName, opts.ClusterConfig)
	if err == nil {
		return info, nil
	}

	// Priority 5: Error (no registry found)
	return nil, ErrNoRegistryFound
}

func resolveFromViper(v *viper.Viper) (*RegistryInfo, error) {
	if v == nil {
		return nil, ErrViperNil
	}

	return DetectRegistryFromViper(v)
}

func resolveFromConfig(cfg *v1alpha1.Cluster) (*RegistryInfo, error) {
	if cfg == nil {
		return nil, ErrLocalRegistryNotConfigured
	}

	return DetectRegistryFromConfig(cfg)
}

func resolveFromGitOps(ctx context.Context, cfg *v1alpha1.Cluster) (*RegistryInfo, error) {
	if cfg != nil {
		return resolveFromGitOpsWithEngine(ctx, cfg.Spec.Cluster.GitOpsEngine)
	}

	// No config, try both GitOps engines
	return tryBothGitOpsEngines(ctx)
}

func resolveFromGitOpsWithEngine(
	ctx context.Context,
	engine v1alpha1.GitOpsEngine,
) (*RegistryInfo, error) {
	switch engine {
	case v1alpha1.GitOpsEngineFlux:
		return DetectRegistryFromFlux(ctx)
	case v1alpha1.GitOpsEngineArgoCD:
		return DetectRegistryFromArgoCD(ctx)
	case v1alpha1.GitOpsEngineNone:
		return tryBothGitOpsEngines(ctx)
	default:
		return tryBothGitOpsEngines(ctx)
	}
}

func tryBothGitOpsEngines(ctx context.Context) (*RegistryInfo, error) {
	info, err := DetectRegistryFromFlux(ctx)
	if err == nil {
		return info, nil
	}

	return DetectRegistryFromArgoCD(ctx)
}

func resolveFromDocker(
	ctx context.Context,
	clusterName string,
	cfg *v1alpha1.Cluster,
) (*RegistryInfo, error) {
	name := clusterName
	if name == "" && cfg != nil {
		name = cfg.Spec.Cluster.Connection.Context
	}

	if name == "" {
		return nil, ErrNoLocalRegistry
	}

	return DetectRegistryFromDocker(ctx, name)
}

// parseRegistryFlag parses the --registry flag value.
// Format: [user:pass@]host[:port][/path].
func parseRegistryFlag(registryFlag string) *RegistryInfo {
	info := &RegistryInfo{}

	// Check for credentials (user:pass@)
	atIdx := strings.LastIndex(registryFlag, "@")

	var hostAndPath string

	if atIdx != -1 {
		credentials := registryFlag[:atIdx]
		hostAndPath = registryFlag[atIdx+1:]

		before, after, ok := strings.Cut(credentials, ":")
		if ok {
			info.Username = os.ExpandEnv(before)
			info.Password = os.ExpandEnv(after)
		} else {
			info.Username = os.ExpandEnv(credentials)
		}
	} else {
		hostAndPath = registryFlag
	}

	// Now parse host[:port][/path]
	before, after, ok := strings.Cut(hostAndPath, "/")

	var hostPort, path string

	if ok {
		hostPort = before
		path = after
	} else {
		hostPort = hostAndPath
	}

	// Parse host and port
	colonIdx := strings.LastIndex(hostPort, ":")
	if colonIdx != -1 {
		info.Host = hostPort[:colonIdx]
		portStr := hostPort[colonIdx+1:]

		var port int

		_, err := fmt.Sscanf(portStr, "%d", &port)
		if err == nil && port > 0 {
			info.Port = int32(port) //nolint:gosec // port is validated
		} else {
			info.Host = hostPort
		}
	} else {
		info.Host = hostPort
	}

	info.Repository = path
	info.IsExternal = !strings.HasPrefix(info.Host, "localhost") &&
		!strings.HasPrefix(info.Host, "127.0.0.1") &&
		!strings.HasSuffix(info.Host, ".localhost")

	return info
}

// FormatRegistryURL formats a registry URL using net.JoinHostPort for proper host:port handling.
func FormatRegistryURL(host string, port int32, repository string) string {
	if port > 0 {
		hostPort := net.JoinHostPort(host, strconv.Itoa(int(port)))

		return fmt.Sprintf("oci://%s/%s", hostPort, repository)
	}

	return fmt.Sprintf("oci://%s/%s", host, repository)
}
