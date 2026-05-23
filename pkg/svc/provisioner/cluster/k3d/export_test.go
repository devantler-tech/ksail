package k3dprovisioner

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/runner"
	k3kv1beta1 "github.com/rancher/k3k/pkg/apis/k3k.io/v1beta1"
)

// NewK3kProvisionerWithServerArgsForTest builds a minimal K3kProvisioner with only
// serverArgs set, so buildClusterCR can be exercised without host-cluster clients.
func NewK3kProvisionerWithServerArgsForTest(serverArgs []string) *K3kProvisioner {
	return &K3kProvisioner{serverArgs: serverArgs}
}

// BuildClusterCRForTest exposes buildClusterCR for unit testing.
func (p *K3kProvisioner) BuildClusterCRForTest(
	clusterName, namespace, certSAN string,
) *k3kv1beta1.Cluster {
	return p.buildClusterCR(clusterName, namespace, certSAN)
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

// ParseClusterNamesForTest exposes parseClusterNames for unit testing.
func ParseClusterNamesForTest(output string) ([]string, error) {
	return parseClusterNames(output)
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
