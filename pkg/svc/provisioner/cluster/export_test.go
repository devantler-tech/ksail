package clusterprovisioner

import (
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindProvisionerFactory is the signature of the injectable Kind provisioner
// factory used by the minimal multi-provisioner path.
type KindProvisionerFactory = func(*v1alpha4.Cluster, string) (*kindprovisioner.Provisioner, error)

// SetKindProvisionerFactory swaps the Kind provisioner factory for a test double
// and returns a restore func. It lets external tests assert the kindConfig and
// kubeconfig arguments passed to the factory without reflecting over the
// provisioner's unexported fields.
func SetKindProvisionerFactory(factory KindProvisionerFactory) func() {
	previous := kindProvisionerFactory
	kindProvisionerFactory = factory

	return func() { kindProvisionerFactory = previous }
}

// ExportWrapK3kServerArgs exposes wrapK3kServerArgs for testing.
func ExportWrapK3kServerArgs(args []string) []string {
	return wrapK3kServerArgs(args)
}

// ExportApplyK3dNodeCounts exposes applyK3dNodeCounts for testing.
func ExportApplyK3dNodeCounts(config *k3dv1alpha5.SimpleConfig, controlPlanes, workers int32) {
	applyK3dNodeCounts(config, controlPlanes, workers)
}

// ExportWriteK3dConfigToTempFile exposes writeK3dConfigToTempFile for testing.
func ExportWriteK3dConfigToTempFile(config *k3dv1alpha5.SimpleConfig) (string, error) {
	return writeK3dConfigToTempFile(config)
}

// ExportKubeadmInstallVersion exposes kubeadmInstallVersion for testing.
func ExportKubeadmInstallVersion() string {
	return kubeadmInstallVersion()
}
