package installer

import (
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

const (
	// DefaultInstallTimeout is the default timeout (5 minutes) for component installation.
	DefaultInstallTimeout = 5 * time.Minute
	// TalosInstallTimeout is the timeout (5 minutes) for Talos component installation.
	// Talos clusters take longer to bootstrap due to the immutable OS design.
	TalosInstallTimeout = 5 * time.Minute
	// CalicoInstallTimeout is the timeout for Calico CNI installs, which often take longer
	// due to multiple components needing to become ready (tigera-operator, calico-node
	// DaemonSet, and calico-kube-controllers Deployment).
	CalicoInstallTimeout = 10 * time.Minute
	// KyvernoInstallTimeout is the timeout for Kyverno policy engine installs, which need
	// extra time for multiple deployments and CRDs to become ready (admission-controller,
	// background-controller, cleanup-controller, reports-controller, and policy CRDs).
	KyvernoInstallTimeout = 10 * time.Minute
	// CertManagerInstallTimeout is the timeout for cert-manager installs, which need
	// extra time for multiple deployments and webhook configurations to become ready.
	CertManagerInstallTimeout = 10 * time.Minute
	// GatekeeperInstallTimeout is the timeout for Gatekeeper policy engine installs,
	// which need extra time for the webhook and audit controller to become ready.
	GatekeeperInstallTimeout = 7 * time.Minute
	// FluxInstallTimeout is the timeout for Flux operator installs, which need
	// extra time for CRD establishment. On resource-constrained runners (e.g., GitHub Actions),
	// the Flux operator CRDs can take 7-10 minutes to become fully "Established" in the API server,
	// even though the operator pod is running. 12 minutes provides sufficient margin for slower
	// environments while ensuring quick feedback for actual failures.
	// See: https://github.com/devantler-tech/ksail/issues/2264
	FluxInstallTimeout = 12 * time.Minute
	// ArgoCDInstallTimeout is the timeout for ArgoCD installs, which need
	// extra time for multiple components to become ready (server, repo-server,
	// application-controller, applicationset-controller, and Redis).
	// See: https://github.com/devantler-tech/ksail/issues/2899
	ArgoCDInstallTimeout = 20 * time.Minute
	// ArgoCDVClusterInstallTimeout is the elevated timeout for ArgoCD installs
	// inside VCluster distributions. VCluster adds significant overhead because
	// each ArgoCD component runs inside the virtual cluster and inherits both the
	// VCluster networking overhead (syncer hop) and the latency imposed by active
	// admission-webhook policies (e.g., Gatekeeper). CI investigations confirmed
	// that the VCluster + Calico + Gatekeeper + ArgoCD combination regularly takes
	// ~15 minutes, exceeding the base ArgoCD timeout.
	// See: https://github.com/devantler-tech/ksail/issues/2899
	// See: https://github.com/devantler-tech/ksail/issues/2904
	ArgoCDVClusterInstallTimeout = 25 * time.Minute
	// VClusterInstallTimeout is the base timeout for component installs inside a VCluster
	// distribution. VCluster adds ~20-30% overhead relative to a native-node cluster
	// because every API call is forwarded through an extra hop (syncer) and admission
	// webhooks from CNI/policy layers run inside the virtual cluster. Using a slightly
	// larger base timeout ensures that even lightweight components have enough margin
	// to become ready in a heavily-loaded CI environment.
	// See: https://github.com/devantler-tech/ksail/issues/2899
	VClusterInstallTimeout = 8 * time.Minute
)

// GetInstallTimeout determines the timeout for component installation.
// Uses cluster connection timeout if configured, otherwise defaults to:
//   - TalosInstallTimeout (5m) for Talos distribution
//   - VClusterInstallTimeout (8m) for VCluster distribution
//   - DefaultInstallTimeout (5m) for all other distributions
//
// Returns DefaultInstallTimeout if clusterCfg is nil.
func GetInstallTimeout(clusterCfg *v1alpha1.Cluster) time.Duration {
	if clusterCfg == nil {
		return DefaultInstallTimeout
	}

	// Use explicit timeout if configured
	if clusterCfg.Spec.Cluster.Connection.Timeout.Duration > 0 {
		return clusterCfg.Spec.Cluster.Connection.Timeout.Duration
	}

	// Use longer timeout for Talos
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos {
		return TalosInstallTimeout
	}

	// Use longer base timeout for VCluster to account for the additional hop through
	// the syncer and the latency introduced by admission webhooks running inside the
	// virtual cluster.
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionVCluster {
		return VClusterInstallTimeout
	}

	return DefaultInstallTimeout
}

// GetArgoCDInstallTimeout returns the minimum timeout for ArgoCD installation,
// accounting for VCluster overhead. VCluster stacks receive a higher timeout
// because the syncer hop and admission-webhook latency (e.g., Gatekeeper)
// significantly delay ArgoCD component readiness.
func GetArgoCDInstallTimeout(clusterCfg *v1alpha1.Cluster) time.Duration {
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionVCluster {
		return ArgoCDVClusterInstallTimeout
	}

	return ArgoCDInstallTimeout
}
