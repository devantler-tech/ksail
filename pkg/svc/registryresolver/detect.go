package registryresolver

import (
	"context"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	registrypkg "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// DetectRegistryFromViper checks for registry configuration from a Viper instance.
// This handles both --registry flag and KSAIL_REGISTRY environment variable since
// Viper binds them together.
func DetectRegistryFromViper(v *viper.Viper) (*Info, error) {
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
func DetectRegistryFromConfig(cfg *v1alpha1.Cluster) (*Info, error) {
	reg := cfg.Spec.Cluster.LocalRegistry
	if !reg.Enabled() {
		return nil, ErrLocalRegistryNotConfigured
	}

	info := &Info{
		Host:       reg.ResolvedHost(),
		Port:       reg.ResolvedPort(),
		Repository: reg.ResolvedPath(),
		Tag:        reg.ResolvedTag(),
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
	refPath    []string // Optional: path to the ref/tag field
	errNoURL   error
	sourceName string
}

// fetchGitOpsResource retrieves the unstructured object from the cluster.
func fetchGitOpsResource(
	ctx context.Context,
	spec gitOpsResourceSpec,
) (*unstructured.Unstructured, error) {
	restConfig, err := k8s.GetRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("get REST config: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	obj, err := dynClient.Resource(spec.gvr).
		Namespace(spec.namespace).
		Get(ctx, spec.name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", spec.sourceName, err)
	}

	return obj, nil
}

// detectRegistryFromGitOps fetches registry info from a GitOps resource.
func detectRegistryFromGitOps(ctx context.Context, spec gitOpsResourceSpec) (*Info, error) {
	obj, err := fetchGitOpsResource(ctx, spec)
	if err != nil {
		return nil, err
	}

	url, found, err := unstructured.NestedString(obj.Object, spec.urlPath...)
	if err != nil || !found || url == "" {
		return nil, spec.errNoURL
	}

	info, err := parseOCIURL(url)
	if err != nil {
		return nil, err
	}

	// Extract ref/tag if refPath is specified
	if len(spec.refPath) > 0 {
		ref, refFound, _ := unstructured.NestedString(obj.Object, spec.refPath...)
		if refFound && ref != "" {
			info.Tag = ref
		}
	}

	// Translate internal Docker hostnames to localhost with real port
	translateErr := translateInternalHostname(ctx, info)
	if translateErr != nil {
		return nil, translateErr
	}

	info.Source = "cluster:" + spec.sourceName

	return info, nil
}

// DetectRegistryFromFlux tries to get registry URL from FluxInstance sync configuration.
func DetectRegistryFromFlux(ctx context.Context) (*Info, error) {
	return detectRegistryFromGitOps(ctx, gitOpsResourceSpec{
		gvr: schema.GroupVersionResource{
			Group:    "fluxcd.controlplane.io",
			Version:  "v1",
			Resource: "fluxinstances",
		},
		namespace:  "flux-system",
		name:       "flux",
		urlPath:    []string{"spec", "sync", "url"},
		refPath:    []string{"spec", "sync", "ref"},
		errNoURL:   ErrFluxNoSyncURL,
		sourceName: "FluxInstance",
	})
}

// DetectRegistryFromArgoCD tries to get registry URL from ArgoCD Application source.
func DetectRegistryFromArgoCD(ctx context.Context) (*Info, error) {
	return detectRegistryFromGitOps(ctx, gitOpsResourceSpec{
		gvr: schema.GroupVersionResource{
			Group:    "argoproj.io",
			Version:  "v1alpha1",
			Resource: "applications",
		},
		namespace:  "argocd",
		name:       "ksail",
		urlPath:    []string{"spec", "source", "repoURL"},
		refPath:    []string{"spec", "source", "targetRevision"},
		errNoURL:   ErrArgoCDNoRepoURL,
		sourceName: "ArgoCD",
	})
}

// DetectRegistryFromDocker tries to find a local registry Docker container.
func DetectRegistryFromDocker(ctx context.Context, clusterName string) (*Info, error) {
	resources, err := dockerclient.NewResources()
	if err != nil {
		return nil, fmt.Errorf("create docker resources: %w", err)
	}

	defer resources.Close()

	registryManager := resources.RegistryManager

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
			return &Info{
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

	return &Info{
		Host:       "localhost",
		Port:       int32(port), //nolint:gosec // port validated by Docker API
		Repository: "",          // Will be derived from source directory
		IsExternal: false,
		Source:     "docker:" + foundName,
	}, nil
}
