package setup

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
)

const (
	// apiServerStabilityTimeout is the maximum time to wait for the API server
	// to stabilize between infrastructure and GitOps installation phases.
	// Infrastructure components (MetalLB, Kyverno, cert-manager, etc.) register
	// webhooks and CRDs that can temporarily destabilize API server connectivity.
	apiServerStabilityTimeout = 2 * time.Minute

	// apiServerStabilitySuccessesDefault is the number of consecutive successful
	// API server health checks required for distributions with potentially
	// complex webhook configurations (Talos, VCluster).
	apiServerStabilitySuccessesDefault = 5

	// apiServerStabilitySuccessesFast is the reduced number of consecutive
	// successes for Vanilla/K3s distributions that have simpler webhook
	// configurations and stabilize faster after infrastructure installations.
	apiServerStabilitySuccessesFast = 3

	// daemonSetStabilityTimeout is the maximum time to wait for kube-system
	// DaemonSets (including the CNI, e.g. Cilium) to be fully ready after
	// infrastructure installations. Cilium marks pods Ready only after the BPF
	// datapath is operational; waiting ensures pod-to-service routing (e.g. to
	// the API server ClusterIP) is functional before GitOps operators start.
	daemonSetStabilityTimeout = 3 * time.Minute

	// nodeReadinessTimeout is the maximum time to wait for all cluster nodes
	// to reach condition Ready=True and for at least one node to become
	// schedulable (no NoSchedule or NoExecute taints, and not cordoned). After
	// Kind/K3d cluster creation, control-plane nodes may briefly carry a
	// NoSchedule taint, causing FailedScheduling for workload pods if they are
	// deployed before the taint clears. Two minutes accommodates CI runners
	// with variable scheduling latency.
	nodeReadinessTimeout = 2 * time.Minute

	// inClusterConnectivityTimeout is the maximum time to wait for a test pod
	// to successfully reach the API server ClusterIP from within the cluster.
	// This catches eBPF dataplane race conditions where Cilium DaemonSet pods
	// report Ready but pod-to-service routing is not yet fully programmed.
	inClusterConnectivityTimeout = 2 * time.Minute

	// inClusterConnectivityTimeoutSlow is the extended timeout for distributions
	// (e.g. VCluster) where Cilium eBPF stabilization takes longer because the
	// virtual cluster runs atop a host cluster's network layer.
	inClusterConnectivityTimeoutSlow = 3 * time.Minute

	// csrApproverReadinessTimeout is the maximum time to wait for the
	// kubelet-serving-cert-approver deployment (from Talos inlineManifests) to
	// become ready. The approver pod starts at cluster creation (before CNI),
	// so it enters CrashLoopBackOff while CNI is unavailable. When a
	// sequential pre-phase (e.g. cert-manager) precedes the main infra phase
	// and a slower CNI (e.g. Cilium) is used, the pod can be in a 160–300 s
	// backoff cycle by the time the wait starts. Ten minutes provides enough
	// headroom for the pod to exit CrashLoopBackOff, restart, and pass its
	// readiness probe even in the worst-case combination of Cilium CNI and a
	// cert-manager pre-phase.
	csrApproverReadinessTimeout = 10 * time.Minute
)

// apiServerStabilitySuccesses returns the number of consecutive successful
// API server health checks required based on the distribution and provider.
// Vanilla and K3s distributions stabilize faster after webhook registrations,
// so fewer consecutive successes are required. Talos on the Docker provider
// also uses the fast threshold because Docker containers do not experience
// the network flapping seen in cloud environments.
func apiServerStabilitySuccesses(dist v1alpha1.Distribution, prov v1alpha1.Provider) int {
	switch dist {
	case v1alpha1.DistributionVanilla, v1alpha1.DistributionK3s, v1alpha1.DistributionKWOK:
		return apiServerStabilitySuccessesFast
	case v1alpha1.DistributionTalos:
		if prov == v1alpha1.ProviderDocker || prov == "" {
			return apiServerStabilitySuccessesFast
		}

		return apiServerStabilitySuccessesDefault
	case v1alpha1.DistributionVCluster:
		return apiServerStabilitySuccessesDefault
	case v1alpha1.DistributionEKS, v1alpha1.DistributionGKE:
		// EKS and GKE control plane stability is managed by the cloud
		// provider; use the default conservative threshold.
		return apiServerStabilitySuccessesDefault
	default:
		return apiServerStabilitySuccessesDefault
	}
}

// inClusterConnectivityDeadline returns the in-cluster connectivity check
// timeout based on the distribution. VCluster runs atop a host cluster's
// network layer, so Cilium eBPF stabilization takes longer and needs an
// extended timeout.
func inClusterConnectivityDeadline(dist v1alpha1.Distribution) time.Duration {
	if dist == v1alpha1.DistributionVCluster {
		return inClusterConnectivityTimeoutSlow
	}

	return inClusterConnectivityTimeout
}

// waitForClusterStability waits for the Kubernetes API server to respond
// consistently and for kube-system DaemonSets (including the CNI) to be fully
// ready. It is used both as a pre-flight check before Phase 1 infrastructure
// installations (to prevent metrics-server panics from incomplete Cilium eBPF
// dataplane programming) and as a gate between Phase 1 and Phase 2 (to prevent
// GitOps operators from entering CrashLoopBackOff due to transient API server
// connectivity issues after infrastructure components register webhooks).
//
// For KWOK clusters, the function returns early after API server stability: KWOK
// simulates pods without running real DaemonSets or nodes, so the node-readiness
// and kube-system DaemonSet checks are not applicable and are skipped.
//
// When cniInstalled is true, the WaitForAllNodesReadyAndSchedulable check is
// skipped because waitForCNIReadiness already verified node readiness and
// schedulability moments ago.
func waitForClusterStability(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	cniInstalled bool,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("get kubeconfig path for API server check: %w", err)
	}

	clientset, err := k8s.NewClientset(
		kubeconfigPath, clusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("create clientset for API server check: %w", err)
	}

	successes := apiServerStabilitySuccesses(
		clusterCfg.Spec.Cluster.Distribution,
		clusterCfg.Spec.Cluster.Provider,
	)

	err = readiness.WaitForAPIServerStable(
		ctx, clientset, apiServerStabilityTimeout, successes,
	)
	if err != nil {
		return fmt.Errorf("wait for API server stability: %w", err)
	}

	// KWOK simulates nodes and DaemonSets as scheduled/running but never
	// executes real binaries. WaitForAllNodesReadyAndSchedulable would time out
	// because no real kubelet reports node status, and
	// WaitForNamespaceDaemonSetsReady would time out because simulated pods
	// never converge to a truly ready state. Skip both and return here.
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionKWOK {
		return nil
	}

	// Wait for all nodes to reach Ready state and for at least one node to
	// be schedulable. After Kind/K3d cluster creation or infrastructure
	// installations, control-plane nodes may briefly carry a NoSchedule taint
	// that prevents workload scheduling. Without this check, pods deployed
	// immediately after stability checks pass can hit FailedScheduling errors.
	//
	// Skipped when CNI was just installed: waitForCNIReadiness already verified
	// both node readiness and schedulability, so re-checking immediately would
	// be redundant.
	if !cniInstalled {
		err = readiness.WaitForAllNodesReadyAndSchedulable(ctx, clientset, nodeReadinessTimeout)
		if err != nil {
			return fmt.Errorf("wait for all nodes to be ready and schedulable: %w", err)
		}
	}

	// Wait for all kube-system DaemonSets (including the CNI, e.g. Cilium)
	// to be fully ready. This ensures the CNI dataplane has re-converged
	// after infrastructure installations and that pod-to-service routing
	// (e.g. to the API server ClusterIP) is functional. Without this check,
	// GitOps operator pods can start before Cilium has programmed the eBPF
	// rules for service routing, causing CrashLoopBackOff with i/o timeout
	// errors when connecting to kubernetes.default.svc:443.
	// Note: this runs sequentially after API server stability because
	// WaitForNamespaceDaemonSetsReady does not retry transient transport errors
	// (e.g. connection refused/reset); starting it before the API server is
	// confirmed stable would cause spurious failures.
	err = readiness.WaitForNamespaceDaemonSetsReady(
		ctx, clientset, "kube-system", daemonSetStabilityTimeout,
	)
	if err != nil {
		return fmt.Errorf("wait for kube-system DaemonSets to be ready: %w", err)
	}

	// Pre-flight in-cluster connectivity check: verify that the API server
	// ClusterIP is actually reachable from a pod. This check is only needed
	// for Cilium CNI where the eBPF dataplane may not have fully programmed
	// pod-to-service routing paths even after DaemonSet pods report Ready.
	// For non-Cilium CNIs (default (distribution-provided) CNI or Calico) this
	// race condition does not apply.
	if needsInClusterConnectivityCheck(clusterCfg) {
		err = readiness.WaitForInClusterAPIConnectivity(
			ctx, clientset, inClusterConnectivityDeadline(clusterCfg.Spec.Cluster.Distribution),
		)
		if err != nil {
			return fmt.Errorf("in-cluster API connectivity pre-flight check: %w", err)
		}
	}

	return nil
}

// needsInClusterConnectivityCheck returns true if the in-cluster connectivity
// check should be performed. The check was introduced to catch eBPF dataplane
// race conditions specific to Cilium, where DaemonSet pods report Ready before
// pod-to-service routing is fully programmed. For non-Cilium CNIs (e.g.,
// default (distribution-provided) CNI or Calico) this race condition does not
// apply and the check is skipped, saving up to 2 minutes of wall-clock time.
// KWOK simulates pod status but has no real network dataplane, so the check
// is meaningless and will always fail on KWOK regardless of CNI.
func needsInClusterConnectivityCheck(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionKWOK {
		return false
	}

	return clusterCfg.Spec.Cluster.CNI == v1alpha1.CNICilium
}

// waitForNodeSchedulability waits for all nodes to reach Ready state and for at
// least one node to be schedulable (no blocking taints). Used after hcloud-ccm
// installation to ensure the uninitialized taint has been removed before
// subsequent components attempt to schedule pods.
func waitForNodeSchedulability(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("get kubeconfig path for node schedulability check: %w", err)
	}

	clientset, err := k8s.NewClientset(
		kubeconfigPath, clusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("create clientset for node schedulability check: %w", err)
	}

	err = readiness.WaitForAllNodesReadyAndSchedulable(ctx, clientset, nodeReadinessTimeout)
	if err != nil {
		return fmt.Errorf("wait for nodes to become schedulable after cloud provider init: %w", err)
	}

	return nil
}

// waitForKubeletCSRApprover waits for the kubelet-serving-cert-approver deployment
// (installed via Talos inlineManifests) to be ready before starting infrastructure
// component installations.
//
// On Talos clusters with rotate-server-certificates enabled, kubelets submit CSRs
// for their serving certificates. These CSRs must be approved by a cert approver
// before metrics-server can TLS-handshake with kubelets. When CNI is managed by
// KSail (e.g., Cilium), the inlineManifests-installed approver pods can't start
// until after CNI is installed. Without this wait, metrics-server races the
// approver startup and fails with TLS errors.
//
// If the deployment does not exist (user didn't include the inlineManifests patch),
// this function returns nil immediately — it is a no-op in that case.
func waitForKubeletCSRApprover(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
) error {
	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(clusterCfg)
	if err != nil {
		return fmt.Errorf("get kubeconfig path for CSR approver check: %w", err)
	}

	clientset, err := k8s.NewClientset(
		kubeconfigPath, clusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("create clientset for CSR approver check: %w", err)
	}

	err = readiness.WaitForDeploymentReadyIfExists(
		ctx, clientset,
		"kubelet-serving-cert-approver",
		"kubelet-serving-cert-approver",
		csrApproverReadinessTimeout,
	)
	if err != nil {
		return fmt.Errorf("wait for kubelet-serving-cert-approver: %w", err)
	}

	return nil
}
