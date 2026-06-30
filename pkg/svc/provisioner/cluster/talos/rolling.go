package talosprovisioner

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kubedrain "k8s.io/kubectl/pkg/drain"
)

// defaultDrainTimeout is the maximum duration to wait for pod eviction during a
// node drain when spec.cluster.talos.drainTimeout is not set. The previous value
// of two minutes was too aggressive for production clusters: graceful eviction of
// PodDisruptionBudget-protected workloads (e.g. Longhorn instance-managers whose
// replicas must rebuild elsewhere, or databases that must fail over) routinely
// takes several minutes, and overrunning it aborted the whole update. It is
// aligned with nodeReadinessTimeout.
const defaultDrainTimeout = 10 * time.Minute

// drainSkipWaitForDeleteSeconds tells the drain helper to stop waiting on a pod
// once its DeletionTimestamp is older than this many seconds. The node is about to
// be rebooted or destroyed, so a pod that has accepted eviction but is slow to
// terminate (e.g. a Job pod stuck in Terminating) must not consume the entire
// drain budget while the rest of the node waits behind it.
const drainSkipWaitForDeleteSeconds = 60

// nodeReadinessTimeout is the timeout for waiting for a single node to become ready
// after reboot.
const nodeReadinessTimeout = 10 * time.Minute

// uncordonCleanupTimeout bounds the best-effort uncordon performed after a drain
// failure (see cordonAndDrain). It runs on a context detached from the (possibly
// already-cancelled or unreachable-API) parent, so it needs its own deadline to
// avoid hanging an already-failing update.
const uncordonCleanupTimeout = 30 * time.Second

// setNodeSchedulable marks a Kubernetes node as schedulable or unschedulable.
func (p *Provisioner) setNodeSchedulable(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
	schedulable bool,
) error {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeName, err)
	}

	helper := kubedrain.NewCordonHelper(node)
	if !helper.UpdateIfRequired(!schedulable) {
		return nil // already in desired state
	}

	patchErr, updateErr := helper.PatchOrReplaceWithContext(ctx, clientset, false)
	if patchErr != nil {
		return fmt.Errorf(
			"set schedulable=%v on node %s (patch): %w",
			schedulable,
			nodeName,
			patchErr,
		)
	}

	if updateErr != nil {
		return fmt.Errorf(
			"set schedulable=%v on node %s (update): %w",
			schedulable,
			nodeName,
			updateErr,
		)
	}

	return nil
}

// cordonNode marks a Kubernetes node as unschedulable.
func (p *Provisioner) cordonNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	return p.setNodeSchedulable(ctx, clientset, nodeName, false)
}

// uncordonNode marks a Kubernetes node as schedulable.
func (p *Provisioner) uncordonNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	return p.setNodeSchedulable(ctx, clientset, nodeName, true)
}

// drainTimeout returns the configured per-node drain timeout, falling back to
// defaultDrainTimeout when unset or non-positive.
func (p *Provisioner) drainTimeout() time.Duration {
	if p.options != nil && p.options.DrainTimeout > 0 {
		return p.options.DrainTimeout
	}

	return defaultDrainTimeout
}

// newDrainHelper builds the kubectl drain helper used by drainNode. It is split
// out so the timeout / skip-wait / eviction-bypass wiring can be unit-tested
// without a live cluster. When disableEviction is true the helper deletes pods
// directly instead of going through the Eviction API, bypassing
// PodDisruptionBudgets (see drainNode for when that is enabled).
func newDrainHelper(
	ctx context.Context,
	clientset kubernetes.Interface,
	timeout time.Duration,
	disableEviction bool,
	logWriter io.Writer,
) *kubedrain.Helper {
	return &kubedrain.Helper{
		Ctx:    ctx,
		Client: clientset,
		// Force is kubectl-drain's flag to also remove standalone pods not backed by
		// a controller; it is unrelated to KSail's --force (which sets disableEviction).
		Force:                           true,
		IgnoreAllDaemonSets:             true,
		DeleteEmptyDirData:              true,
		Timeout:                         timeout,
		GracePeriodSeconds:              -1, // use pod's terminationGracePeriodSeconds
		SkipWaitForDeleteTimeoutSeconds: drainSkipWaitForDeleteSeconds,
		DisableEviction:                 disableEviction,
		Out:                             logWriter,
		ErrOut:                          logWriter,
	}
}

// drainNode evicts all pods from a Kubernetes node. The wait budget is
// p.drainTimeout(); pods already terminating past drainSkipWaitForDeleteSeconds
// no longer block it. When p.drainForce is set (an explicit --force/--yes on the
// update) the drain deletes pods directly, bypassing PodDisruptionBudgets so the
// roll can complete even when a budget would never allow graceful eviction (e.g.
// a single-replica StatefulSet) — at the cost of the disruption the budget
// guards against.
func (p *Provisioner) drainNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) error {
	timeout := p.drainTimeout()
	drainer := newDrainHelper(ctx, clientset, timeout, p.drainForce, p.logWriter)

	pods, errs := drainer.GetPodsForDeletion(nodeName)
	if len(errs) > 0 {
		return fmt.Errorf("%w on %s: %v", ErrDrainPodRetrieval, nodeName, errs)
	}

	deleteErr := drainer.DeleteOrEvictPods(pods.Pods())
	if deleteErr != nil {
		// Don't suggest --force when it is already in effect (drainForce set).
		hint := ""
		if !p.drainForce {
			hint = "; raise spec.cluster.talos.drainTimeout (or --drain-timeout) to give " +
				"workloads more time, or re-run with --force-drain to delete pods bypassing " +
				"PodDisruptionBudgets"
		}

		return fmt.Errorf("drain node %s (timeout %s): %w%s", nodeName, timeout, deleteErr, hint)
	}

	return nil
}

// rebootNode sends a reboot request to a Talos node via the Talos API. Reboot is
// not idempotent (a lost ack must not trigger a second reboot), so the transient
// apid handshake race is absorbed by the Version probe inside
// dialTalosClientWithRetry and the Reboot RPC itself is issued exactly once.
func (p *Provisioner) rebootNode(ctx context.Context, nodeIP string) error {
	talosClient, err := p.dialTalosClientWithRetry(ctx, nodeIP, "reboot connect")
	if err != nil {
		return fmt.Errorf("create talos client for reboot %s: %w", nodeIP, err)
	}

	defer talosClient.Close() //nolint:errcheck

	rebootErr := talosClient.Reboot(ctx)
	if rebootErr != nil {
		return fmt.Errorf("reboot node %s: %w", nodeIP, rebootErr)
	}

	return nil
}

// waitForK8sNodeReady polls until a specific Kubernetes node has condition Ready=True.
func (p *Provisioner) waitForK8sNodeReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
	timeout time.Duration,
) error {
	pollErr := readiness.PollForReadiness(ctx, timeout, func(ctx context.Context) (bool, error) {
		node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})
	if pollErr != nil {
		return fmt.Errorf("wait for node %s readiness: %w", nodeName, pollErr)
	}

	return nil
}

// resolveNodeName maps a Talos node IP address to its Kubernetes node name.
// It searches all nodes for one with a matching InternalIP or ExternalIP address.
func (p *Provisioner) resolveNodeName(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeIP string,
) (string, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list nodes for IP lookup: %w", err)
	}

	for i := range nodes.Items {
		for _, addr := range nodes.Items[i].Status.Addresses {
			if (addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP) &&
				addr.Address == nodeIP {
				return nodes.Items[i].Name, nil
			}
		}
	}

	return "", fmt.Errorf("%w: %s", ErrNodeNotFoundByIP, nodeIP)
}

// sortNodesWorkersFirst returns nodes sorted with workers first, then control-planes.
// Within each group, nodes are sorted by IP for deterministic ordering.
func sortNodesWorkersFirst(nodes []nodeWithRole) []nodeWithRole {
	var workers, controlPlanes []nodeWithRole

	for _, n := range nodes {
		switch n.Role {
		case RoleWorker:
			workers = append(workers, n)
		case RoleControlPlane:
			controlPlanes = append(controlPlanes, n)
		}
	}

	slices.SortFunc(workers, func(a, b nodeWithRole) int { return strings.Compare(a.IP, b.IP) })
	slices.SortFunc(
		controlPlanes,
		func(a, b nodeWithRole) int { return strings.Compare(a.IP, b.IP) },
	)

	ordered := make([]nodeWithRole, 0, len(workers)+len(controlPlanes))
	ordered = append(ordered, workers...)
	ordered = append(ordered, controlPlanes...)

	return ordered
}

// resolveKubeconfig expands and canonicalizes the provisioner's kubeconfig path and
// resolves the context for clusterName (defaulting to "admin@<clusterName>"). It is
// shared by createK8sClient and the dynamic client used by the storage-health gate so
// both reach the same cluster the same way.
func (p *Provisioner) resolveKubeconfig(clusterName string) (string, string, error) {
	kubeconfigPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return "", "", fmt.Errorf("expand kubeconfig path: %w", err)
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	kubeconfigContext := p.options.KubeconfigContext
	if kubeconfigContext == "" {
		kubeconfigContext = "admin@" + clusterName
	}

	return canonicalPath, kubeconfigContext, nil
}

// createK8sClient creates a Kubernetes clientset using the provisioner's kubeconfig
// path and the appropriate context for the given cluster name. The kubeconfig path
// is expanded and canonicalized for path safety.
func (p *Provisioner) createK8sClient(clusterName string) (kubernetes.Interface, error) {
	canonicalPath, kubeconfigContext, err := p.resolveKubeconfig(clusterName)
	if err != nil {
		return nil, err
	}

	clientset, err := k8s.NewClientset(canonicalPath, kubeconfigContext)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return clientset, nil
}

// rollingApplyRebootChanges applies config changes with STAGED mode and performs
// a rolling reboot across all cluster nodes. Workers are rebooted first to minimize
// control-plane disruption. For each node: cordon → drain → apply config (STAGED) →
// reboot → wait for Ready → uncordon.
func (p *Provisioner) rollingApplyRebootChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	clientset, err := p.createK8sClient(clusterName)
	if err != nil {
		return err
	}

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("list nodes for rolling reboot: %w", err)
	}

	ordered := sortNodesWorkersFirst(nodes)

	// The staged-config rebuild needs the cluster PKI, which only a control-plane
	// node carries. Resolve one control-plane config up front and reuse it as the
	// secrets source for every node — seeding the rebuild from a worker's own config
	// fails with "failed to parse PEM block" (#4963). All control-planes share the
	// same PKI, so any one is a valid source (mirrors applyInPlaceConfigChanges).
	secretsSource := p.fetchSecretsSource(ctx, clusterName)

	// Build the between-node storage-health prober once (it is reused for every node).
	// It is nil when the gate is disabled (spec.cluster.talos.storageHealthTimeout
	// unset) or no replicated storage backend is detected, in which case the gate
	// no-ops. A construction error degrades gracefully: warn and proceed without the
	// gate rather than aborting the roll.
	storageProber := p.buildStorageHealthProberOrWarn(ctx, clientset, clusterName)

	for i, node := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Rolling reboot for %s (%s)...\n",
			i+1, len(ordered), node.IP, node.Role,
		)

		rebootErr := p.rollingRebootSingleNode(ctx, clientset, node, secretsSource, storageProber)
		if rebootErr != nil {
			recordFailedChange(result, node.Role, node.IP, rebootErr)

			return fmt.Errorf("rolling reboot node %s (%s): %w", node.IP, node.Role, rebootErr)
		}

		result.RebootsPerformed++

		recordAppliedChange(result, node.Role, node.IP, "rebooted")

		_, _ = fmt.Fprintf(p.logWriter,
			"  ✓ Node %s (%s) rebooted successfully\n",
			node.IP, node.Role,
		)
	}

	return nil
}

// rollingRebootSingleNode performs the cordon → drain → stage config → reboot →
// wait → uncordon sequence for a single node. secretsSource is a control-plane
// config supplying the cluster PKI for the staged-config rebuild (see
// stageNodeConfigForReboot). prober, when non-nil, gates progression to the next
// node on replicated-storage volume health after the node is Ready and uncordoned
// (see waitForStorageHealthy); a nil prober skips the gate.
func (p *Provisioner) rollingRebootSingleNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	node nodeWithRole,
	secretsSource talosconfig.Provider,
	prober storageHealthProber,
) error {
	nodeName, err := p.resolveNodeName(ctx, clientset, node.IP)
	if err != nil {
		return fmt.Errorf("resolve node name: %w", err)
	}

	drainErr := p.cordonAndDrain(ctx, clientset, nodeName)
	if drainErr != nil {
		return drainErr
	}

	stageErr := p.stageNodeConfigForReboot(ctx, node, secretsSource)
	if stageErr != nil {
		return stageErr
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Rebooting %s...\n", node.IP)

	rebootErr := p.rebootNode(ctx, node.IP)
	if rebootErr != nil {
		return fmt.Errorf("reboot: %w", rebootErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to become ready...\n", nodeName)

	waitErr := p.waitForK8sNodeReady(ctx, clientset, nodeName, nodeReadinessTimeout)
	if waitErr != nil {
		return fmt.Errorf("wait for ready: %w", waitErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Uncordoning %s...\n", nodeName)

	uncordonErr := p.uncordonNode(ctx, clientset, nodeName)
	if uncordonErr != nil {
		return fmt.Errorf("uncordon: %w", uncordonErr)
	}

	// Before advancing to the next node, optionally wait for replicated-storage
	// volumes to recover so a one-replica-per-node volume is not faulted by rebooting
	// consecutive replica holders before a rebuild completes (#5467). No-op when the
	// gate is disabled or no backend was detected (prober == nil).
	storageErr := p.waitForStorageHealthy(ctx, prober, p.storageHealthTimeout())
	if storageErr != nil {
		return fmt.Errorf("storage health gate: %w", storageErr)
	}

	return nil
}

// stageNodeConfigForReboot stages the node's desired machine config with STAGED
// mode, so it takes effect on the next reboot. The config is rebuilt from the
// node's running config through buildDesiredNodeConfig — the same machinery the
// in-place reconcile uses — so the per-node sections ksail injects post-generation
// at create/scale (static hostname, registry mirrors, cert SANs) survive the
// reboot. Staging the raw regenerated talosConfigs.ControlPlane()/Worker() instead
// would silently revert them: e.g. dropping machine.network.hostname so a Hetzner
// node re-registers under a generated talos-xxxxx name once it reboots — the same
// class of bug fixed for the in-place path in detect_inplace.go (graftNodeHostname).
// It is a no-op when no Talos config is loaded (nothing to stage).
func (p *Provisioner) stageNodeConfigForReboot(
	ctx context.Context,
	node nodeWithRole,
	secretsSource talosconfig.Provider,
) error {
	return p.applyDesiredNodeConfig(
		ctx, node, secretsSource,
		machineapi.ApplyConfigurationRequest_STAGED, "Staging",
	)
}

// applyDesiredNodeConfig rebuilds a node's desired machine config (see
// fetchAndBuildDesiredNodeConfig) and applies it with the given mode, logging the
// step with actionLabel (e.g. "Staging", "Reconciling"). It is the shared core of
// the two per-node config-application paths: the rolling-reboot path STAGES the
// config (stageNodeConfigForReboot) and the pre-upgrade reconcile applies it
// NO_REBOOT (reconcileNodeConfigBeforeUpgrade). It is a no-op when no Talos config is
// loaded (nothing to apply). secretsSource supplies the cluster PKI and must be a
// control-plane config (see buildDesiredNodeConfig and #4963).
func (p *Provisioner) applyDesiredNodeConfig(
	ctx context.Context,
	node nodeWithRole,
	secretsSource talosconfig.Provider,
	mode machineapi.ApplyConfigurationRequest_Mode,
	actionLabel string,
) error {
	if p.talosConfigs == nil {
		return nil
	}

	desired, err := p.fetchAndBuildDesiredNodeConfig(ctx, node, secretsSource)
	if err != nil {
		return fmt.Errorf("build desired config: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    %s config on %s...\n", actionLabel, node.IP)

	err = p.applyConfigWithMode(ctx, node.IP, desired, mode)
	if err != nil {
		return fmt.Errorf("apply config: %w", err)
	}

	return nil
}

// fetchAndBuildDesiredNodeConfig fetches a node's running config and rebuilds the
// machine config KSail wants on it through buildDesiredNodeConfig, which preserves
// the per-node post-generation transforms (static hostname, registry mirrors, cert
// SANs) instead of reverting to the freshly regenerated base config. The caller
// decides how to apply the result: the rolling-reboot path STAGES it
// (stageNodeConfigForReboot), while the pre-upgrade reconcile applies it NO_REBOOT
// so the Talos installer validates the desired config (reconcileNodeConfigBeforeUpgrade,
// #5294). secretsSource supplies the cluster PKI and must be a control-plane config
// (see buildDesiredNodeConfig and #4963).
func (p *Provisioner) fetchAndBuildDesiredNodeConfig(
	ctx context.Context,
	node nodeWithRole,
	secretsSource talosconfig.Provider,
) (talosconfig.Provider, error) {
	running, err := p.nodeConfigFetcher(ctx, node.IP)
	if err != nil {
		return nil, fmt.Errorf("fetch running config: %w", err)
	}

	desired, err := p.buildDesiredNodeConfig(running, secretsSource, node.Role)
	if err != nil {
		return nil, fmt.Errorf("build desired config: %w", err)
	}

	return desired, nil
}
