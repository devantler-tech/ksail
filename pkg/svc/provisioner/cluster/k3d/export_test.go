package k3dprovisioner

import (
	"context"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	k3kv1beta1 "github.com/rancher/k3k/pkg/apis/k3k.io/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// ListAgentNodesForTest exposes listAgentNodes (the pipe+goroutine node-list path)
// so its runtime-down behavior can be exercised.
func (k *Provisioner) ListAgentNodesForTest(ctx context.Context, clusterName string) error {
	_, err := k.listAgentNodes(ctx, clusterName)

	return err
}

// RunK3dSafelyForTest exposes runK3dSafely so a test can raise k3d's fatal exit
// with a chosen message and check the error it becomes.
func (k *Provisioner) RunK3dSafelyForTest(action func() error) error {
	return k.runK3dSafely(action)
}

// RuntimeInfoClientForTest is the client interface the runtime check uses, so a
// test can stand in for the Docker daemon without a connection.
type RuntimeInfoClientForTest = runtimeInfoClient

// RuntimeReachableWithinForTest exposes the real runtime check with a chosen
// client and time limit, so a test can prove a silent daemon cannot hold it up.
func RuntimeReachableWithinForTest(
	newClient func() (RuntimeInfoClientForTest, error),
	timeout time.Duration,
) error {
	return runtimeReachableWithin(newClient, timeout)
}

// WithRuntimeProbeForTest replaces the check of whether k3d's runtime answers,
// so a test decides whether Docker counts as reachable.
func (k *Provisioner) WithRuntimeProbeForTest(probe func() error) *Provisioner {
	k.runtimeReachable = probe

	return k
}

// AddAgentNodesForTest exposes addAgentNodes (a synchronous node-command site) so
// its runtime-down behavior can be exercised.
func (k *Provisioner) AddAgentNodesForTest(
	ctx context.Context,
	clusterName string,
	count int,
) error {
	return k.addAgentNodes(ctx, clusterName, count, &clusterupdate.UpdateResult{})
}

// RemoveAgentNodesForTest exposes removeAgentNodes (a synchronous node-command
// site) so its runtime-down behavior can be exercised.
func (k *Provisioner) RemoveAgentNodesForTest(
	ctx context.Context,
	clusterName string,
	count int,
) error {
	return k.removeAgentNodes(ctx, clusterName, count, &clusterupdate.UpdateResult{})
}

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

// EnsureNamespaceForTest exposes ensureNamespace for unit testing. name is the lifecycle argument
// Create/Delete pass, so a test can exercise the case where the operator's provisioned name
// differs from the factory's configured fallback.
func (p *K3kProvisioner) EnsureNamespaceForTest(
	ctx context.Context,
	name, namespace string,
) error {
	return p.ensureNamespace(ctx, p.effectiveClusterName(name), namespace)
}

// DeletePrivilegedPodGuardForTest exposes deletePrivilegedPodGuard for unit testing.
func (p *K3kProvisioner) DeletePrivilegedPodGuardForTest(
	ctx context.Context,
	clusterName string,
) error {
	return p.deletePrivilegedPodGuard(ctx, clusterName)
}

// PrivilegedPodGuardNameForTest exposes privilegedPodGuardName for unit testing.
func (p *K3kProvisioner) PrivilegedPodGuardNameForTest(clusterName string) string {
	return p.privilegedPodGuardName(clusterName)
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
