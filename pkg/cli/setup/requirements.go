package setup

import (
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/spf13/cobra"
)

const (
	// kwokPolicyEngineWarning is emitted when a policy engine is configured but
	// cannot be installed on KWOK (admission webhooks always time out).
	kwokPolicyEngineWarning = "policy engine %q is not installed on KWOK: " +
		"admission webhook calls always time out (no real pod serves the endpoint) — skipping"

	// kwokFluxWarning is emitted when Flux is configured but cannot be fully set
	// up on KWOK. The flux-operator pod is simulated and never actually runs, so
	// it cannot process FluxInstance resources or register Flux CRDs. Waiting
	// for source.toolkit.fluxcd.io/v1 would time out after 12 minutes.
	kwokFluxWarning = "Flux is not configured on KWOK: " +
		"flux-operator pod is simulated and never registers Flux CRDs — skipping"

	// kwokCSIWarning is emitted when CSI is configured but cannot be installed
	// on KWOK. KWOK simulates pod status at the API level only — no container
	// binary runs, so CSI node-plugin DaemonSet pods would never become Ready
	// and the readiness wait would time out.
	kwokCSIWarning = "CSI is not installed on KWOK: " +
		"CSI node-plugin pods are simulated and never become Ready — skipping"

	// kwokCertManagerWarning is emitted when cert-manager is configured but
	// cannot be installed on KWOK. The cert-manager webhook pod is simulated and
	// never runs real TLS logic; calls to the admission webhook always time out.
	kwokCertManagerWarning = "cert-manager is not installed on KWOK: " +
		"webhook pod is simulated and admission webhook calls always time out — skipping"
)

// ComponentRequirements represents which components need to be installed.
type ComponentRequirements struct {
	NeedsMetricsServer      bool
	NeedsLoadBalancer       bool
	NeedsKubeletCSRApprover bool
	NeedsCSI                bool
	NeedsCertManager        bool
	NeedsPolicyEngine       bool
	NeedsClusterAutoscaler  bool
	NeedsArgoCD             bool
	NeedsFlux               bool
}

// Count returns the number of components that need to be installed.
func (r ComponentRequirements) Count() int {
	components := []bool{
		r.NeedsMetricsServer,
		r.NeedsLoadBalancer,
		r.NeedsKubeletCSRApprover,
		r.NeedsCSI,
		r.NeedsCertManager,
		r.NeedsPolicyEngine,
		r.NeedsClusterAutoscaler,
		r.NeedsArgoCD,
		r.NeedsFlux,
	}

	count := 0

	for _, needed := range components {
		if needed {
			count++
		}
	}

	return count
}

// GetComponentRequirements determines which components need to be installed based on cluster config.
func GetComponentRequirements(clusterCfg *v1alpha1.Cluster) ComponentRequirements {
	needsMetricsServer := NeedsMetricsServerInstall(clusterCfg)

	// For Talos, the kubelet-serving-cert-approver is installed during bootstrap via inlineManifests,
	// so we skip the Helm-based installation. For other distributions, we use postfinance/kubelet-csr-approver via Helm.
	needsKubeletCSRApprover := needsMetricsServer &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos

	// KWOK simulates pod status but has no real network dataplane. Policy engines
	// (Gatekeeper, Kyverno) register global MutatingWebhookConfigurations that
	// intercept ALL Kubernetes API requests. On KWOK these webhook calls always
	// time out because no real pod is serving the webhook endpoint, causing every
	// subsequent Helm install (ArgoCD, cert-manager, etc.) to fail. Skip policy
	// engine installation for KWOK entirely.
	needsPolicyEngine := clusterCfg.Spec.Cluster.PolicyEngine != v1alpha1.PolicyEngineNone &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKWOK

	// KWOK simulates pod status but cannot run real controller logic. The
	// flux-operator pod is simulated and never actually runs, so it cannot process
	// FluxInstance resources or register Flux CRDs (source.toolkit.fluxcd.io/v1).
	// SetupFluxInstance waits up to 12 minutes for those CRDs, which always times
	// out on KWOK. Skip Flux installation for KWOK entirely.
	needsFlux := clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKWOK

	// KWOK simulates pod status at the API level only — no container binary runs.
	// CSI node-plugin DaemonSet pods are simulated and never become Ready, so
	// any readiness wait would time out. Skip CSI installation for KWOK entirely.
	needsCSI := needsCSIInstall(clusterCfg) &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKWOK

	// KWOK simulates pod status but cannot run real webhook logic. cert-manager
	// registers an admission webhook that intercepts certificate-related API
	// requests; on KWOK these calls always time out because no real pod serves
	// the webhook endpoint. Skip cert-manager installation for KWOK entirely.
	needsCertManager := clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKWOK

	return ComponentRequirements{
		NeedsMetricsServer:      needsMetricsServer,
		NeedsLoadBalancer:       NeedsLoadBalancerInstall(clusterCfg),
		NeedsKubeletCSRApprover: needsKubeletCSRApprover,
		NeedsCSI:                needsCSI,
		NeedsCertManager:        needsCertManager,
		NeedsPolicyEngine:       needsPolicyEngine,
		NeedsClusterAutoscaler:  NeedsClusterAutoscalerInstall(clusterCfg),
		NeedsArgoCD:             clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD,
		NeedsFlux:               needsFlux,
	}
}

// needsCSIInstall determines if CSI needs to be installed.
//
// In general, we install CSI only when it is explicitly Enabled AND the
// distribution × provider combination does not provide it by default.
//
// Special case:
//   - Talos × Hetzner: Hetzner CSI is not pre-installed and must be installed
//     by KSail when CSI is either Default or Enabled.
func needsCSIInstall(clusterCfg *v1alpha1.Cluster) bool {
	dist := clusterCfg.Spec.Cluster.Distribution
	provider := clusterCfg.Spec.Cluster.Provider
	csiSetting := clusterCfg.Spec.Cluster.CSI

	// Special handling for Talos clusters on Hetzner:
	// According to the distribution × provider matrix, Hetzner CSI must be
	// installed by KSail for both Default and Enabled CSI settings.
	if dist == v1alpha1.DistributionTalos && provider == v1alpha1.ProviderHetzner {
		return csiSetting == v1alpha1.CSIDefault || csiSetting == v1alpha1.CSIEnabled
	}

	// Generic behavior for all other distribution × provider combinations.
	if csiSetting != v1alpha1.CSIEnabled {
		return false
	}

	// Don't install if distribution × provider provides it by default.
	return !dist.ProvidesCSIByDefault(provider)
}

// emitKWOKUnsupportedComponentWarnings emits user-visible warnings for components
// that are configured but cannot be installed on KWOK (simulated pods never run real
// controller logic). Called at the start of InstallPostCNIComponents to notify the
// user about skipped components before installation begins.
func emitKWOKUnsupportedComponentWarnings(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKWOK {
		return
	}

	if clusterCfg.Spec.Cluster.PolicyEngine != v1alpha1.PolicyEngineNone {
		notify.Warningf(cmd.OutOrStdout(), kwokPolicyEngineWarning,
			clusterCfg.Spec.Cluster.PolicyEngine,
		)
	}

	if clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux {
		notify.Warningf(cmd.OutOrStdout(), kwokFluxWarning)
	}

	if clusterCfg.Spec.Cluster.CSI == v1alpha1.CSIEnabled {
		notify.Warningf(cmd.OutOrStdout(), kwokCSIWarning)
	}

	if clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled {
		notify.Warningf(cmd.OutOrStdout(), kwokCertManagerWarning)
	}
}

// needsCloudProviderInitPhase returns true when the cluster uses an external
// cloud controller manager that must initialize nodes (remove the
// node.cloudprovider.kubernetes.io/uninitialized:NoSchedule taint) before any
// other infrastructure component can schedule pods. Currently this applies to
// Talos × Hetzner clusters where hcloud-ccm is the external CCM.
func needsCloudProviderInitPhase(
	clusterCfg *v1alpha1.Cluster,
	reqs ComponentRequirements,
) bool {
	return clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos &&
		clusterCfg.Spec.Cluster.Provider == v1alpha1.ProviderHetzner &&
		reqs.NeedsLoadBalancer
}
