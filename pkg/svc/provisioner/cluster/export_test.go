package clusterprovisioner

import k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"

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
