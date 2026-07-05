//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package kwokprovisioner

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

// KwokControllerImageVersionForTest exposes kwokControllerImageVersion for unit testing.
const KwokControllerImageVersionForTest = kwokControllerImageVersion

// NewKubernetesProvisionerForConnectorTest builds a minimal KubernetesProvisioner exercising only
// the operator Connector paths (publish + read): the host clientset, the nested cluster name
// (resolved through the embedded Provisioner), and the on-disk kubeconfig path. It skips the DinD
// infra the full NewKubernetesProvisioner wires, which a Connector unit test does not need.
func NewKubernetesProvisionerForConnectorTest(
	clientset kubernetes.Interface, clusterName, kubeconfigPath string,
) *KubernetesProvisioner {
	return &KubernetesProvisioner{
		Provisioner:    &Provisioner{name: clusterName},
		hostClientset:  clientset,
		kubeconfigPath: kubeconfigPath,
	}
}

// PublishConnectorKubeconfigForTest exposes publishConnectorKubeconfig for unit testing.
func (p *KubernetesProvisioner) PublishConnectorKubeconfigForTest(
	ctx context.Context, target string,
) error {
	return p.publishConnectorKubeconfig(ctx, target)
}

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry

// TransientCreateErrorsForTest exposes transientCreateErrors for unit testing.
var TransientCreateErrorsForTest = transientCreateErrors

// KwokContainerNamesForTest exposes kwokContainerNames for unit testing.
var KwokContainerNamesForTest = kwokContainerNames

// KwokStateDirForTest exposes kwokStateDir for unit testing.
var KwokStateDirForTest = kwokStateDir

// SetDefaultClusterForTest exposes setDefaultCluster for unit testing.
var SetDefaultClusterForTest = setDefaultCluster

// ResolveNameForTest exposes resolveName for unit testing.
func (p *Provisioner) ResolveNameForTest(name string) string {
	return p.resolveName(name)
}

// ResolveConfigPathForTest exposes resolveConfigPath for unit testing.
func (p *Provisioner) ResolveConfigPathForTest() (string, func(), error) {
	return p.resolveConfigPath()
}

// DiscoverAPIServerPortForTest exposes discoverAPIServerPort for unit testing.
func (p *KubernetesProvisioner) DiscoverAPIServerPortForTest(name string) (int, error) {
	return p.discoverAPIServerPort(name)
}

// ApplyKwokCertSANsForTest exposes applyKwokCertSANs for unit testing.
func (p *KubernetesProvisioner) ApplyKwokCertSANsForTest(address string) (func(), error) {
	return p.applyKwokCertSANs(address)
}

// ConfigPathForTest returns the inner provisioner's configPath for assertions.
func (p *KubernetesProvisioner) ConfigPathForTest() string {
	return p.configPath
}
