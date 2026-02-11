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
	// extra time when running under heavy parallel load on resource-constrained nodes.
	FluxInstallTimeout = 7 * time.Minute
	// ArgoCDInstallTimeout is the timeout for ArgoCD installs, which need
	// extra time for multiple components to become ready (server, repo-server,
	// application-controller, and Redis).
	ArgoCDInstallTimeout = 10 * time.Minute
)

// GetInstallTimeout determines the timeout for component installation.
// Uses cluster connection timeout if configured, otherwise defaults to:
//   - TalosInstallTimeout (5m) for Talos distribution
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

	return DefaultInstallTimeout
}
