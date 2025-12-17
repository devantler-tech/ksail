package k8s

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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

	return restConfig, nil
}
