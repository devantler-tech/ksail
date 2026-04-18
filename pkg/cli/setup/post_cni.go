package setup

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	registryhelpers "github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
)

const (
	fluxResourcesActivity   = "applying custom resources"
	argoCDResourcesActivity = "configuring argocd resources"

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
	case v1alpha1.DistributionEKS:
		// EKS control plane stability is managed by AWS; use the default
		// conservative threshold.
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

var (
	//nolint:gochecknoglobals // dependency injection for tests
	clusterStabilityCheckMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterStabilityCheckOverride func(context.Context, *v1alpha1.Cluster, bool) error
)

// getClusterStabilityCheckFn returns the cluster stability check function,
// using the test override if one is set.
func getClusterStabilityCheckFn() func(context.Context, *v1alpha1.Cluster, bool) error {
	clusterStabilityCheckMu.RLock()
	defer clusterStabilityCheckMu.RUnlock()

	if clusterStabilityCheckOverride != nil {
		return clusterStabilityCheckOverride
	}

	return waitForClusterStability
}

// SetClusterStabilityCheckForTests overrides the cluster stability check for testing.
// Returns a cleanup function that restores the previous check.
func SetClusterStabilityCheckForTests(
	fn func(context.Context, *v1alpha1.Cluster, bool) error,
) func() {
	clusterStabilityCheckMu.Lock()

	previous := clusterStabilityCheckOverride
	clusterStabilityCheckOverride = fn

	clusterStabilityCheckMu.Unlock()

	return func() {
		clusterStabilityCheckMu.Lock()

		clusterStabilityCheckOverride = previous

		clusterStabilityCheckMu.Unlock()
	}
}

// ShouldPushOCIArtifact determines if OCI artifact push should happen for GitOps engines.
// Returns true if Flux or ArgoCD is enabled and a local registry is configured.
func ShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	// Only push for GitOps engines that consume OCI artifacts
	engine := clusterCfg.Spec.Cluster.GitOpsEngine
	if engine != v1alpha1.GitOpsEngineFlux && engine != v1alpha1.GitOpsEngineArgoCD {
		return false
	}

	// Only push if local registry is enabled
	return clusterCfg.Spec.Cluster.LocalRegistry.Enabled()
}

// resolveClusterNameFromContext resolves the cluster name from the cluster config.
// It first attempts to parse the cluster name from Connection.Context
// (e.g., "k3d-system-test-cluster" -> "system-test-cluster").
// Falls back to the distribution's default cluster name if context is not set or parsing fails.
// The cluster name is used for constructing registry container names
// (e.g., system-test-cluster-local-registry).
func resolveClusterNameFromContext(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg == nil {
		return kindconfigmanager.DefaultClusterName
	}

	// First try to extract cluster name from the context if available
	contextName := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context)
	if contextName != "" {
		_, clusterName, err := clusterdetector.DetectDistributionFromContext(contextName)
		if err == nil && clusterName != "" {
			return clusterName
		}
	}

	// Fall back to default cluster name for the distribution
	return clusterCfg.Spec.Cluster.Distribution.DefaultClusterName()
}

// ComponentRequirements represents which components need to be installed.
type ComponentRequirements struct {
	NeedsMetricsServer      bool
	NeedsLoadBalancer       bool
	NeedsKubeletCSRApprover bool
	NeedsCSI                bool
	NeedsCertManager        bool
	NeedsPolicyEngine       bool
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

	// For Talos, the kubelet-serving-cert-approver is installed during bootstrap via extraManifests,
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

// InstallPostCNIComponents installs all post-CNI components in parallel.
// This includes metrics-server, CSI, cert-manager, and GitOps engines (Flux/ArgoCD).
// For Flux, the OCI artifact push and readiness wait happens after installation.
// cniInstalled indicates whether CNI was just installed — when true, the node
// readiness check in the stability pre-flight is skipped since waitForCNIReadiness
// already verified it.
func InstallPostCNIComponents(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	tmr timer.Timer,
	cniInstalled bool,
) error {
	reqs := GetComponentRequirements(clusterCfg)

	emitKWOKUnsupportedComponentWarnings(cmd, clusterCfg)

	if reqs.Count() == 0 {
		return nil
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		gitOpsKubeconfig    string
		gitOpsKubeconfigErr error
	)

	if reqs.NeedsArgoCD || reqs.NeedsFlux {
		_, gitOpsKubeconfig, gitOpsKubeconfigErr = factories.HelmClientFactory(clusterCfg)
		if gitOpsKubeconfigErr != nil {
			return fmt.Errorf("failed to create helm client for gitops: %w", gitOpsKubeconfigErr)
		}
	}

	err := installComponentsInPhases(ctx, cmd, clusterCfg, factories, tmr, reqs, cniInstalled)
	if err != nil {
		return err
	}

	return configureGitOpsResources(
		ctx,
		cmd,
		clusterCfg,
		factories,
		reqs,
		gitOpsKubeconfig,
	)
}

func installComponentsInPhases(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	tmr timer.Timer,
	reqs ComponentRequirements,
	cniInstalled bool,
) error {
	writer := cmd.OutOrStdout()
	labels := notify.InstallingLabels()

	infraTasks := buildInfrastructureTasks(clusterCfg, factories, reqs)
	if len(infraTasks) > 0 {
		err := runInfraPhase(ctx, clusterCfg, writer, labels, tmr, infraTasks, cniInstalled)
		if err != nil {
			return err
		}
	}

	gitopsTasks := buildGitOpsTasks(clusterCfg, factories, reqs)
	if len(gitopsTasks) > 0 {
		// After infra phase, CNI node readiness is no longer fresh — always
		// run the full stability check before GitOps installation.
		err := runGitOpsPhase(ctx, clusterCfg, writer, labels, tmr, infraTasks, gitopsTasks)
		if err != nil {
			return err
		}
	}

	return nil
}

// runInfraPhase installs Phase 1 infrastructure components (metrics-server,
// load-balancer, kubelet-csr-approver, CSI, cert-manager, policy-engine).
// For Cilium CNI, a pre-flight stability check ensures the eBPF dataplane
// has programmed pod-to-service routing before components are deployed.
// cniInstalled indicates whether CNI was just installed — when true, the node
// readiness check in the stability pre-flight is skipped.
func runInfraPhase(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	writer io.Writer,
	labels notify.ProgressLabels,
	tmr timer.Timer,
	infraTasks []notify.ProgressTask,
	cniInstalled bool,
) error {
	if needsInClusterConnectivityCheck(clusterCfg) {
		err := getClusterStabilityCheckFn()(ctx, clusterCfg, cniInstalled)
		if err != nil {
			return fmt.Errorf(
				"cluster not stable before infrastructure installation: %w", err,
			)
		}
	}

	infraGroup := notify.NewProgressGroup(
		"Installing infrastructure components",
		"📦",
		writer,
		notify.WithLabels(labels),
		notify.WithTimer(tmr),
	)

	err := infraGroup.Run(ctx, infraTasks...)
	if err != nil {
		return fmt.Errorf("failed to install infrastructure components: %w", err)
	}

	return nil
}

// runGitOpsPhase installs Phase 2 GitOps engines (ArgoCD, Flux) after
// infrastructure components are ready, if any. A stability check always runs
// before GitOps operators start, both to recover from webhook/CRD
// registrations after infrastructure installation and to guard against
// distributions (e.g. K3s/K3d) that report cluster creation success before the
// API server is fully ready. Without this guard, Helm's cluster reachability
// check can fail with "the server is currently unable to handle the request"
// when no infrastructure components are installed and the cluster was just
// created.
func runGitOpsPhase(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	writer io.Writer,
	labels notify.ProgressLabels,
	tmr timer.Timer,
	infraTasks []notify.ProgressTask,
	gitopsTasks []notify.ProgressTask,
) error {
	err := getClusterStabilityCheckFn()(ctx, clusterCfg, false)
	if err != nil {
		if len(infraTasks) > 0 {
			return fmt.Errorf(
				"cluster not stable after infrastructure installation: %w", err,
			)
		}

		return fmt.Errorf("cluster not stable before GitOps installation: %w", err)
	}

	gitopsGroup := notify.NewProgressGroup(
		"Installing GitOps engines",
		"📦",
		writer,
		notify.WithLabels(labels),
		notify.WithTimer(tmr),
	)

	err = gitopsGroup.Run(ctx, gitopsTasks...)
	if err != nil {
		return fmt.Errorf("failed to install GitOps engines: %w", err)
	}

	return nil
}

// buildInfrastructureTasks returns tasks for infrastructure components that
// should be installed before GitOps engines. This includes policy engines
// whose webhooks must be fully ready before other Helm installations begin.
func buildInfrastructureTasks(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
) []notify.ProgressTask {
	var tasks []notify.ProgressTask

	if reqs.NeedsMetricsServer {
		tasks = append(
			tasks,
			newTask("metrics-server", clusterCfg, factories, InstallMetricsServerSilent),
		)
	}

	if reqs.NeedsLoadBalancer {
		tasks = append(
			tasks,
			newTask("load-balancer", clusterCfg, factories, InstallLoadBalancerSilent),
		)
	}

	if reqs.NeedsKubeletCSRApprover {
		tasks = append(
			tasks,
			newTask("kubelet-csr-approver", clusterCfg, factories, installKubeletCSRApproverSilent),
		)
	}

	if reqs.NeedsCSI {
		tasks = append(tasks, newTask("csi", clusterCfg, factories, InstallCSISilent))
	}

	if reqs.NeedsCertManager {
		tasks = append(
			tasks,
			newTask("cert-manager", clusterCfg, factories, InstallCertManagerSilent),
		)
	}

	if reqs.NeedsPolicyEngine {
		tasks = append(
			tasks,
			newTask("policy-engine", clusterCfg, factories, InstallPolicyEngineSilent),
		)
	}

	return tasks
}

// buildGitOpsTasks returns tasks for GitOps engines that should be installed
// after infrastructure components are ready.
func buildGitOpsTasks(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
) []notify.ProgressTask {
	var tasks []notify.ProgressTask

	if reqs.NeedsArgoCD {
		tasks = append(tasks, newTask("argocd", clusterCfg, factories, InstallArgoCDSilent))
	}

	if reqs.NeedsFlux {
		tasks = append(tasks, newTask("flux", clusterCfg, factories, InstallFluxSilent))
	}

	return tasks
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

type silentInstallFunc func(ctx context.Context, cfg *v1alpha1.Cluster, f *InstallerFactories) error

func newTask(
	name string,
	cfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	fn silentInstallFunc,
) notify.ProgressTask {
	return notify.ProgressTask{
		Name: name,
		Fn:   func(ctx context.Context) error { return fn(ctx, cfg, factories) },
	}
}

func configureGitOpsResources(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
	gitOpsKubeconfig string,
) error {
	// Only show configure stage if there are GitOps resources to configure
	if !reqs.NeedsArgoCD && !reqs.NeedsFlux {
		return nil
	}

	// Resolve cluster name for registry naming
	clusterName := resolveClusterNameFromContext(clusterCfg)
	writer := cmd.OutOrStdout()

	// Show title for configure stage
	notify.WriteMessage(notify.Message{
		Type: notify.TitleType, Content: "Configuring components...", Emoji: "⚙️", Writer: writer,
	})

	// Post-install GitOps configuration
	if reqs.NeedsArgoCD {
		err := configureArgoCD(
			ctx,
			cmd,
			factories,
			gitOpsKubeconfig,
			clusterCfg,
			clusterName,
			writer,
		)
		if err != nil {
			return err
		}
	}

	if reqs.NeedsFlux {
		err := configureFlux(
			ctx,
			cmd,
			factories,
			gitOpsKubeconfig,
			clusterCfg,
			clusterName,
			writer,
		)
		if err != nil {
			return err
		}
	}

	// Show success message for configure stage
	notify.WriteMessage(
		notify.Message{Type: notify.SuccessType, Content: "components configured", Writer: writer},
	)

	return nil
}

func configureArgoCD(
	ctx context.Context,
	cmd *cobra.Command,
	factories *InstallerFactories,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) error {
	// Ensure OCI artifact exists before creating the ArgoCD Application,
	// otherwise ArgoCD enters a ComparisonError loop that can saturate etcd.
	_, err := factories.callEnsureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	if err != nil {
		return fmt.Errorf("failed to ensure OCI artifact for ArgoCD: %w", err)
	}

	notify.WriteMessage(
		notify.Message{Type: notify.ActivityType, Content: argoCDResourcesActivity, Writer: writer},
	)

	err = factories.EnsureArgoCDResources(ctx, kubeconfig, clusterCfg, clusterName)
	if err != nil {
		return fmt.Errorf("failed to configure Argo CD resources: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
		Writer:  writer,
	})

	return nil
}

func configureFlux(
	ctx context.Context,
	cmd *cobra.Command,
	factories *InstallerFactories,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) error {
	notify.WriteMessage(
		notify.Message{Type: notify.ActivityType, Content: fluxResourcesActivity, Writer: writer},
	)

	// For VCluster, resolve the registry container's Docker IP since pods inside
	// VCluster use CoreDNS which cannot resolve Docker container names.
	registryHost, resolveErr := resolveRegistryHost(ctx, clusterCfg, clusterName)
	if resolveErr != nil {
		return fmt.Errorf("resolve registry host for flux: %w", resolveErr)
	}

	// Step 1: Setup FluxInstance CR (does not wait for readiness)
	err := factories.SetupFluxInstance(ctx, kubeconfig, clusterCfg, clusterName, registryHost)
	if err != nil {
		return fmt.Errorf("failed to setup FluxInstance: %w", err)
	}

	// Step 2: Check if OCI artifact exists and push if needed
	artifactPushed, err := factories.callEnsureOCIArtifact(
		ctx,
		cmd,
		clusterCfg,
		clusterName,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to ensure OCI artifact: %w", err)
	}

	// Step 3: Wait for FluxInstance to be ready (only if artifact was pushed/exists)
	if artifactPushed {
		notify.WriteMessage(
			notify.Message{
				Type:    notify.ActivityType,
				Content: "waiting for flux to be ready",
				Writer:  writer,
			},
		)

		err = factories.WaitForFluxReady(ctx, kubeconfig)
		if err != nil {
			return fmt.Errorf("failed waiting for Flux to be ready: %w", err)
		}
	}

	return nil
}

// callEnsureOCIArtifact calls EnsureOCIArtifact if set on the factory, or falls
// back to the default ensureOCIArtifact implementation. This eliminates the
// repeated nil-guard pattern in configureArgoCD and configureFlux.
func (f *InstallerFactories) callEnsureOCIArtifact(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	if f.EnsureOCIArtifact != nil {
		return f.EnsureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	}

	return ensureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
}

// ensureOCIArtifact checks if an OCI artifact exists and pushes one if needed.
// Returns true if an artifact exists or was pushed, false if no artifact needed.
func ensureOCIArtifact(
	ctx context.Context,
	_ *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	// Only check/push for local registries
	if !clusterCfg.Spec.Cluster.LocalRegistry.Enabled() {
		return false, nil
	}

	// Resolve registry info
	registryInfo, err := registryhelpers.ResolveRegistry(
		ctx,
		registryhelpers.ResolveRegistryOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   clusterName,
		},
	)
	if err != nil {
		return false, fmt.Errorf("resolve registry: %w", err)
	}

	// Build the artifact reference details
	artifactOpts := buildArtifactExistsOptions(registryInfo, clusterCfg)

	// Check if artifact already exists
	verifier := oci.NewRegistryVerifier()

	exists, err := verifier.ArtifactExists(ctx, artifactOpts)
	if err != nil {
		// Log warning but continue - we'll try to push
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "checking for existing artifact",
			Writer:  writer,
		})
	}

	if exists {
		// Artifact already exists, no need to push
		return true, nil
	}

	return pushInitialOCIArtifact(ctx, clusterCfg, clusterName, writer)
}

// pushInitialOCIArtifact pushes an initial OCI artifact when none exists.
func pushInitialOCIArtifact(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	// Artifact doesn't exist, push an empty one
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing initial oci artifact",
		Writer:  writer,
	})

	result, err := registryhelpers.PushOCIArtifact(ctx, registryhelpers.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   clusterName,
		SourceDir:     "", // Use default from config
		Ref:           "", // Use default tag
		Validate:      clusterCfg.Spec.Workload.ValidateOnPush,
	})
	if err != nil {
		return false, fmt.Errorf("push oci artifact: %w", err)
	}

	if result.Empty {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "pushed empty kustomization (source directory not found)",
			Writer:  writer,
		})
	}

	return result.Pushed, nil
}

// buildArtifactExistsOptions creates options for checking artifact existence.
func buildArtifactExistsOptions(
	registryInfo *registryhelpers.Info,
	clusterCfg *v1alpha1.Cluster,
) oci.ArtifactExistsOptions {
	return oci.ArtifactExistsOptions{
		RegistryEndpoint: resolveRegistryEndpoint(registryInfo),
		Repository:       resolveRepository(registryInfo, clusterCfg),
		Tag:              resolveTag(registryInfo, clusterCfg),
		Username:         registryInfo.Username,
		Password:         registryInfo.Password,
		Insecure:         !clusterCfg.Spec.Cluster.LocalRegistry.IsExternal(),
	}
}

func resolveRegistryEndpoint(info *registryhelpers.Info) string {
	if info.Port > 0 {
		return net.JoinHostPort(info.Host, strconv.Itoa(int(info.Port)))
	}

	return info.Host
}

func resolveRepository(info *registryhelpers.Info, cfg *v1alpha1.Cluster) string {
	if info.Repository != "" {
		return info.Repository
	}

	sourceDir := cfg.Spec.Workload.SourceDirectory
	if sourceDir == "" {
		return v1alpha1.DefaultSourceDirectory
	}

	return sourceDir
}

// resolveTag determines the OCI artifact tag using priority-based resolution.
// Priority: workload tag > registry-embedded tag > default "dev".
func resolveTag(info *registryhelpers.Info, cfg *v1alpha1.Cluster) string {
	if cfg.Spec.Workload.Tag != "" {
		return cfg.Spec.Workload.Tag
	}

	if info.Tag != "" {
		return info.Tag
	}

	return registry.DefaultLocalArtifactTag
}
