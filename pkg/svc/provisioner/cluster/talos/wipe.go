package talosprovisioner

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"k8s.io/client-go/kubernetes"
)

// maintenanceWaitTimeout is the maximum duration to wait for a node to enter
// maintenance mode after a STATE partition reset.
const maintenanceWaitTimeout = 10 * time.Minute

// resetNode sends a Talos Reset request to wipe specific partition labels on a node.
// The node will reboot after the reset if reboot is true.
func (p *Provisioner) resetNode(
	ctx context.Context,
	nodeIP string,
	systemLabelsToWipe []string,
	reboot bool,
) error {
	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return fmt.Errorf("failed to create Talos client for reset: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	_, _ = fmt.Fprintf(p.logWriter, "    Resetting node %s (wipe: %v, reboot: %v)\n",
		nodeIP, systemLabelsToWipe, reboot)

	partitions := make([]*machineapi.ResetPartitionSpec, 0, len(systemLabelsToWipe))
	for _, label := range systemLabelsToWipe {
		partitions = append(partitions, &machineapi.ResetPartitionSpec{
			Label: label,
			Wipe:  true,
		})
	}

	err = talosClient.ResetGeneric(ctx, &machineapi.ResetRequest{
		SystemPartitionsToWipe: partitions,
		Reboot:                reboot,
		Graceful:              true,
	})
	if err != nil {
		return fmt.Errorf("failed to reset node %s: %w", nodeIP, err)
	}

	return nil
}

// applyConfigInsecure applies configuration to a node in maintenance mode.
// During maintenance mode, the node's API is available but without TLS,
// so we need an insecure client connection.
func (p *Provisioner) applyConfigInsecure(
	ctx context.Context,
	nodeIP string,
	config talosconfig.Provider,
) error {
	if config == nil {
		return fmt.Errorf("insecure apply on %s: %w", nodeIP, ErrConfigNilForInsecureApply)
	}

	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config for insecure apply: %w", err)
	}

	client, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		//nolint:gosec // Intentional: maintenance mode nodes have no TLS certificates.
		// Talos nodes in maintenance mode (after STATE partition wipe) serve the API
		// without TLS. InsecureSkipVerify is required to connect and apply the initial
		// config that will establish PKI. This is equivalent to `talosctl apply-config --insecure`.
		talosclient.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, // #nosec G402
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create insecure Talos client for %s: %w", nodeIP, err)
	}

	defer client.Close() //nolint:errcheck

	_, err = client.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
	})
	if err != nil {
		return fmt.Errorf("failed to apply config (insecure) on %s: %w", nodeIP, err)
	}

	return nil
}

// waitForMaintenanceMode polls until a node enters Talos maintenance mode
// (responds to API but is not fully booted). Used after STATE partition wipe.
func (p *Provisioner) waitForMaintenanceMode(
	ctx context.Context,
	nodeIP string,
	timeout time.Duration,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to enter maintenance mode...\n", nodeIP)

	pollErr := readiness.PollForReadiness(ctx, timeout, func(ctx context.Context) (bool, error) {
		client, err := talosclient.New(ctx,
			talosclient.WithEndpoints(nodeIP),
			//nolint:gosec // Intentional: maintenance mode nodes have no TLS certificates.
			// See applyConfigInsecure for full rationale.
			talosclient.WithTLSConfig(&tls.Config{
				InsecureSkipVerify: true, // #nosec G402
			}),
		)
		if err != nil {
			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		defer client.Close() //nolint:errcheck

		_, versionErr := client.Version(ctx)

		return versionErr == nil, nil
	})
	if pollErr != nil {
		return fmt.Errorf("wait for maintenance mode on %s: %w", nodeIP, pollErr)
	}

	return nil
}

// rollingWipeEphemeral performs a rolling EPHEMERAL partition wipe across all nodes.
// For each node: cordon → drain → staged apply → reset EPHEMERAL → wait Ready → uncordon.
func (p *Provisioner) rollingWipeEphemeral(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	clientset, ordered, err := p.prepareRollingWipe(ctx, clusterName, "EPHEMERAL")
	if err != nil {
		return err
	}

	for i, node := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Rolling EPHEMERAL wipe for %s (%s)...\n",
			i+1, len(ordered), node.IP, node.Role,
		)

		wipeErr := p.wipeEphemeralSingleNode(ctx, clientset, node, result)
		if wipeErr != nil {
			return wipeErr
		}

		recordAppliedChange(result, node.Role, node.IP, "EPHEMERAL partition wiped")

		_, _ = fmt.Fprintf(p.logWriter,
			"  ✓ Node %s (%s) EPHEMERAL wipe completed\n",
			node.IP, node.Role,
		)
	}

	return nil
}

// wipeEphemeralSingleNode performs the cordon → drain → stage → reset EPHEMERAL →
// wait → uncordon sequence for a single node.
//
//nolint:cyclop // sequential cordon/drain/stage/reset/wait/uncordon steps
func (p *Provisioner) wipeEphemeralSingleNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	node nodeWithRole,
	result *clusterupdate.UpdateResult,
) error {
	nodeName, resolveErr := p.resolveNodeName(ctx, clientset, node.IP)
	if resolveErr != nil {
		recordFailedChange(result, node.Role, node.IP, resolveErr)

		return fmt.Errorf("resolve node name for %s: %w", node.IP, resolveErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Cordoning %s (%s)...\n", nodeName, node.IP)

	if cordonErr := p.cordonNode(ctx, clientset, nodeName); cordonErr != nil {
		recordFailedChange(result, node.Role, node.IP, cordonErr)

		return fmt.Errorf("cordon %s: %w", nodeName, cordonErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Draining %s...\n", nodeName)

	if drainErr := p.drainNode(ctx, clientset, nodeName); drainErr != nil {
		recordFailedChange(result, node.Role, node.IP, drainErr)

		return fmt.Errorf("drain %s: %w", nodeName, drainErr)
	}

	if stageErr := p.stageConfigIfNeeded(ctx, node); stageErr != nil {
		recordFailedChange(result, node.Role, node.IP, stageErr)

		return fmt.Errorf("stage config on %s: %w", node.IP, stageErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Resetting EPHEMERAL partition on %s...\n", node.IP)

	if resetErr := p.resetNode(ctx, node.IP,
		[]string{constants.EphemeralPartitionLabel}, true,
	); resetErr != nil {
		recordFailedChange(result, node.Role, node.IP, resetErr)

		return fmt.Errorf("reset EPHEMERAL on %s: %w", node.IP, resetErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to become ready...\n", nodeName)

	if waitErr := p.waitForK8sNodeReady(ctx, clientset, nodeName, nodeReadinessTimeout); waitErr != nil {
		recordFailedChange(result, node.Role, node.IP, waitErr)

		return fmt.Errorf("wait for ready after EPHEMERAL wipe on %s: %w", nodeName, waitErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Uncordoning %s...\n", nodeName)

	if uncordonErr := p.uncordonNode(ctx, clientset, nodeName); uncordonErr != nil {
		recordFailedChange(result, node.Role, node.IP, uncordonErr)

		return fmt.Errorf("uncordon %s: %w", nodeName, uncordonErr)
	}

	return nil
}

// rollingWipeState performs a rolling STATE partition wipe across all nodes.
// For each node: cordon → drain → reset STATE → wait maintenance → insecure apply → wait Ready → uncordon.
// STATE partition wipe causes the node to enter maintenance mode, requiring an insecure apply.
func (p *Provisioner) rollingWipeState(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	clientset, ordered, err := p.prepareRollingWipe(ctx, clusterName, "STATE")
	if err != nil {
		return err
	}

	for i, node := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Rolling STATE wipe for %s (%s)...\n",
			i+1, len(ordered), node.IP, node.Role,
		)

		wipeErr := p.wipeStateSingleNode(ctx, clientset, node, result)
		if wipeErr != nil {
			return wipeErr
		}

		recordAppliedChange(result, node.Role, node.IP, "STATE partition wiped")

		_, _ = fmt.Fprintf(p.logWriter,
			"  ✓ Node %s (%s) STATE wipe completed\n",
			node.IP, node.Role,
		)
	}

	return nil
}

// wipeStateSingleNode performs the cordon → drain → reset STATE → wait maintenance →
// insecure apply → wait Ready → uncordon sequence for a single node.
//
//nolint:cyclop // sequential cordon/drain/reset/wait-maintenance/apply/wait-ready/uncordon steps
func (p *Provisioner) wipeStateSingleNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	node nodeWithRole,
	result *clusterupdate.UpdateResult,
) error {
	nodeName, resolveErr := p.resolveNodeName(ctx, clientset, node.IP)
	if resolveErr != nil {
		recordFailedChange(result, node.Role, node.IP, resolveErr)

		return fmt.Errorf("resolve node name for %s: %w", node.IP, resolveErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Cordoning %s (%s)...\n", nodeName, node.IP)

	if cordonErr := p.cordonNode(ctx, clientset, nodeName); cordonErr != nil {
		recordFailedChange(result, node.Role, node.IP, cordonErr)

		return fmt.Errorf("cordon %s: %w", nodeName, cordonErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Draining %s...\n", nodeName)

	if drainErr := p.drainNode(ctx, clientset, nodeName); drainErr != nil {
		recordFailedChange(result, node.Role, node.IP, drainErr)

		return fmt.Errorf("drain %s: %w", nodeName, drainErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Resetting STATE partition on %s...\n", node.IP)

	if resetErr := p.resetNode(ctx, node.IP,
		[]string{constants.StatePartitionLabel}, true,
	); resetErr != nil {
		recordFailedChange(result, node.Role, node.IP, resetErr)

		return fmt.Errorf("reset STATE on %s: %w", node.IP, resetErr)
	}

	// STATE wipe causes the node to enter maintenance mode.
	if waitErr := p.waitForMaintenanceMode(ctx, node.IP, maintenanceWaitTimeout); waitErr != nil {
		recordFailedChange(result, node.Role, node.IP, waitErr)

		return fmt.Errorf("wait for maintenance mode on %s: %w", node.IP, waitErr)
	}

	if applyErr := p.applyInsecureConfigIfNeeded(ctx, node); applyErr != nil {
		recordFailedChange(result, node.Role, node.IP, applyErr)

		return fmt.Errorf("insecure apply on %s: %w", node.IP, applyErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to become ready...\n", nodeName)

	if waitErr := p.waitForK8sNodeReady(ctx, clientset, nodeName, nodeReadinessTimeout); waitErr != nil {
		recordFailedChange(result, node.Role, node.IP, waitErr)

		return fmt.Errorf("wait for ready after STATE wipe on %s: %w", nodeName, waitErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Uncordoning %s...\n", nodeName)

	if uncordonErr := p.uncordonNode(ctx, clientset, nodeName); uncordonErr != nil {
		recordFailedChange(result, node.Role, node.IP, uncordonErr)

		return fmt.Errorf("uncordon %s: %w", nodeName, uncordonErr)
	}

	return nil
}

// prepareRollingWipe creates the Kubernetes clientset and returns sorted nodes
// for a rolling wipe operation. Shared between rollingWipeEphemeral and rollingWipeState.
func (p *Provisioner) prepareRollingWipe(
	ctx context.Context,
	clusterName, partitionType string,
) (kubernetes.Interface, []nodeWithRole, error) {
	kubeconfigPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	kubeconfigContext := p.options.KubeconfigContext
	if kubeconfigContext == "" {
		kubeconfigContext = "admin@" + clusterName
	}

	clientset, err := k8s.NewClientset(canonicalPath, kubeconfigContext)
	if err != nil {
		return nil, nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("list nodes for %s wipe: %w", partitionType, err)
	}

	return clientset, sortNodesWorkersFirst(nodes), nil
}

// stageConfigIfNeeded applies config with STAGED mode before a partition reset
// so new config takes effect on reboot.
func (p *Provisioner) stageConfigIfNeeded(
	ctx context.Context,
	node nodeWithRole,
) error {
	if p.talosConfigs == nil {
		return nil
	}

	config := p.talosConfigs.ControlPlane()
	if node.Role == RoleWorker {
		config = p.talosConfigs.Worker()
	}

	if config == nil {
		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Staging config on %s...\n", node.IP)

	return p.applyConfigWithMode(
		ctx, node.IP, config,
		machineapi.ApplyConfigurationRequest_STAGED,
	)
}

// applyInsecureConfigIfNeeded applies config via insecure connection when the
// node is in maintenance mode (after STATE partition wipe).
func (p *Provisioner) applyInsecureConfigIfNeeded(
	ctx context.Context,
	node nodeWithRole,
) error {
	if p.talosConfigs == nil {
		return nil
	}

	config := p.talosConfigs.ControlPlane()
	if node.Role == RoleWorker {
		config = p.talosConfigs.Worker()
	}

	if config == nil {
		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Applying config (insecure) on %s...\n", node.IP)

	return p.applyConfigInsecure(ctx, node.IP, config)
}

// partitionWipeDecision determines which partitions need wiping based on the
// wipe-required changes in the update result.
func partitionWipeDecision(changes []clusterupdate.Change) (bool, bool) {
	var ephemeral, state bool

	for _, change := range changes {
		switch change.Field {
		case FieldEphemeralEncryption:
			ephemeral = true
		case FieldStateEncryption:
			state = true
		}
	}

	return ephemeral, state
}

// applyWipeRequiredChanges orchestrates partition wipe operations for
// encryption migration. It determines which partitions need wiping based
// on the detected changes and executes the appropriate rolling wipe flow.
// EPHEMERAL must be wiped before STATE (STATE wipe is more disruptive).
func (p *Provisioner) applyWipeRequiredChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	needsEphemeral, needsState := partitionWipeDecision(result.WipeRequired)

	// EPHEMERAL wipe is less disruptive and must complete before STATE wipe.
	if needsEphemeral {
		_, _ = fmt.Fprintf(p.logWriter, "\n  🔄 Starting EPHEMERAL partition wipe migration...\n")

		if err := p.rollingWipeEphemeral(ctx, clusterName, result); err != nil {
			return fmt.Errorf("EPHEMERAL partition wipe failed: %w", err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ EPHEMERAL partition wipe completed\n")
	}

	if needsState {
		_, _ = fmt.Fprintf(p.logWriter, "\n  🔄 Starting STATE partition wipe migration...\n")

		if err := p.rollingWipeState(ctx, clusterName, result); err != nil {
			return fmt.Errorf("STATE partition wipe failed: %w", err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ STATE partition wipe completed\n")
	}

	return nil
}
