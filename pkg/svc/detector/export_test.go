package detector

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
)

// ExportDetectCNI exposes detectCNI for testing.
func (d *ComponentDetector) ExportDetectCNI(ctx context.Context) (v1alpha1.CNI, error) {
	return d.detectCNI(ctx)
}

// ExportDetectCSI exposes detectCSI for testing.
func (d *ComponentDetector) ExportDetectCSI(
	ctx context.Context,
	dist v1alpha1.Distribution,
	prov v1alpha1.Provider,
) (v1alpha1.CSI, error) {
	return d.detectCSI(ctx, dist, prov)
}

// ExportDetectMetricsServer exposes detectMetricsServer for testing.
func (d *ComponentDetector) ExportDetectMetricsServer(
	ctx context.Context,
	dist v1alpha1.Distribution,
) (v1alpha1.MetricsServer, error) {
	return d.detectMetricsServer(ctx, dist)
}

// ExportDetectLoadBalancer exposes detectLoadBalancer for testing.
func (d *ComponentDetector) ExportDetectLoadBalancer(
	ctx context.Context,
	dist v1alpha1.Distribution,
	prov v1alpha1.Provider,
) (v1alpha1.LoadBalancer, error) {
	return d.detectLoadBalancer(ctx, dist, prov)
}

// ExportDetectCertManager exposes detectCertManager for testing.
func (d *ComponentDetector) ExportDetectCertManager(
	ctx context.Context,
) (v1alpha1.CertManager, error) {
	return d.detectCertManager(ctx)
}

// ExportDetectPolicyEngine exposes detectPolicyEngine for testing.
func (d *ComponentDetector) ExportDetectPolicyEngine(
	ctx context.Context,
) (v1alpha1.PolicyEngine, error) {
	return d.detectPolicyEngine(ctx)
}

// ExportDetectGitOpsEngine exposes detectGitOpsEngine for testing.
func (d *ComponentDetector) ExportDetectGitOpsEngine(
	ctx context.Context,
) (v1alpha1.GitOpsEngine, error) {
	return d.detectGitOpsEngine(ctx)
}

// ExportDeploymentExists exposes deploymentExists for testing.
func (d *ComponentDetector) ExportDeploymentExists(
	ctx context.Context,
	name, namespace string,
) bool {
	return d.deploymentExists(ctx, name, namespace)
}

// ExportDaemonSetExistsWithLabel exposes daemonSetExistsWithLabel for testing.
func (d *ComponentDetector) ExportDaemonSetExistsWithLabel(
	ctx context.Context,
	labelKey string,
) bool {
	return d.daemonSetExistsWithLabel(ctx, labelKey)
}

// ExportContainerExists exposes containerExists for testing.
func (d *ComponentDetector) ExportContainerExists(
	ctx context.Context,
	containerName string,
) (bool, error) {
	return d.containerExists(ctx, containerName)
}

// ReleaseMappingForTest is the exported alias of releaseMapping for testing.
type ReleaseMappingForTest[T ~string] = releaseMapping[T]

// NewReleaseMappingForTest creates a releaseMapping for testing.
func NewReleaseMappingForTest[T ~string](
	release, namespace string,
	value T,
) ReleaseMappingForTest[T] {
	return releaseMapping[T]{release: release, namespace: namespace, value: value}
}

// ExportDetectFirstRelease exposes detectFirstRelease for testing.
func ExportDetectFirstRelease[T ~string](
	ctx context.Context,
	helmClient helm.Interface,
	mappings []ReleaseMappingForTest[T],
	defaultVal T,
) (T, error) {
	return detectFirstRelease(ctx, helmClient, mappings, defaultVal)
}
