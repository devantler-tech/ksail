package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
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
		"unknown distribution: context does not match kind-, k3d-, or admin@ pattern",
	)

	// ErrEmptyClusterName is returned when cluster name detection results in an empty string.
	// This happens with malformed contexts like "kind-", "k3d-", or "admin@".
	ErrEmptyClusterName = errors.New("empty cluster name detected from context")

	// ErrUnableToDetectProvider indicates the provider could not be determined from the server endpoint.
	ErrUnableToDetectProvider = errors.New("unable to detect provider from server endpoint")

	// ErrNoCloudCredentials indicates no cloud provider credentials were found for a public IP.
	ErrNoCloudCredentials = errors.New("public IP detected but no cloud provider credentials found")
)

// ClusterInfo contains the detected distribution and provider for a cluster.
type ClusterInfo struct {
	Distribution   v1alpha1.Distribution
	Provider       v1alpha1.Provider
	ClusterName    string
	ServerURL      string
	KubeconfigPath string
}

// DetectClusterInfo detects the distribution and provider from the kubeconfig context.
// It reads the kubeconfig, determines the distribution from the context name pattern,
// and detects the provider by analyzing the server endpoint.
func DetectClusterInfo(kubeconfigPath, contextName string) (*ClusterInfo, error) {
	// Resolve kubeconfig path
	kubeconfigPath = resolveKubeconfigPath(kubeconfigPath)

	// Load kubeconfig
	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Resolve context name
	if contextName == "" {
		if config.CurrentContext == "" {
			return nil, ErrNoCurrentContext
		}

		contextName = config.CurrentContext
	}

	// Get context
	kubeContext, exists := config.Contexts[contextName]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	// Get cluster
	cluster, exists := config.Clusters[kubeContext.Cluster]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrClusterNotFound, kubeContext.Cluster)
	}

	// Detect distribution from context name
	distribution, clusterName, err := DetectDistributionFromContext(contextName)
	if err != nil {
		return nil, err
	}

	// Detect provider from server endpoint
	provider, err := detectProviderFromEndpoint(distribution, cluster.Server, clusterName)
	if err != nil {
		return nil, err
	}

	return &ClusterInfo{
		Distribution:   distribution,
		Provider:       provider,
		ClusterName:    clusterName,
		ServerURL:      cluster.Server,
		KubeconfigPath: kubeconfigPath,
	}, nil
}

// DetectDistributionFromContext detects the Kubernetes distribution and cluster name
// from the kubeconfig context name pattern.
// Returns the distribution, cluster name, and any error.
//
// Context name patterns:
//   - kind-<cluster-name> → Vanilla (Kind)
//   - k3d-<cluster-name> → K3s (K3d)
//   - admin@<cluster-name> → Talos
//
// Returns an error if the pattern is unrecognized or if the extracted cluster name is empty.
func DetectDistributionFromContext(contextName string) (v1alpha1.Distribution, string, error) {
	// Vanilla: kind-<cluster-name>
	if clusterName, ok := strings.CutPrefix(contextName, "kind-"); ok {
		if clusterName == "" {
			return "", "", fmt.Errorf(
				"%w: context %q has empty cluster name", ErrEmptyClusterName, contextName,
			)
		}

		return v1alpha1.DistributionVanilla, clusterName, nil
	}

	// K3s: k3d-<cluster-name>
	if clusterName, ok := strings.CutPrefix(contextName, "k3d-"); ok {
		if clusterName == "" {
			return "", "", fmt.Errorf(
				"%w: context %q has empty cluster name", ErrEmptyClusterName, contextName,
			)
		}

		return v1alpha1.DistributionK3s, clusterName, nil
	}

	// Talos: admin@<cluster-name>
	if clusterName, ok := strings.CutPrefix(contextName, "admin@"); ok {
		if clusterName == "" {
			return "", "", fmt.Errorf(
				"%w: context %q has empty cluster name", ErrEmptyClusterName, contextName,
			)
		}

		return v1alpha1.DistributionTalos, clusterName, nil
	}

	return "", "", fmt.Errorf("%w: %s", ErrUnknownContextPattern, contextName)
}

// detectProviderFromEndpoint determines the provider based on the server endpoint URL.
// For localhost endpoints, returns ProviderDocker.
// For public IPs, queries cloud provider APIs to verify ownership.
func detectProviderFromEndpoint(
	distribution v1alpha1.Distribution,
	serverURL string,
	clusterName string,
) (v1alpha1.Provider, error) {
	// Kind and K3d always use Docker
	if distribution == v1alpha1.DistributionVanilla || distribution == v1alpha1.DistributionK3s {
		return v1alpha1.ProviderDocker, nil
	}

	// For Talos, analyze the server endpoint
	host, err := extractHostFromURL(serverURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse server URL: %w", err)
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
		return "", err
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host found in URL: %s", serverURL)
	}

	return host, nil
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

	// TODO: Add more cloud providers here as they are implemented
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
func checkHetznerOwnership(ctx context.Context, token, ipAddress, clusterName string) (bool, error) {
	client := hcloud.NewClient(hcloud.WithToken(token))
	provider := hetzner.NewProvider(client)

	// List all nodes for the cluster
	nodes, err := provider.ListNodes(ctx, clusterName)
	if err != nil {
		return false, err
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

// resolveKubeconfigPath returns the kubeconfig path, resolving defaults if empty.
func resolveKubeconfigPath(kubeconfigPath string) string {
	if kubeconfigPath != "" {
		return kubeconfigPath
	}

	// Check KUBECONFIG env var
	if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		// Use first path if multiple are specified
		paths := strings.Split(envPath, string(os.PathListSeparator))
		if len(paths) > 0 && paths[0] != "" {
			return paths[0]
		}
	}

	// Default to ~/.kube/config
	return clientcmd.RecommendedHomeFile
}
