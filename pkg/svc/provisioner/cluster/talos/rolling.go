package talosprovisioner

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kubedrain "k8s.io/kubectl/pkg/drain"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// drainTimeout is the maximum duration to wait for pod eviction during node drain.
const drainTimeout = 120 * time.Second

// nodeReadinessTimeout is the timeout for waiting for a single node to become ready
// after reboot.
const nodeReadinessTimeout = 10 * time.Minute

// cordonNode marks a Kubernetes node as unschedulable.
func (p *Provisioner) cordonNode(ctx context.Context, clientset kubernetes.Interface, nodeName string) error {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeName, err)
	}

	helper := kubedrain.NewCordonHelper(node)
	if !helper.UpdateIfRequired(true) {
		return nil // already cordoned
	}

	patchErr, updateErr := helper.PatchOrReplaceWithContext(ctx, clientset, false)
	if patchErr != nil {
		return fmt.Errorf("cordon node %s (patch): %w", nodeName, patchErr)
	}

	if updateErr != nil {
		return fmt.Errorf("cordon node %s (update): %w", nodeName, updateErr)
	}

	return nil
}

// drainNode evicts all pods from a Kubernetes node.
func (p *Provisioner) drainNode(ctx context.Context, clientset kubernetes.Interface, nodeName string) error {
	drainer := &kubedrain.Helper{
		Ctx:                 ctx,
		Client:              clientset,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Timeout:             drainTimeout,
		GracePeriodSeconds:  -1, // use pod's terminationGracePeriodSeconds
		Out:                 p.logWriter,
		ErrOut:              p.logWriter,
	}

	pods, errs := drainer.GetPodsForDeletion(nodeName)
	if len(errs) > 0 {
		return fmt.Errorf("%w on %s: %v", ErrDrainPodRetrieval, nodeName, errs)
	}

	deleteErr := drainer.DeleteOrEvictPods(pods.Pods())
	if deleteErr != nil {
		return fmt.Errorf("drain node %s: %w", nodeName, deleteErr)
	}

	return nil
}

// uncordonNode marks a Kubernetes node as schedulable.
func (p *Provisioner) uncordonNode(ctx context.Context, clientset kubernetes.Interface, nodeName string) error {
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", nodeName, err)
	}

	helper := kubedrain.NewCordonHelper(node)
	if !helper.UpdateIfRequired(false) {
		return nil // already uncordoned
	}

	patchErr, updateErr := helper.PatchOrReplaceWithContext(ctx, clientset, false)
	if patchErr != nil {
		return fmt.Errorf("uncordon node %s (patch): %w", nodeName, patchErr)
	}

	if updateErr != nil {
		return fmt.Errorf("uncordon node %s (update): %w", nodeName, updateErr)
	}

	return nil
}

// rebootNode sends a reboot request to a Talos node via the Talos API.
func (p *Provisioner) rebootNode(ctx context.Context, nodeIP string) error {
	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return fmt.Errorf("create talos client for reboot %s: %w", nodeIP, err)
	}

	defer talosClient.Close() //nolint:errcheck

	if rebootErr := talosClient.Reboot(ctx); rebootErr != nil {
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
			if (addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP) && addr.Address == nodeIP {
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

	sort.Slice(workers, func(i, j int) bool { return workers[i].IP < workers[j].IP })
	sort.Slice(controlPlanes, func(i, j int) bool { return controlPlanes[i].IP < controlPlanes[j].IP })

	ordered := make([]nodeWithRole, 0, len(workers)+len(controlPlanes))
	ordered = append(ordered, workers...)
	ordered = append(ordered, controlPlanes...)

	return ordered
}

// rollingApplyRebootChanges applies config changes with STAGED mode and performs
// a rolling reboot across all cluster nodes. Workers are rebooted first to minimize
// control-plane disruption. For each node: cordon → drain → apply config (STAGED) →
// reboot → wait for Ready → uncordon.
//
//nolint:cyclop // sequential rolling-reboot workflow with cordon/drain/reboot/wait/uncordon
func (p *Provisioner) rollingApplyRebootChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	kubeconfigPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("expand kubeconfig path: %w", err)
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	kubeconfigContext := p.options.KubeconfigContext
	if kubeconfigContext == "" {
		kubeconfigContext = "admin@" + clusterName
	}

	clientset, err := k8s.NewClientset(canonicalPath, kubeconfigContext)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("list nodes for rolling reboot: %w", err)
	}

	ordered := sortNodesWorkersFirst(nodes)

	for i, node := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Rolling reboot for %s (%s)...\n",
			i+1, len(ordered), node.IP, node.Role,
		)

		rebootErr := p.rollingRebootSingleNode(ctx, clientset, node)
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
// wait → uncordon sequence for a single node.
//
//nolint:cyclop // sequential cordon/drain/stage/reboot/wait/uncordon steps
func (p *Provisioner) rollingRebootSingleNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	node nodeWithRole,
) error {
	nodeName, err := p.resolveNodeName(ctx, clientset, node.IP)
	if err != nil {
		return fmt.Errorf("resolve node name: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Cordoning %s (%s)...\n", nodeName, node.IP)

	cordonErr := p.cordonNode(ctx, clientset, nodeName)
	if cordonErr != nil {
		return fmt.Errorf("cordon: %w", cordonErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Draining %s...\n", nodeName)

	drainErr := p.drainNode(ctx, clientset, nodeName)
	if drainErr != nil {
		return fmt.Errorf("drain: %w", drainErr)
	}

	// Apply config with STAGED mode — config takes effect on next reboot.
	if p.talosConfigs != nil {
		config := p.talosConfigs.ControlPlane()
		if node.Role == RoleWorker {
			config = p.talosConfigs.Worker()
		}

		if config != nil {
			_, _ = fmt.Fprintf(p.logWriter, "    Staging config on %s...\n", node.IP)

				stageErr := p.applyConfigWithMode(
					ctx, node.IP, config,
					machineapi.ApplyConfigurationRequest_STAGED,
				)
				if stageErr != nil {
					return fmt.Errorf("stage config: %w", stageErr)
				}
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Rebooting %s...\n", node.IP)

	rebootErr := p.rebootNode(ctx, node.IP)
	if rebootErr != nil {
		return fmt.Errorf("reboot: %w", rebootErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to become ready...\n", nodeName)

	if err := p.waitForK8sNodeReady(ctx, clientset, nodeName, nodeReadinessTimeout); err != nil {
		return fmt.Errorf("wait for ready: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Uncordoning %s...\n", nodeName)

	if err := p.uncordonNode(ctx, clientset, nodeName); err != nil {
		return fmt.Errorf("uncordon: %w", err)
	}

	return nil
}
