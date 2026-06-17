package k3dprovisioner

import (
	"context"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/runner"
	k3kv1beta1 "github.com/rancher/k3k/pkg/apis/k3k.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// K3kReadyTimeoutForTest exposes k3kReadyTimeout for unit testing.
func K3kReadyTimeoutForTest() time.Duration {
	return k3kReadyTimeout()
}

// FirstRunningPodNameForTest exposes firstRunningPodName for unit testing.
func FirstRunningPodNameForTest(pods []corev1.Pod) string {
	return firstRunningPodName(pods)
}

// BuildClusterCRForTest exposes buildClusterCR for unit testing.
func (p *K3kProvisioner) BuildClusterCRForTest(
	clusterName, namespace, certSAN string,
) *k3kv1beta1.Cluster {
	return p.buildClusterCR(clusterName, namespace, certSAN)
}

// EnsureNamespaceForTest exposes ensureNamespace for unit testing.
func (p *K3kProvisioner) EnsureNamespaceForTest(ctx context.Context, namespace string) error {
	return p.ensureNamespace(ctx, namespace)
}

// WithRunnerForTest injects a command runner so lifecycle operations can be
// exercised without invoking the real k3d runtime.
func (k *Provisioner) WithRunnerForTest(r runner.CommandRunner) *Provisioner {
	k.runner = r

	return k
}

// WithListClustersRawForTest injects a stub that returns canned cluster-list
// output, so List/Exists can be tested without invoking the real k3d runtime.
func (k *Provisioner) WithListClustersRawForTest(
	f func(ctx context.Context) (string, error),
) *Provisioner {
	k.listClustersRaw = f

	return k
}

// WithWaitForReadyForTest injects a stub readiness waiter so Start can be
// exercised without a live cluster.
func (k *Provisioner) WithWaitForReadyForTest(
	f func(ctx context.Context, kubeconfigPath, contextName string) error,
) *Provisioner {
	k.waitForReady = f

	return k
}

// KubeconfigForTest returns the kubeconfig field for testing purposes.
func (k *Provisioner) KubeconfigForTest() string {
	return k.kubeconfig
}

// ParseClusterNamesForTest exposes parseClusterNames for unit testing.
func ParseClusterNamesForTest(output string) ([]string, error) {
	return parseClusterNames(output)
}

// AgentNodeNamesForTest exposes agentNodeNames for unit testing.
func AgentNodeNamesForTest(existing []string, clusterName string, count int) []string {
	return agentNodeNames(existing, clusterName, count)
}

// ResolveNameForTest exposes resolveName for unit testing.
func (k *Provisioner) ResolveNameForTest(name string) string {
	return k.resolveName(name)
}

// AppendConfigFlagForTest exposes appendConfigFlag for unit testing.
func (k *Provisioner) AppendConfigFlagForTest(args []string) []string {
	return k.appendConfigFlag(args)
}

// AppendImageFlagForTest exposes appendImageFlag for unit testing.
func (k *Provisioner) AppendImageFlagForTest(args []string) []string {
	return k.appendImageFlag(args)
}
