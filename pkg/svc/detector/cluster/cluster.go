package cluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"k8s.io/client-go/tools/clientcmd"
)

// Detection errors.
var (
	// ErrNoCurrentContext indicates no current context is set in kubeconfig.
	ErrNoCurrentContext = errors.New("no current context set in kubeconfig")

	// ErrContextNotFound indicates the specified context was not found in kubeconfig.
	ErrContextNotFound = errors.New("context not found in kubeconfig")

	// ErrClusterNotFound indicates the cluster referenced by the context was not found.
	ErrClusterNotFound = errors.New("cluster not found in kubeconfig")

	// ErrUnknownContextPattern indicates the context name doesn't match any known distribution pattern.
	ErrUnknownContextPattern = errors.New(
		"unknown distribution: context does not match kind-, k3d-, admin@, vcluster-docker_, or kwok- pattern",
	)

	// ErrEmptyClusterName is returned when cluster name detection results in an empty string.
	// This happens with malformed contexts like "kind-", "k3d-", or "admin@".
	ErrEmptyClusterName = errors.New("empty cluster name detected from context")

	// ErrUnableToDetectProvider indicates the provider could not be determined from the server endpoint.
	ErrUnableToDetectProvider = errors.New("unable to detect provider from server endpoint")

	// ErrNoCloudCredentials indicates no cloud provider credentials were found for a public IP.
	ErrNoCloudCredentials = errors.New("public IP detected but no cloud provider credentials found")

	// ErrNoHostInURL indicates a URL was parsed but contained no host component.
	ErrNoHostInURL = errors.New("no host found in URL")
)

// Info contains the detected distribution and provider for a cluster.
type Info struct {
	Distribution   v1alpha1.Distribution
	Provider       v1alpha1.Provider
	ClusterName    string
	Context        string
	ServerURL      string
	KubeconfigPath string
}

// kubeContext holds the resolved kubeconfig fields needed by DetectInfo.
type kubeContext struct {
	kubeconfigPath string
	contextName    string
	clusterRef     string
	serverURL      string
}

// loadKubeContext resolves the kubeconfig path, loads the file, and extracts
// the context name, cluster reference, and server URL.
func loadKubeContext(kubeconfigPath, contextName string) (*kubeContext, error) {
	resolvedPath, err := ResolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // G304: Intentional file reading from user-provided kubeconfig path
	configBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	if contextName == "" {
		if config.CurrentContext == "" {
			return nil, ErrNoCurrentContext
		}

		contextName = config.CurrentContext
	}

	ctx, exists := config.Contexts[contextName]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	cluster, exists := config.Clusters[ctx.Cluster]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrClusterNotFound, ctx.Cluster)
	}

	return &kubeContext{
		kubeconfigPath: resolvedPath,
		contextName:    contextName,
		clusterRef:     ctx.Cluster,
		serverURL:      cluster.Server,
	}, nil
}

// DetectInfo detects the distribution and provider from the kubeconfig context.
// It reads the kubeconfig, determines the distribution from the context name pattern,
// and detects the provider by analyzing the server endpoint.
func DetectInfo(kubeconfigPath, contextName string) (*Info, error) {
	resolved, err := loadKubeContext(kubeconfigPath, contextName)
	if err != nil {
		return nil, err
	}

	// Detect distribution from context name
	distribution, clusterName, err := DetectDistributionFromContext(resolved.contextName)
	if err != nil {
		// Context name didn't match any known pattern. Fall back to server
		// URL-based detection for cloud providers with distinctive hostnames.
		if !errors.Is(err, ErrUnknownContextPattern) {
			return nil, err
		}

		distribution, clusterName, err = detectFromServerURL(
			resolved.serverURL,
			resolved.clusterRef,
		)
		if err != nil {
			return nil, err
		}
	}

	// Detect provider from server endpoint
	provider, err := detectProviderFromEndpoint(distribution, resolved.serverURL, clusterName)
	if err != nil {
		return nil, err
	}

	return &Info{
		Distribution:   distribution,
		Provider:       provider,
		ClusterName:    clusterName,
		Context:        resolved.contextName,
		ServerURL:      resolved.serverURL,
		KubeconfigPath: resolved.kubeconfigPath,
	}, nil
}

// contextPatterns maps distribution context prefixes to their distributions.
// Used by DetectDistributionFromContext to detect the distribution.
var contextPatterns = []struct { //nolint:gochecknoglobals // static lookup table
	prefix       string
	distribution v1alpha1.Distribution
}{
	{"kind-", v1alpha1.DistributionVanilla},
	{"k3d-", v1alpha1.DistributionK3s},
	{"admin@", v1alpha1.DistributionTalos},
	{"vcluster-docker_", v1alpha1.DistributionVCluster},
	{"kwok-", v1alpha1.DistributionKWOK},
}

// DetectDistributionFromContext detects the Kubernetes distribution and cluster name
// from the kubeconfig context name pattern.
// Returns the distribution, cluster name, and any error.
//
// Context name patterns:
//   - kind-<cluster-name> → Vanilla (Kind)
//   - k3d-<cluster-name> → K3s (K3d)
//   - admin@<cluster-name> → Talos
//   - vcluster-docker_<cluster-name> → VCluster
//   - kwok-<cluster-name> → KWOK
//
// Returns an error if the pattern is unrecognized or if the extracted cluster name is empty.
func DetectDistributionFromContext(contextName string) (v1alpha1.Distribution, string, error) {
	for _, pattern := range contextPatterns {
		if clusterName, ok := strings.CutPrefix(contextName, pattern.prefix); ok {
			if clusterName == "" {
				return "", "", fmt.Errorf(
					"%w: context %q has empty cluster name", ErrEmptyClusterName, contextName,
				)
			}

			return pattern.distribution, clusterName, nil
		}
	}

	return "", "", fmt.Errorf("%w: %s", ErrUnknownContextPattern, contextName)
}

// detectFromServerURL infers distribution and cluster name from the server URL
// when the kubeconfig context name doesn't match any known pattern. This handles
// cloud providers with distinctive hostnames, such as Sidero Omni whose
// Kubernetes API proxy URLs end in .omni.siderolabs.io. The kubeconfig cluster
// reference is used only when endpoint-based detection needs a cluster name.
func detectFromServerURL(
	serverURL string,
	kubeconfigClusterRef string,
) (v1alpha1.Distribution, string, error) {
	host, err := extractHostFromURL(serverURL)
	if err != nil {
		return "", "", fmt.Errorf(
			"%w: server URL %q is unrecognizable (kubeconfig cluster reference %q): %w",
			ErrUnknownContextPattern, serverURL, kubeconfigClusterRef, err,
		)
	}

	// Omni endpoints → Talos (Omni only supports Talos)
	if isOmniEndpoint(host) {
		// Use the kubeconfig cluster reference as the cluster name since
		// the context name is a service-account identifier for Omni.
		clusterName := kubeconfigClusterRef
		if clusterName == "" {
			return "", "", fmt.Errorf(
				"%w: Omni endpoint detected but cluster name is empty",
				ErrEmptyClusterName,
			)
		}

		return v1alpha1.DistributionTalos, clusterName, nil
	}

	return "", "", fmt.Errorf(
		"%w: server URL %s does not match any known cloud provider pattern",
		ErrUnknownContextPattern, serverURL,
	)
}

// detectProviderFromEndpoint determines the provider based on the server endpoint URL.
// For localhost endpoints, returns ProviderDocker.
// For public IPs, queries cloud provider APIs to verify ownership.
func detectProviderFromEndpoint(
	distribution v1alpha1.Distribution,
	serverURL string,
	clusterName string,
) (v1alpha1.Provider, error) {
	// Kind, K3d, VCluster, and KWOK always use Docker
	if distribution == v1alpha1.DistributionVanilla ||
		distribution == v1alpha1.DistributionK3s ||
		distribution == v1alpha1.DistributionVCluster ||
		distribution == v1alpha1.DistributionKWOK {
		return v1alpha1.ProviderDocker, nil
	}

	// For Talos, analyze the server endpoint
	host, err := extractHostFromURL(serverURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse server URL: %w", err)
	}

	// Check if it's a Sidero Omni endpoint → Omni
	if isOmniEndpoint(host) {
		return v1alpha1.ProviderOmni, nil
	}

	// Check if it's a localhost endpoint → Docker
	if isLocalhost(host) {
		return v1alpha1.ProviderDocker, nil
	}

	// Public IP detected - query cloud providers to verify ownership
	return detectCloudProvider(host, clusterName)
}

// extractHostFromURL extracts the host (IP or hostname) from a URL.
func extractHostFromURL(serverURL string) (string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse server URL: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("%w: %s", ErrNoHostInURL, serverURL)
	}

	return host, nil
}

// omniHostSuffix is the DNS suffix for Sidero Omni SaaS endpoints.
// Omni Kubernetes API proxy URLs follow the pattern:
// https://<account>.kubernetes.<region>.omni.siderolabs.io
const omniHostSuffix = ".omni.siderolabs.io"

// isOmniEndpoint checks if the host is a Sidero Omni endpoint.
func isOmniEndpoint(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), omniHostSuffix)
}

// isLocalhost checks if the host is a localhost address.
func isLocalhost(host string) bool {
	// Check common localhost names
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	// Check if it's a loopback IP
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}

	return false
}

// detectCloudProvider queries cloud provider APIs to find which provider owns the IP.
// It checks each provider that has credentials available.
func detectCloudProvider(ipAddress, clusterName string) (v1alpha1.Provider, error) {
	ctx := context.Background()

	// Check Hetzner
	hetznerToken := os.Getenv("HCLOUD_TOKEN")
	if hetznerToken != "" {
		found, err := checkHetznerOwnership(ctx, hetznerToken, ipAddress, clusterName)
		if err == nil && found {
			return v1alpha1.ProviderHetzner, nil
		}
		// Continue checking other providers if not found
	}

	// Add more cloud providers here as they are implemented.
	// Example:
	// if awsCredentialsAvailable() {
	//     found, err := checkAWSOwnership(ctx, ipAddress, clusterName)
	//     if err == nil && found {
	//         return v1alpha1.ProviderAWS, nil
	//     }
	// }

	// No provider found
	if hetznerToken == "" {
		return "", fmt.Errorf("%w: set HCLOUD_TOKEN for Hetzner", ErrNoCloudCredentials)
	}

	return "", fmt.Errorf(
		"%w: IP %s not found in any configured cloud provider",
		ErrUnableToDetectProvider,
		ipAddress,
	)
}

// checkHetznerOwnership verifies if a server with the given IP exists in Hetzner
// and belongs to the specified cluster (via KSail labels).
func checkHetznerOwnership(
	ctx context.Context,
	token, ipAddress, clusterName string,
) (bool, error) {
	client := hcloud.NewClient(hcloud.WithToken(token))
	provider := hetzner.NewProvider(client)

	// List all nodes for the cluster
	nodes, err := provider.ListNodes(ctx, clusterName)
	if err != nil {
		return false, fmt.Errorf("failed to list nodes for cluster %s: %w", clusterName, err)
	}

	// Check if any node has the matching IP
	for _, node := range nodes {
		// Get the server details to check its IPs
		server, _, err := client.Server.GetByName(ctx, node.Name)
		if err != nil {
			continue
		}

		if server == nil {
			continue
		}

		// Check public IPv4
		if server.PublicNet.IPv4.IP != nil && server.PublicNet.IPv4.IP.String() == ipAddress {
			return true, nil
		}

		// Check public IPv6
		if server.PublicNet.IPv6.IP != nil && server.PublicNet.IPv6.IP.String() == ipAddress {
			return true, nil
		}
	}

	return false, nil
}

// ResolveKubeconfigPath returns the kubeconfig path, resolving defaults if empty
// and expanding ~ to the home directory.
func ResolveKubeconfigPath(kubeconfigPath string) (string, error) {
	// If path is provided, expand ~ and return it
	if kubeconfigPath != "" {
		expanded, err := fsutil.ExpandHomePath(kubeconfigPath)
		if err != nil {
			return "", fmt.Errorf("expand kubeconfig path: %w", err)
		}

		return expanded, nil
	}

	// Check KUBECONFIG env var
	if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		// Use first path if multiple are specified
		paths := strings.Split(envPath, string(os.PathListSeparator))
		if len(paths) > 0 && paths[0] != "" {
			expanded, err := fsutil.ExpandHomePath(paths[0])
			if err != nil {
				return "", fmt.Errorf("expand kubeconfig path from env: %w", err)
			}

			return expanded, nil
		}
	}

	// Default to ~/.kube/config
	return clientcmd.RecommendedHomeFile, nil
}
