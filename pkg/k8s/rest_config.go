package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Default client-side rate limiter settings.
//
// client-go defaults to QPS=5 / Burst=10 which is too restrictive for CLI
// tools that use exec-based credential plugins (e.g. OIDC). The conservative
// defaults can cause "client rate limiter Wait returned an error: context
// deadline exceeded" when the exec plugin takes time to acquire a token.
//
// These values align with kubectl's QPS=50 and provide generous burst
// headroom. They are safe for CLI usage where requests are user-initiated
// rather than automated at high frequency.
const (
	defaultQPS   = 50
	defaultBurst = 100
)

// DefaultKubeconfigPath returns the default kubeconfig path for the current user.
// The path is constructed as ~/.kube/config using the user's home directory.
func DefaultKubeconfigPath() string {
	homeDir, _ := os.UserHomeDir()

	return filepath.Join(homeDir, ".kube", "config")
}

// ResolveKubeconfigPath returns a usable kubeconfig path from the given input.
// If path is empty, the default kubeconfig path is returned.
// Otherwise path is expanded (tilde → home directory, relative → absolute) via fsutil.ExpandHomePath.
func ResolveKubeconfigPath(path string) (string, error) {
	if path == "" {
		return DefaultKubeconfigPath(), nil
	}

	expanded, err := fsutil.ExpandHomePath(path)
	if err != nil {
		return "", fmt.Errorf("expand kubeconfig path: %w", err)
	}

	return expanded, nil
}

// GetRESTConfig loads the kubeconfig using default loading rules and returns a REST config.
// This is a convenience function that uses the standard client-go loading rules
// (KUBECONFIG env var, default kubeconfig path) without requiring explicit paths.
func GetRESTConfig() (*rest.Config, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	applyDefaults(config)

	return config, nil
}

// BuildRESTConfig builds a Kubernetes REST config from kubeconfig path and optional context.
//
// The kubeconfig parameter must be a non-empty path to a valid kubeconfig file.
// The context parameter is optional and specifies which context to use from the kubeconfig.
// If context is empty, the default context from the kubeconfig is used.
//
// Returns ErrKubeconfigPathEmpty if kubeconfig path is empty.
// Returns an error if the kubeconfig cannot be loaded or parsed.
func BuildRESTConfig(kubeconfig, context string) (*rest.Config, error) {
	if kubeconfig == "" {
		return nil, ErrKubeconfigPathEmpty
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}

	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	applyDefaults(restConfig)

	return restConfig, nil
}

// ValidateContextExists checks that contextName exists in the kubeconfig at
// kubeconfigPath. It returns nil when the context is present, a wrapped
// ErrKubeconfigContextNotFound (listing the available contexts) when it is
// absent, and a wrapped load error when the kubeconfig cannot be read.
//
// This gives callers a deterministic, actionable check for a user-pinned
// context — clearer than the generic "no configuration has been provided"
// error client-go raises when an unresolved current-context is used.
func ValidateContextExists(kubeconfigPath, contextName string) error {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("load kubeconfig for context validation: %w", err)
	}

	if _, exists := config.Contexts[contextName]; exists {
		return nil
	}

	available := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		available = append(available, name)
	}

	slices.Sort(available)

	return fmt.Errorf(
		"%w: %q not found in %s (available: %s)",
		ErrKubeconfigContextNotFound,
		contextName, kubeconfigPath, strings.Join(available, ", "),
	)
}

// applyDefaults sets client-side rate limiter defaults on a REST config.
// This raises QPS/Burst from the client-go defaults (5/10) to values
// compatible with exec-based credential plugins such as OIDC.
func applyDefaults(config *rest.Config) {
	if config.QPS == 0 {
		config.QPS = defaultQPS
	}

	if config.Burst == 0 {
		config.Burst = defaultBurst
	}
}

// NewClientset creates a Kubernetes clientset from kubeconfig path and context.
// This is a convenience function that combines BuildRESTConfig and client creation.
func NewClientset(kubeconfig, context string) (*kubernetes.Clientset, error) {
	restConfig, err := BuildRESTConfig(kubeconfig, context)
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}
