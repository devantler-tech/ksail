package registryresolver

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Clients carries the cluster-targeting coordinates (kubeconfig path + context)
// that registry resolution needs so that --kubeconfig / --context plumbed by the
// CLI are honored consistently across GitOps-resource lookups, credential
// merging, and GitOps-engine detection.
//
// When Kubeconfig and Context are both empty, the bundle falls back to the
// client-go default loading rules (KUBECONFIG env var, ~/.kube/config),
// preserving the historical behavior of the package.
//
// The optional KubernetesClient / DynamicClient / HelmClient fields let tests
// inject fakes without touching the kubeconfig on disk; when nil, the
// corresponding accessor builds a real client from the resolved REST config.
type Clients struct {
	// Kubeconfig is the explicit kubeconfig path. Empty means default loading rules.
	Kubeconfig string
	// Context is the kubeconfig context to use. Empty means the default context.
	Context string

	// KubernetesClient, when non-nil, is used instead of building one. Test seam.
	KubernetesClient kubernetes.Interface
	// DynamicClient, when non-nil, is used instead of building one. Test seam.
	DynamicClient dynamic.Interface
	// HelmClient, when non-nil, is used instead of building one. Test seam.
	HelmClient helm.Interface
}

// restConfig returns the REST config for the bundle's kubeconfig/context. When
// both are empty it uses the default loading rules (preserving the package's
// historical behavior); otherwise it loads the explicit kubeconfig/context.
func (c *Clients) restConfig() (*rest.Config, error) {
	if c == nil || (c.Kubeconfig == "" && c.Context == "") {
		cfg, err := k8s.GetRESTConfig()
		if err != nil {
			return nil, fmt.Errorf("get REST config: %w", err)
		}

		return cfg, nil
	}

	cfg, err := k8s.BuildRESTConfig(c.Kubeconfig, c.Context)
	if err != nil {
		return nil, fmt.Errorf("build REST config: %w", err)
	}

	return cfg, nil
}

// kubernetesClient returns the bundle's injected Kubernetes client or builds one
// from the resolved REST config.
func (c *Clients) kubernetesClient() (kubernetes.Interface, error) {
	if c != nil && c.KubernetesClient != nil {
		return c.KubernetesClient, nil
	}

	restConfig, err := c.restConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return clientset, nil
}

// dynamicClient returns the bundle's injected dynamic client or builds one from
// the resolved REST config.
func (c *Clients) dynamicClient() (dynamic.Interface, error) {
	if c != nil && c.DynamicClient != nil {
		return c.DynamicClient, nil
	}

	restConfig, err := c.restConfig()
	if err != nil {
		return nil, err
	}

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return dynClient, nil
}

// helmClient returns the bundle's injected Helm client or builds one from the
// bundle's kubeconfig/context.
func (c *Clients) helmClient() (helm.Interface, error) {
	if c != nil && c.HelmClient != nil {
		return c.HelmClient, nil
	}

	kubeconfig, context := "", ""
	if c != nil {
		kubeconfig, context = c.Kubeconfig, c.Context
	}

	client, err := helm.NewClient(kubeconfig, context)
	if err != nil {
		return nil, fmt.Errorf("create helm client: %w", err)
	}

	return client, nil
}
