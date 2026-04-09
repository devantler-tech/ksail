package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// Upgrade timeouts and retry intervals.
const (
	// upgradeNodeReadinessTimeout is the maximum time to wait for a node to
	// become reachable after a reboot following an upgrade.
	upgradeNodeReadinessTimeout = 10 * time.Minute
	// upgradeReadinessInterval is the poll interval for node readiness checks.
	upgradeReadinessInterval = 5 * time.Second
)

// upgradeNodeTalosVersion performs a Talos OS upgrade on a single node using the
// LifecycleService API. The flow is:
//  1. Pull the installer image onto the node's containerd.
//  2. Call LifecycleService.Upgrade to write the new OS image.
//  3. Reboot the node.
//  4. Wait for the node to come back online.
func (p *Provisioner) upgradeNodeTalosVersion(
	ctx context.Context,
	nodeIP, installerImage string,
) error {
	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return fmt.Errorf("creating talos client: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	containerd := &common.ContainerdInstance{
		Driver:    common.ContainerDriver_CONTAINERD,
		Namespace: common.ContainerdNamespace_NS_SYSTEM,
	}

	// Step 1: Pull the installer image on the node.
	if pullErr := p.pullInstallerImage(ctx, talosClient, containerd, nodeIP, installerImage); pullErr != nil {
		return pullErr
	}

	// Step 2: Upgrade via LifecycleService.
	if upgradeErr := p.lifecycleUpgrade(ctx, talosClient, containerd, nodeIP, installerImage); upgradeErr != nil {
		return upgradeErr
	}

	// Step 3: Reboot.
	_, _ = fmt.Fprintf(p.logWriter, "    Rebooting %s...\n", nodeIP)

	if rebootErr := talosClient.Reboot(ctx); rebootErr != nil {
		return fmt.Errorf("rebooting node %s: %w", nodeIP, rebootErr)
	}

	// Step 4: Wait for the node to come back.
	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to become ready...\n", nodeIP)

	if waitErr := p.waitForNodeReadyAfterUpgrade(ctx, nodeIP); waitErr != nil {
		return fmt.Errorf("waiting for node %s readiness: %w", nodeIP, waitErr)
	}

	return nil
}

// pullInstallerImage pulls the Talos installer image on the remote node via the
// ImageService.Pull streaming RPC.
func (p *Provisioner) pullInstallerImage(
	ctx context.Context,
	talosClient *talosclient.Client,
	containerd *common.ContainerdInstance,
	nodeIP, installerImage string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "    Pulling installer image %s on %s...\n", installerImage, nodeIP)

	stream, err := talosClient.ImageClient.Pull(ctx, &machineapi.ImageServicePullRequest{
		Containerd: containerd,
		ImageRef:   installerImage,
	})
	if err != nil {
		return fmt.Errorf("pulling installer image on %s: %w", nodeIP, err)
	}

	for {
		_, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}

		if recvErr != nil {
			return fmt.Errorf("pulling installer image on %s: %w", nodeIP, recvErr)
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "    ✓ Installer image pulled on %s\n", nodeIP)

	return nil
}

// lifecycleUpgrade calls LifecycleService.Upgrade on the node and drains the
// streaming progress response.
func (p *Provisioner) lifecycleUpgrade(
	ctx context.Context,
	talosClient *talosclient.Client,
	containerd *common.ContainerdInstance,
	nodeIP, installerImage string,
) error {
	_, _ = fmt.Fprintf(p.logWriter, "    Upgrading %s via LifecycleService...\n", nodeIP)

	stream, err := talosClient.LifecycleClient.Upgrade(ctx, &machineapi.LifecycleServiceUpgradeRequest{
		Containerd: containerd,
		Source: &machineapi.InstallArtifactsSource{
			ImageName: installerImage,
		},
	})
	if err != nil {
		return fmt.Errorf("lifecycle upgrade on %s: %w", nodeIP, err)
	}

	for {
		resp, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}

		if recvErr != nil {
			return fmt.Errorf("lifecycle upgrade on %s: %w", nodeIP, recvErr)
		}

		progress := resp.GetProgress()
		if progress == nil {
			continue
		}

		switch msg := progress.GetResponse().(type) {
		case *machineapi.LifecycleServiceInstallProgress_Message:
			_, _ = fmt.Fprintf(p.logWriter, "      %s: %s\n", nodeIP, msg.Message)
		case *machineapi.LifecycleServiceInstallProgress_ExitCode:
			if msg.ExitCode != 0 {
				return fmt.Errorf("upgrade on %s failed with exit code %d", nodeIP, msg.ExitCode)
			}
		}
	}

	_, _ = fmt.Fprintf(p.logWriter, "    ✓ Upgrade completed on %s\n", nodeIP)

	return nil
}

// waitForNodeReadyAfterUpgrade polls a node's Talos API until it responds,
// indicating the node has rebooted and is reachable again after an upgrade.
func (p *Provisioner) waitForNodeReadyAfterUpgrade(ctx context.Context, nodeIP string) error {
	deadline := time.Now().Add(upgradeNodeReadinessTimeout)

	// Short delay to allow the node to begin rebooting before we start polling.
	select {
	case <-time.After(upgradeReadinessInterval):
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck
	}

	for time.Now().Before(deadline) {
		_, err := p.getRunningTalosVersion(ctx, nodeIP)
		if err == nil {
			return nil
		}

		select {
		case <-time.After(upgradeReadinessInterval):
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		}
	}

	return fmt.Errorf("node %s did not become ready within %s", nodeIP, upgradeNodeReadinessTimeout)
}

// rollingUpgradeNodes performs a rolling Talos OS upgrade across all cluster
// nodes. Workers are upgraded first, then control-planes, one node at a time.
func (p *Provisioner) rollingUpgradeNodes(
	ctx context.Context,
	clusterName, installerImage string,
) error {
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("listing nodes for upgrade: %w", err)
	}

	// Partition into workers and control-planes.
	var workers, controlPlanes []nodeWithRole

	for _, n := range nodes {
		switch n.Role {
		case RoleWorker:
			workers = append(workers, n)
		case RoleControlPlane:
			controlPlanes = append(controlPlanes, n)
		}
	}

	// Sort for deterministic ordering.
	sort.Slice(workers, func(i, j int) bool { return workers[i].IP < workers[j].IP })
	sort.Slice(controlPlanes, func(i, j int) bool { return controlPlanes[i].IP < controlPlanes[j].IP })

	// Upgrade workers first, then control-planes.
	ordered := append(workers, controlPlanes...) //nolint:gocritic // intentional append to new slice

	for i, node := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Upgrading %s (%s)...\n",
			i+1, len(ordered), node.IP, node.Role,
		)

		if upgradeErr := p.upgradeNodeTalosVersion(ctx, node.IP, installerImage); upgradeErr != nil {
			return fmt.Errorf("upgrading node %s (%s): %w", node.IP, node.Role, upgradeErr)
		}

		_, _ = fmt.Fprintf(p.logWriter,
			"  ✓ Node %s (%s) upgraded successfully\n",
			node.IP, node.Role,
		)
	}

	return nil
}
