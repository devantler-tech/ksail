package clusterapi

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"k8s.io/client-go/dynamic"
)

// SetDynamicClientForTest overrides the dynamic-client builder so resource-browser tests can inject a
// fake client instead of resolving a real kubeconfig context.
func (s *Service) SetDynamicClientForTest(
	build func(ctx context.Context, clusterName string) (dynamic.Interface, error),
) {
	s.newDynamicClient = build
}

// ContextForCluster exposes contextForCluster for black-box tests of name→context resolution.
func ContextForCluster(kubeconfigPath, clusterName string) (string, error) {
	return contextForCluster(kubeconfigPath, clusterName)
}

// NewTestService returns a Service whose provisioner factory is overridden, so black-box tests can
// substitute fake provisioners without touching the real Docker-backed factory. Discovery is
// restricted to the Docker provider so tests stay hermetic — they never reach out to cloud
// providers (Hetzner/Omni/AWS) or a host Kubernetes cluster based on the developer's environment.
func NewTestService(factory FactoryFunc) *Service {
	service := NewService()
	service.newFactory = factory
	service.discoverProviders = []v1alpha1.Provider{v1alpha1.ProviderDocker}

	return service
}

// ExportEKSConfigForCreate exposes eksDistributionConfig for testing the generated eks.yaml. It
// returns the written config path and the resolved region.
func ExportEKSConfigForCreate(name string) (string, string, error) {
	config, err := eksDistributionConfig(name)
	if err != nil {
		return "", "", err
	}

	return config.EKS.ConfigPath, config.EKS.Region, nil
}
