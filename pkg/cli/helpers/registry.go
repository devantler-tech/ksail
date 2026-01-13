package helpers

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// ErrNoRegistryFound is returned when no registry can be detected from any source.
var ErrNoRegistryFound = errors.New(
	"unable to detect registry; provide --registry flag, set KSAIL_REGISTRY env, or configure local-registry in ksail.yaml",
)

// ViperRegistryKey is the viper key for the registry flag/env var.
const ViperRegistryKey = "registry"

// DetectRegistryFromViper checks for registry configuration from a Viper instance.
// This handles both --registry flag and KSAIL_REGISTRY environment variable since
// Viper binds them together.
func DetectRegistryFromViper(v *viper.Viper) (*RegistryInfo, error) {
	if v == nil {
		return nil, errors.New("viper instance is nil")
	}

	registry := v.GetString(ViperRegistryKey)
	if registry == "" {
		return nil, errors.New("registry not set via flag or environment variable")
	}

	info, err := parseRegistryFlag(registry)
	if err != nil {
		return nil, err
	}

	// Determine source based on whether it was from flag or env
	// Viper doesn't expose which source was used, so we check if the flag was explicitly set
	info.Source = "flag/env:registry"

	return info, nil
}

// DetectRegistryFromConfig extracts registry info from ksail cluster configuration.
func DetectRegistryFromConfig(cfg *v1alpha1.Cluster) (*RegistryInfo, error) {
	reg := cfg.Spec.Cluster.LocalRegistry
	if !reg.Enabled() {
		return nil, errors.New("local registry not configured in ksail.yaml")
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

// DetectRegistryFromFlux tries to get registry URL from FluxInstance sync configuration.
func DetectRegistryFromFlux(ctx context.Context) (*RegistryInfo, error) {
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

	// FluxInstance GVR
	gvr := schema.GroupVersionResource{
		Group:    "fluxcd.controlplane.io",
		Version:  "v1",
		Resource: "fluxinstances",
	}

	// Try to get the "flux" FluxInstance in flux-system namespace
	obj, err := dynClient.Resource(gvr).
		Namespace("flux-system").
		Get(ctx, "flux", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get FluxInstance: %w", err)
	}

	// Extract sync.url from spec
	url, found, err := unstructured.NestedString(obj.Object, "spec", "sync", "url")
	if err != nil || !found || url == "" {
		return nil, errors.New("FluxInstance has no sync.url configured")
	}

	info, err := parseOCIURL(url)
	if err != nil {
		return nil, err
	}

	info.Source = "cluster:FluxInstance"

	return info, nil
}

// DetectRegistryFromArgoCD tries to get registry URL from ArgoCD Application source.
func DetectRegistryFromArgoCD(ctx context.Context) (*RegistryInfo, error) {
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

	// ArgoCD Application GVR
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	// Try to get the "ksail" Application in argocd namespace
	obj, err := dynClient.Resource(gvr).Namespace("argocd").Get(ctx, "ksail", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get ArgoCD Application: %w", err)
	}

	// Extract source.repoURL from spec
	repoURL, found, err := unstructured.NestedString(obj.Object, "spec", "source", "repoURL")
	if err != nil || !found || repoURL == "" {
		return nil, errors.New("ArgoCD Application has no source.repoURL configured")
	}

	info, err := parseOCIURL(repoURL)
	if err != nil {
		return nil, err
	}

	info.Source = "cluster:ArgoCD"

	return info, nil
}

// DetectRegistryFromDocker tries to find a local registry Docker container.
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
		return nil, errors.New("empty OCI URL")
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
// 1. CLI flag or env var via Viper (--registry / KSAIL_REGISTRY)
// 2. Config file (ksail.yaml localRegistry)
// 3. Cluster GitOps resources (FluxInstance or ArgoCD Application)
// 4. Docker containers (matching cluster name)
// 5. Error (no registry found)
//
//nolint:cyclop // Resolution requires checking multiple sources in priority order
func ResolveRegistry(ctx context.Context, opts ResolveRegistryOptions) (*RegistryInfo, error) {
	// Priority 1: CLI flag or env var via Viper (--registry / KSAIL_REGISTRY)
	if opts.Viper != nil {
		if info, err := DetectRegistryFromViper(opts.Viper); err == nil {
			return info, nil
		}
	}

	// Priority 2: Config file (ksail.yaml localRegistry)
	if opts.ClusterConfig != nil {
		if info, err := DetectRegistryFromConfig(opts.ClusterConfig); err == nil {
			return info, nil
		}
	}

	// Priority 3: Cluster GitOps resources (FluxInstance or ArgoCD Application)
	// Try to detect GitOps engine first
	if opts.ClusterConfig != nil {
		switch opts.ClusterConfig.Spec.Cluster.GitOpsEngine {
		case v1alpha1.GitOpsEngineFlux:
			if info, err := DetectRegistryFromFlux(ctx); err == nil {
				return info, nil
			}
		case v1alpha1.GitOpsEngineArgoCD:
			if info, err := DetectRegistryFromArgoCD(ctx); err == nil {
				return info, nil
			}
		default:
			// Try both GitOps engines
			if info, err := DetectRegistryFromFlux(ctx); err == nil {
				return info, nil
			}

			if info, err := DetectRegistryFromArgoCD(ctx); err == nil {
				return info, nil
			}
		}
	} else {
		// No config, try both GitOps engines
		if info, err := DetectRegistryFromFlux(ctx); err == nil {
			return info, nil
		}

		if info, err := DetectRegistryFromArgoCD(ctx); err == nil {
			return info, nil
		}
	}

	// Priority 4: Docker containers (matching cluster name)
	clusterName := opts.ClusterName
	if clusterName == "" && opts.ClusterConfig != nil {
		clusterName = opts.ClusterConfig.Spec.Cluster.Connection.Context
	}

	if clusterName != "" {
		if info, err := DetectRegistryFromDocker(ctx, clusterName); err == nil {
			return info, nil
		}
	}

	// Priority 5: Error (no registry found)
	return nil, ErrNoRegistryFound
}

// parseRegistryFlag parses the --registry flag value.
// Format: [user:pass@]host[:port][/path].
func parseRegistryFlag(registryFlag string) (*RegistryInfo, error) {
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
		if _, err := fmt.Sscanf(portStr, "%d", &port); err == nil && port > 0 {
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

	return info, nil
}
