package registryresolver

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
	"github.com/spf13/viper"
)

// Info contains detected registry information.
type Info struct {
	Host       string
	Port       int32
	Repository string
	Tag        string
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

// Registry secret constants for GitOps engines.
const (
	// ViperRegistryKey is the viper key for the registry flag/env var.
	ViperRegistryKey = "registry"
	// credentialParts is the expected number of parts when splitting username:password.
	credentialParts = 2

	// Flux stores registry credentials as a Docker config secret.
	fluxSecretNamespace = "flux-system"
	fluxSecretName      = "ksail-registry-credentials" //nolint:gosec // Not a credential, just a secret name

	// ArgoCD stores registry credentials in a repository secret with plain username/password fields.
	argoCDSecretNamespace = "argocd"
	argoCDSecretName      = "ksail-local-registry-repo" //nolint:gosec // Not a credential, just a secret name
)

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

	port64, err := strconv.ParseInt(portStr, 10, 32)
	if err == nil && port64 > 0 {
		return hostPortInfo{
			host: host,
			port: int32(port64),
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

// parseOCIURL parses an OCI URL like "oci://host:port/repo" or "oci://host/repo" into RegistryInfo.
func parseOCIURL(url string) (*Info, error) {
	// Remove oci:// prefix
	url = strings.TrimPrefix(url, "oci://")

	if url == "" {
		return nil, ErrEmptyOCIURL
	}

	// Split host:port from path
	hostPort, path, _ := strings.Cut(url, "/")

	// Parse host and port using shared helper
	parsed := parseHostPort(hostPort)

	return &Info{
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
func translateInternalHostname(ctx context.Context, info *Info) error {
	if !isInternalDockerHostname(info.Host) {
		return nil
	}

	resources, err := dockerclient.NewResources()
	if err != nil {
		// Can't connect to Docker, leave as-is and let caller handle
		return nil
	}

	defer resources.Close()

	registryManager := resources.RegistryManager

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
func ResolveRegistry(ctx context.Context, opts ResolveRegistryOptions) (*Info, error) {
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

func resolveFromViper(v *viper.Viper) (*Info, error) {
	if v == nil {
		return nil, ErrViperNil
	}

	return DetectRegistryFromViper(v)
}

func resolveFromConfig(cfg *v1alpha1.Cluster) (*Info, error) {
	if cfg == nil {
		return nil, ErrLocalRegistryNotConfigured
	}

	return DetectRegistryFromConfig(cfg)
}

func resolveFromGitOps(ctx context.Context, cfg *v1alpha1.Cluster) (*Info, error) {
	var info *Info

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

	// Merge credentials from cluster secrets if needed
	if info != nil && info.Username == "" && info.IsExternal {
		mergeCredentialsFromClusterSecrets(ctx, info)
	}

	return info, nil
}

func resolveFromGitOpsWithEngine(
	ctx context.Context,
	engine v1alpha1.GitOpsEngine,
) (*Info, error) {
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

func tryBothGitOpsEngines(ctx context.Context) (*Info, error) {
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
) (*Info, error) {
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
func parseRegistryFlag(registryFlag string) *Info {
	info := &Info{}

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
