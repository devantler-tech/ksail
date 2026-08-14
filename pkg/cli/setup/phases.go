package setup

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
)

// clusterStabilityCheck returns the cluster stability check function, falling
// back to the default waitForClusterStability when the factory field is unset.
func (f *InstallerFactories) clusterStabilityCheck() func(
	context.Context, *v1alpha1.Cluster, bool,
) error {
	if f != nil && f.ClusterStabilityCheck != nil {
		return f.ClusterStabilityCheck
	}

	return waitForClusterStability
}

// nodeSchedulabilityWait returns the node schedulability wait function, falling
// back to the default waitForNodeSchedulability when the factory field is unset.
func (f *InstallerFactories) nodeSchedulabilityWait() func(
	context.Context, *v1alpha1.Cluster,
) error {
	if f != nil && f.NodeSchedulabilityWait != nil {
		return f.NodeSchedulabilityWait
	}

	return waitForNodeSchedulability
}

// waitForCSRApprover returns the CSR approver wait function, falling back to the
// default waitForKubeletCSRApprover when the factory field is unset.
func (f *InstallerFactories) waitForCSRApprover() func(
	context.Context, *v1alpha1.Cluster,
) error {
	if f != nil && f.WaitForCSRApprover != nil {
		return f.WaitForCSRApprover
	}

	return waitForKubeletCSRApprover
}

// cloudProviderInitInstall returns the load-balancer install function used by
// the cloud-provider init pre-phase, falling back to the default
// InstallLoadBalancerSilent when the factory field is unset. The override only
// affects the pre-phase; the normal parallel infra path uses
// InstallLoadBalancerSilent directly.
func (f *InstallerFactories) cloudProviderInitInstall() silentInstallFunc {
	if f != nil && f.CloudProviderInitInstall != nil {
		return f.CloudProviderInitInstall
	}

	return InstallLoadBalancerSilent
}

// runCloudProviderInitPhase installs the cloud controller manager (hcloud-ccm)
// and waits for nodes to become schedulable. The CCM chart includes a built-in
// toleration for the uninitialized taint, so it can schedule on tainted nodes.
// Once running, the CCM initializes nodes by assigning provider IDs and removing
// the taint. We wait for at least one node to become schedulable before
// returning, so subsequent components (cert-manager, metrics-server, etc.) can
// schedule without hitting FailedScheduling.
func runCloudProviderInitPhase(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	writer io.Writer,
	labels notify.ProgressLabels,
	tmr timer.Timer,
	factories *InstallerFactories,
	cniInstalled bool,
) error {
	ccmTask := newTask("load-balancer", clusterCfg, factories, factories.cloudProviderInitInstall())

	err := runInfraPhase(
		ctx, clusterCfg, writer, labels, tmr, factories,
		[]notify.ProgressTask{ccmTask},
		cniInstalled, false,
	)
	if err != nil {
		return err
	}

	err = factories.nodeSchedulabilityWait()(ctx, clusterCfg)
	if err != nil {
		return fmt.Errorf(
			"nodes not schedulable after cloud provider initialization: %w", err,
		)
	}

	return nil
}

// runCloudProviderInitAndClearReqs runs the cloud-provider init pre-phase and
// returns updated requirements with NeedsLoadBalancer cleared and cniInstalled
// set to false so subsequent phases include the node readiness check in their
// stability pre-flight (the in-cluster connectivity check is separate and only
// runs for Cilium CNI).
func runCloudProviderInitAndClearReqs(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	writer io.Writer,
	labels notify.ProgressLabels,
	tmr timer.Timer,
	factories *InstallerFactories,
	reqs ComponentRequirements,
	cniInstalled bool,
) (ComponentRequirements, bool, error) {
	err := runCloudProviderInitPhase(
		ctx, clusterCfg, writer, labels, tmr, factories, cniInstalled,
	)
	if err != nil {
		return reqs, cniInstalled, err
	}

	reqs.NeedsLoadBalancer = false

	return reqs, false, nil
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

	return runWithReservedSandboxMonitor(
		ctx,
		clusterCfg,
		factories,
		func(runCtx context.Context) error {
			err := installComponentsInPhases(
				runCtx,
				cmd,
				clusterCfg,
				factories,
				tmr,
				reqs,
				cniInstalled,
			)
			if err != nil {
				return err
			}

			return configureGitOpsResources(
				runCtx,
				cmd,
				clusterCfg,
				factories,
				reqs,
				gitOpsKubeconfig,
			)
		},
	)
}

//nolint:cyclop,funlen // Phase orchestration is inherently branchy; each phase gate is a distinct concern.
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

	// On Hetzner clusters, all nodes carry the
	// node.cloudprovider.kubernetes.io/uninitialized:NoSchedule taint until the
	// external cloud controller manager (hcloud-ccm) initializes them. Install
	// hcloud-ccm first and wait for nodes to become schedulable before any other
	// infrastructure component, otherwise all pods fail with FailedScheduling.
	if needsCloudProviderInitPhase(clusterCfg, reqs) {
		var err error

		reqs, cniInstalled, err = runCloudProviderInitAndClearReqs(
			ctx, clusterCfg, writer, labels, tmr, factories, reqs, cniInstalled,
		)
		if err != nil {
			return err
		}
	}

	// When cert-manager and a policy engine are both needed, install cert-manager
	// first in its own sequential phase before the parallel infrastructure phase.
	// This ensures cert-manager is fully ready (webhook + cainjector up) when the
	// policy engine starts — otherwise cert issuance for Kyverno's webhook TLS can
	// take 15-20+ minutes because cert-manager is still initialising.
	if reqs.NeedsCertManager && reqs.NeedsPolicyEngine {
		certManagerTask := newTask("cert-manager", clusterCfg, factories, InstallCertManagerSilent)

		err := runInfraPhase(
			ctx, clusterCfg, writer, labels, tmr, factories,
			[]notify.ProgressTask{certManagerTask},
			cniInstalled, false,
		)
		if err != nil {
			return err
		}

		// cert-manager is installed; exclude it from the parallel infra phase below.
		// Also clear cniInstalled so the main phase runs the full stability check
		// (node readiness + kube-system DaemonSets) rather than the abbreviated one.
		reqs.NeedsCertManager = false
		cniInstalled = false
	}

	infraTasks := buildInfrastructureTasks(clusterCfg, factories, reqs)
	if len(infraTasks) > 0 {
		needsCSRApproverWait := reqs.NeedsMetricsServer &&
			clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos

		err := runInfraPhase(
			ctx,
			clusterCfg,
			writer,
			labels,
			tmr,
			factories,
			infraTasks,
			cniInstalled,
			needsCSRApproverWait,
		)
		if err != nil {
			return err
		}
	}

	gitopsTasks := buildGitOpsTasks(clusterCfg, factories, reqs)
	if len(gitopsTasks) > 0 {
		// After infra phase, CNI node readiness is no longer fresh — always
		// run the full stability check before GitOps installation.
		err := runGitOpsPhase(
			ctx, clusterCfg, writer, labels, tmr, factories, infraTasks, gitopsTasks,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// runInfraPhase runs a set of infrastructure tasks in parallel, preceded by a
// pre-flight stability check for Cilium CNI and an optional CSR approver wait.
// It is safe to call multiple times (e.g., once for cert-manager, once for the
// remaining components). The stability check completes quickly when the cluster
// is already stable after a previous call.
// cniInstalled indicates whether CNI was just installed — when true, the node
// readiness check in the stability pre-flight is skipped.
// needsCSRApproverWait indicates whether to wait for the kubelet-serving-cert-approver
// deployment (from Talos inlineManifests) to be ready before starting the parallel
// infrastructure installations. This prevents the race condition where metrics-server
// starts before kubelet serving CSRs are approved.
func runInfraPhase(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	writer io.Writer,
	labels notify.ProgressLabels,
	tmr timer.Timer,
	factories *InstallerFactories,
	infraTasks []notify.ProgressTask,
	cniInstalled bool,
	needsCSRApproverWait bool,
) error {
	if needsInClusterConnectivityCheck(clusterCfg) {
		err := factories.clusterStabilityCheck()(ctx, clusterCfg, cniInstalled)
		if err != nil {
			return fmt.Errorf(
				"cluster not stable before infrastructure installation: %w", err,
			)
		}
	}

	if needsCSRApproverWait {
		err := factories.waitForCSRApprover()(ctx, clusterCfg)
		if err != nil {
			return fmt.Errorf(
				"kubelet CSR approver not ready before infrastructure installation: %w", err,
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
	factories *InstallerFactories,
	infraTasks []notify.ProgressTask,
	gitopsTasks []notify.ProgressTask,
) error {
	err := factories.clusterStabilityCheck()(ctx, clusterCfg, false)
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
	return tasksFromEntries(clusterCfg, factories, []componentTask{
		{needed: reqs.NeedsMetricsServer, name: "metrics-server", fn: InstallMetricsServerSilent},
		{needed: reqs.NeedsLoadBalancer, name: "load-balancer", fn: InstallLoadBalancerSilent},
		{
			needed: reqs.NeedsKubeletCSRApprover,
			name:   "kubelet-csr-approver",
			fn:     installKubeletCSRApproverSilent,
		},
		{needed: reqs.NeedsCSI, name: "csi", fn: InstallCSISilent},
		{needed: reqs.NeedsCertManager, name: "cert-manager", fn: InstallCertManagerSilent},
		{needed: reqs.NeedsPolicyEngine, name: "policy-engine", fn: InstallPolicyEngineSilent},
		{
			needed: reqs.NeedsClusterAutoscaler,
			name:   "cluster-autoscaler",
			fn:     InstallClusterAutoscalerSilent,
		},
	})
}

// buildGitOpsTasks returns tasks for GitOps engines that should be installed
// after infrastructure components are ready.
func buildGitOpsTasks(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
) []notify.ProgressTask {
	return tasksFromEntries(clusterCfg, factories, []componentTask{
		{needed: reqs.NeedsArgoCD, name: "argocd", fn: InstallArgoCDSilent},
		{needed: reqs.NeedsFlux, name: "flux", fn: InstallFluxSilent},
	})
}

type silentInstallFunc func(ctx context.Context, cfg *v1alpha1.Cluster, f *InstallerFactories) error

// componentTask pairs a "needed" gate with a component name and its install function.
// It is used to build the install-task lists in buildInfrastructureTasks and buildGitOpsTasks.
type componentTask struct {
	needed bool
	name   string
	fn     silentInstallFunc
}

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

// tasksFromEntries converts a slice of componentTask entries into ProgressTasks,
// including only the entries whose needed field is true.
func tasksFromEntries(
	cfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	entries []componentTask,
) []notify.ProgressTask {
	var tasks []notify.ProgressTask

	for _, e := range entries {
		if e.needed {
			tasks = append(tasks, newTask(e.name, cfg, factories, e.fn))
		}
	}

	return tasks
}
