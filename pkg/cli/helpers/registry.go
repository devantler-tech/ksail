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

// hostPortInfo holds parsed host and port information.
type hostPortInfo struct {
	host string
	port int32
}

// parseHostPort parses a host:port string into separate host and port values.
// If the port is not a valid number, it's treated as part of the host (e.g., ghcr.io).
func parseHostPort(hostPort string) hostPortInfo {
	colonIdx := strings.LastIndex(hostPort, ":")
	if colonIdx == -1 {
		return hostPortInfo{host: hostPort}
	}

	host := hostPort[:colonIdx]
	portStr := hostPort[colonIdx+1:]

	var port int

	_, err := fmt.Sscanf(portStr, "%d", &port)
	if err == nil && port > 0 {
		return hostPortInfo{
			host: host,
			port: int32(port), //nolint:gosec // port is validated
		}
	}

	// Not a valid port number, treat entire string as host (e.g., ghcr.io)
	return hostPortInfo{host: hostPort}
}

// isExternalHost checks if a host is external (not localhost).
func isExternalHost(host string) bool {
	return !strings.HasPrefix(host, "localhost") &&
		!strings.HasPrefix(host, "127.0.0.1") &&
		!strings.HasSuffix(host, ".localhost")
}

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

	// Translate internal Docker hostnames to localhost with real port
	// This handles cases where GitOps resources use internal Docker network names
	// (e.g., "k3d-default-local-registry:5000") which aren't resolvable from the host
	translateErr := translateInternalHostname(ctx, info)
	if translateErr != nil {
		return nil, translateErr
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

	// Split host:port from path
	hostPort, path, _ := strings.Cut(url, "/")

	// Parse host and port using shared helper
	parsed := parseHostPort(hostPort)

	return &RegistryInfo{
		Host:       parsed.host,
		Port:       parsed.port,
		Repository: path,
		IsExternal: isExternalHost(parsed.host),
	}, nil
}

// isInternalDockerHostname checks if a hostname is an internal Docker network hostname
// (e.g., "k3d-default-local-registry", "kind-local-registry").
// These hostnames are only resolvable inside the Docker network, not from the host.
func isInternalDockerHostname(host string) bool {
	return strings.HasSuffix(host, "-"+registrypkg.LocalRegistryBaseName)
}

// translateInternalHostname translates an internal Docker hostname to localhost
// by looking up the container's host port.
// Returns the original info unchanged if:
// - The host is not an internal Docker hostname
// - The container cannot be found
// - The container's port cannot be determined.
func translateInternalHostname(ctx context.Context, info *RegistryInfo) error {
	if !isInternalDockerHostname(info.Host) {
		return nil
	}

	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		// Can't connect to Docker, leave as-is and let caller handle
		return nil
	}

	defer func() { _ = dockerClient.Close() }()

	registryManager, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		// Can't create manager, leave as-is
		return nil
	}

	// The internal hostname IS the container name
	containerName := info.Host

	running, err := registryManager.IsContainerRunning(ctx, containerName)
	if err != nil || !running {
		return nil
	}

	port, err := registryManager.GetContainerPort(
		ctx,
		containerName,
		dockerclient.DefaultRegistryPort,
	)
	if err != nil {
		// Can't get port, leave as-is
		return nil
	}

	// Successfully resolved - update info to use localhost
	info.Host = "localhost"
	info.Port = int32(port) //nolint:gosec // port validated by Docker API
	info.IsExternal = false

	return nil
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
	var info *RegistryInfo

	var err error

	if cfg != nil {
		info, err = resolveFromGitOpsWithEngine(ctx, cfg.Spec.Cluster.GitOpsEngine)
	} else {
		// No config, try both GitOps engines
		info, err = tryBothGitOpsEngines(ctx)
	}

	if err != nil {
		return nil, err
	}

	// Merge credentials from config if available and info has no credentials
	if info != nil && info.Username == "" && cfg != nil {
		mergeCredentialsFromConfig(info, cfg)
	}

	return info, nil
}

// mergeCredentialsFromConfig adds credentials from cluster config to registry info.
// This is used when the registry URL is auto-discovered from GitOps resources
// but credentials are configured in ksail.yaml.
func mergeCredentialsFromConfig(info *RegistryInfo, cfg *v1alpha1.Cluster) {
	reg := cfg.Spec.Cluster.LocalRegistry
	if !reg.HasCredentials() {
		return
	}

	username, password := reg.ResolveCredentials()
	info.Username = username
	info.Password = password
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

	// Parse host[:port][/path] using shared helper
	hostPort, path, _ := strings.Cut(hostAndPath, "/")
	parsed := parseHostPort(hostPort)

	info.Host = parsed.host
	info.Port = parsed.port
	info.Repository = path
	info.IsExternal = isExternalHost(parsed.host)

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
