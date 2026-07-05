package kindprovisioner

import (
	"context"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/log"
)

// NewKubernetesProvisionerForConnectorTest builds a minimal KubernetesProvisioner exercising only
// the operator Connector paths (publish + read): the host clientset, the nested cluster name
// (resolved through the embedded kindConfig), and the on-disk kubeconfig path. It skips the DinD
// infra the full NewKubernetesProvisioner wires, which a Connector unit test does not need.
func NewKubernetesProvisionerForConnectorTest(
	clientset kubernetes.Interface, clusterName, kubeconfigPath string,
) *KubernetesProvisioner {
	return &KubernetesProvisioner{
		Provisioner:    &Provisioner{kindConfig: &v1alpha4.Cluster{Name: clusterName}},
		hostClientset:  clientset,
		kubeconfigPath: kubeconfigPath,
	}
}

// PublishConnectorKubeconfigForTest exposes publishConnectorKubeconfig for unit testing.
func (k *KubernetesProvisioner) PublishConnectorKubeconfigForTest(
	ctx context.Context, target string,
) error {
	return k.publishConnectorKubeconfig(ctx, target)
}

// KubeConfigForTest returns the kubeConfig field for testing purposes.
func (k *Provisioner) KubeConfigForTest() string {
	return k.kubeConfig
}

// WithWaitForReadyForTest injects a stub readiness waiter so Start can be
// exercised without a live cluster.
func (k *Provisioner) WithWaitForReadyForTest(
	f func(ctx context.Context, kubeconfigPath, contextName string) error,
) *Provisioner {
	k.waitForReady = f

	return k
}

// NewStreamLoggerForTest creates a streamLogger for testing.
func NewStreamLoggerForTest(w interface {
	Write(p []byte) (n int, err error)
},
) log.Logger {
	return &streamLogger{writer: w}
}

// SetNameForTest exposes setName for unit testing.
func SetNameForTest(name, kindConfigName string) string {
	return setName(name, kindConfigName)
}
