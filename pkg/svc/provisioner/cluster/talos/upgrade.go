package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"k8s.io/client-go/kubernetes"
)

const (
	// lifecycleUpgradeMinMajor and lifecycleUpgradeMinMinor are the major/minor of
	// the first Talos release (1.13.0) that implements the ImageService.Pull and
	// LifecycleService.Upgrade APIs. Nodes running older releases only expose the
	// legacy MachineService.Upgrade unary API, so calling the new services on them
	// fails with "unknown service machine.ImageService". Mirrors talosctl's upgrade
	// API range (">1.13.0-alpha.2 <2.0.0").
	lifecycleUpgradeMinMajor = 1
	lifecycleUpgradeMinMinor = 13
)

// supportsLifecycleUpgradeAPI reports whether a node running the given Talos
// version tag implements the ImageService.Pull / LifecycleService.Upgrade APIs
// (Talos >= 1.13). Unparseable tags conservatively fall back to the legacy
// MachineService.Upgrade API, which is available on every currently supported
// Talos release.
func supportsLifecycleUpgradeAPI(versionTag string) bool {
	parsed, err := versionresolver.ParseVersion(versionTag)
	if err != nil {
		return false
	}

	minVersion := versionresolver.Version{
		Major: lifecycleUpgradeMinMajor,
		Minor: lifecycleUpgradeMinMinor,
	}

	return !parsed.Less(minVersion)
}

// runningVersionMatchesTarget reports whether a node's running Talos version is
// already the upgrade target, compared as parsed semver so a "v"-prefix mismatch
// does not hide an already-upgraded node. Unparseable versions return false, so an
// undeterminable version errs toward upgrading rather than silently skipping a node
// that may still need it.
func runningVersionMatchesTarget(running, target string) bool {
	runningVer, runErr := versionresolver.ParseVersion(running)
	targetVer, tgtErr := versionresolver.ParseVersion(target)

	return runErr == nil && tgtErr == nil && runningVer.Equal(targetVer)
}

// upgradeNodeTalosVersion performs a Talos OS upgrade on a single node, then
// waits for it to come back online with the expected version. The upgrade API
// is chosen by the node's running Talos version: nodes on Talos >= 1.13 use the
// ImageService.Pull + LifecycleService.Upgrade APIs, while older nodes (e.g.
// v1.12.x) use the legacy MachineService.Upgrade unary API. Using the newer
// services on an old node fails with "unknown service machine.ImageService";
// this dispatch mirrors talosctl's own version-gated upgrade fallback.
func (p *Provisioner) upgradeNodeTalosVersion(
	ctx context.Context,
	nodeIP, installerImage, desiredTag string,
) error {
	// The upgrade issues non-idempotent RPCs (LifecycleService.Upgrade / legacy
	// MachineService.Upgrade + Reboot) on this one client, so the transient apid
	// handshake race is absorbed by the Version probe inside
	// dialTalosClientWithRetry and the upgrade itself runs exactly once.
	talosClient, err := p.dialTalosClientWithRetry(ctx, nodeIP, "upgrade connect")
	if err != nil {
		return fmt.Errorf("creating talos client for node %s: %w", nodeIP, err)
	}

	defer talosClient.Close() //nolint:errcheck

	runningVersion, verErr := versionTagFromClient(ctx, talosClient)
	if verErr != nil {
		return fmt.Errorf("determining running Talos version on %s: %w", nodeIP, verErr)
	}

	// Skip nodes already at the target. A rolling upgrade walks every node, so when
	// resuming an interrupted or partial roll (a mixed-version cluster) it would
	// otherwise reboot nodes that already run the desired version.
	if runningVersionMatchesTarget(runningVersion, desiredTag) {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"    %s already at %s, skipping upgrade\n",
			nodeIP,
			desiredTag,
		)

		return nil
	}

	if supportsLifecycleUpgradeAPI(runningVersion) {
		err = p.lifecycleUpgradeNode(ctx, talosClient, nodeIP, installerImage)
	} else {
		err = p.legacyUpgradeNode(ctx, talosClient, nodeIP, installerImage)
	}

	if err != nil {
		return err
	}

	// Wait for the node to come back with the desired version.
	_, _ = fmt.Fprintf(p.logWriter, "    Waiting for %s to become ready...\n", nodeIP)

	waitErr := p.waitForNodeReadyAfterUpgrade(ctx, nodeIP, desiredTag)
	if waitErr != nil {
		return fmt.Errorf("waiting for node %s readiness: %w", nodeIP, waitErr)
	}

	return nil
}

// lifecycleUpgradeNode upgrades a node running Talos >= 1.13 using the
// ImageService.Pull + LifecycleService.Upgrade APIs and then reboots it.
// LifecycleService.Upgrade installs the new OS image but does not reboot, so the
// reboot is issued separately (matching talosctl).
func (p *Provisioner) lifecycleUpgradeNode(
	ctx context.Context,
	talosClient *talosclient.Client,
	nodeIP, installerImage string,
) error {
	containerd := &common.ContainerdInstance{
		Driver:    common.ContainerDriver_CONTAINERD,
		Namespace: common.ContainerdNamespace_NS_SYSTEM,
	}

	// Step 1: Pull the installer image on the node.
	pullErr := p.pullInstallerImage(ctx, talosClient, containerd, nodeIP, installerImage)
	if pullErr != nil {
		return pullErr
	}

	// Step 2: Upgrade via LifecycleService.
	upgradeErr := p.lifecycleUpgrade(ctx, talosClient, containerd, nodeIP, installerImage)
	if upgradeErr != nil {
		return upgradeErr
	}

	// Step 3: Reboot.
	_, _ = fmt.Fprintf(p.logWriter, "    Rebooting %s...\n", nodeIP)

	rebootErr := talosClient.Reboot(ctx)
	if rebootErr != nil {
		return fmt.Errorf("rebooting node %s: %w", nodeIP, rebootErr)
	}

	return nil
}

// legacyUpgradeNode upgrades a node running Talos < 1.13 using the legacy
// MachineService.Upgrade unary API. That API pulls the installer image and
// reboots the node itself, so no separate ImageService.Pull or reboot is needed.
func (p *Provisioner) legacyUpgradeNode(
	ctx context.Context,
	talosClient *talosclient.Client,
	nodeIP, installerImage string,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"    Upgrading %s via MachineService (legacy API)...\n", nodeIP)

	// MachineService.Upgrade is deprecated in favour of LifecycleService but is
	// the only upgrade API available on Talos < 1.13; talosctl keeps the same
	// fallback. WithUpgradeRebootMode(DEFAULT) lets the node reboot itself.
	opts := []talosclient.UpgradeOption{
		talosclient.WithUpgradeImage(installerImage),
		talosclient.WithUpgradeRebootMode(machineapi.UpgradeRequest_DEFAULT),
	}

	_, err := talosClient.UpgradeWithOptions(ctx, opts...) //nolint:staticcheck // legacy <1.13 API
	if err != nil {
		return fmt.Errorf("legacy upgrade on %s: %w", nodeIP, err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    ✓ Upgrade initiated on %s\n", nodeIP)

	return nil
}

// versionTagFromClient returns the running Talos version tag reported by a node
// through an already-established client.
func versionTagFromClient(ctx context.Context, talosClient *talosclient.Client) (string, error) {
	resp, err := talosClient.Version(ctx)
	if err != nil {
		return "", fmt.Errorf("querying Talos version: %w", err)
	}

	if len(resp.GetMessages()) == 0 || resp.GetMessages()[0].GetVersion() == nil {
		return "", ErrEmptyVersionResponse
	}

	return resp.GetMessages()[0].GetVersion().GetTag(), nil
}

// pullInstallerImage pulls the Talos installer image on the remote node via the
// ImageService.Pull streaming RPC.
func (p *Provisioner) pullInstallerImage(
	ctx context.Context,
	talosClient *talosclient.Client,
	containerd *common.ContainerdInstance,
	nodeIP, installerImage string,
) error {
	_, _ = fmt.Fprintf(
		p.logWriter,
		"    Pulling installer image %s on %s...\n",
		installerImage,
		nodeIP,
	)

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

	stream, err := talosClient.LifecycleClient.Upgrade(
		ctx,
		&machineapi.LifecycleServiceUpgradeRequest{
			Containerd: containerd,
			Source: &machineapi.InstallArtifactsSource{
				ImageName: installerImage,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("lifecycle upgrade on %s: %w", nodeIP, err)
	}

	drainErr := drainUpgradeStream(stream, p.logWriter, nodeIP)
	if drainErr != nil {
		return drainErr
	}

	_, _ = fmt.Fprintf(p.logWriter, "    ✓ Upgrade completed on %s\n", nodeIP)

	return nil
}

// drainUpgradeStream reads all messages from a LifecycleService.Upgrade stream,
// logging progress messages and checking the exit code.
func drainUpgradeStream(
	stream machineapi.LifecycleService_UpgradeClient,
	logWriter io.Writer,
	nodeIP string,
) error {
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("lifecycle upgrade on %s: %w", nodeIP, err)
		}

		progress := resp.GetProgress()
		if progress == nil {
			continue
		}

		switch msg := progress.GetResponse().(type) {
		case *machineapi.LifecycleServiceInstallProgress_Message:
			_, _ = fmt.Fprintf(logWriter, "      %s: %s\n", nodeIP, msg.Message)
		case *machineapi.LifecycleServiceInstallProgress_ExitCode:
			if msg.ExitCode != 0 {
				return fmt.Errorf(
					"node %s exit code %d: %w",
					nodeIP,
					msg.ExitCode,
					ErrUpgradeFailed,
				)
			}
		}
	}
}

// waitForNodeReadyAfterUpgrade polls a node's Talos API until it responds with
// the desired version tag, indicating the node has rebooted into the new OS.
func (p *Provisioner) waitForNodeReadyAfterUpgrade(
	ctx context.Context,
	nodeIP, desiredTag string,
) error {
	deadline := time.Now().Add(clusterReadinessTimeout)

	// Short delay to allow the node to begin rebooting before we start polling.
	select {
	case <-time.After(retryInterval):
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck
	}

	for time.Now().Before(deadline) {
		pollCtx, pollCancel := context.WithTimeout(ctx, retryInterval)

		tag, err := p.getRunningTalosVersion(pollCtx, nodeIP)

		pollCancel()

		if err == nil && tag == desiredTag {
			return nil
		}

		select {
		case <-time.After(retryInterval):
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		}
	}

	return fmt.Errorf("node %s: %w", nodeIP, ErrNodeNotReady)
}

// rollingUpgradeNodes performs a rolling Talos OS upgrade across all cluster
// nodes. Workers are upgraded first, then control-planes, one node at a time.
// Each node is cordoned and drained before its reboot and uncordoned after it
// returns Ready — the same graceful per-node sequence the config-change rolling
// reboot uses (rollingRebootSingleNode) — so an OS upgrade evicts workloads
// cleanly instead of hard-rebooting them out from under their pods and volumes.
// Before each node's OS upgrade it reconciles the node's desired machine config so
// the new installer validates the desired config rather than the stale committed
// one (issue #5294, see reconcileNodeConfigBeforeUpgrade).
func (p *Provisioner) rollingUpgradeNodes(
	ctx context.Context,
	clusterName, installerImage, desiredTag string,
) error {
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("listing nodes for upgrade: %w", err)
	}

	// Sort workers first, then control-planes for minimal disruption.
	ordered := sortNodesWorkersFirst(nodes)

	// The desired-config rebuild needs the cluster PKI, which only a control-plane
	// node carries. Reuse the node list already fetched above (rather than re-listing
	// via fetchSecretsSource) to resolve one control-plane config up front, then reuse
	// it as the secrets source for every node: all nodes are still at the old version
	// here, and a control-plane's PKI is unchanged by an OS upgrade, so the source
	// stays valid across the roll. A nil result (no control-plane reachable) degrades
	// to a best-effort per-node reconcile.
	secretsSource, _, _ := p.fetchNodeConfigForRole(ctx, nodes, RoleControlPlane)

	// Graceful drain and the between-node storage-health gate both need the
	// Kubernetes API. Build the clientset (and the prober) once; when the API is
	// unreachable both are nil and the per-node sequence degrades to the legacy
	// reboot-without-drain rather than aborting a needed OS upgrade.
	clientset := p.k8sClientOrWarnForUpgrade(clusterName)

	var storageProber storageHealthProber
	if clientset != nil {
		storageProber = p.buildStorageHealthProberOrWarn(ctx, clientset, clusterName)
	}

	for i, node := range ordered {
		_, _ = fmt.Fprintf(p.logWriter,
			"  [%d/%d] Upgrading %s (%s)...\n",
			i+1, len(ordered), node.IP, node.Role,
		)

		upgradeErr := p.upgradeSingleNode(ctx, upgradeNodeRequest{
			clientset:      clientset,
			node:           node,
			secretsSource:  secretsSource,
			prober:         storageProber,
			installerImage: installerImage,
			desiredTag:     desiredTag,
		})
		if upgradeErr != nil {
			return fmt.Errorf("upgrading node %s (%s): %w", node.IP, node.Role, upgradeErr)
		}

		_, _ = fmt.Fprintf(p.logWriter,
			"  ✓ Node %s (%s) upgraded successfully\n",
			node.IP, node.Role,
		)
	}

	return nil
}

// upgradeNodeRequest bundles the inputs for upgradeSingleNode's graceful per-node
// upgrade sequence.
type upgradeNodeRequest struct {
	clientset      kubernetes.Interface
	node           nodeWithRole
	secretsSource  talosconfig.Provider
	prober         storageHealthProber
	installerImage string
	desiredTag     string
}

// upgradeSingleNode runs the full graceful per-node OS-upgrade sequence: cordon →
// drain → reconcile desired config (#5294) → upgrade + reboot → wait Ready →
// uncordon → between-node storage-health gate (#5467). When the clientset is nil
// (Kubernetes API unreachable) or the node cannot be resolved to a Kubernetes node,
// it degrades to the legacy reconcile + upgrade + reboot without draining, so a
// needed OS upgrade still proceeds. A drain failure aborts the roll with the node
// best-effort uncordoned (see cordonAndDrain), matching the config-change path.
func (p *Provisioner) upgradeSingleNode(ctx context.Context, req upgradeNodeRequest) error {
	nodeName := ""

	if req.clientset != nil {
		resolved, resolveErr := p.resolveNodeName(ctx, req.clientset, req.node.IP)
		if resolveErr != nil {
			_, _ = fmt.Fprintf(p.logWriter,
				"  ⚠ Could not resolve %s to a Kubernetes node; upgrading without drain: %v\n",
				req.node.IP, resolveErr,
			)
		} else {
			nodeName = resolved

			drainErr := p.cordonAndDrain(ctx, req.clientset, nodeName)
			if drainErr != nil {
				return drainErr
			}
		}
	}

	// Reconcile the desired config onto the node before upgrading it. Best-effort:
	// a failure here must not regress upgrades that already validate cleanly against
	// the committed config, so warn and proceed (the update's in-place reconcile
	// re-surfaces any genuine drift afterwards).
	configErr := p.reconcileNodeConfigBeforeUpgrade(ctx, req.node, req.secretsSource)
	if configErr != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ Could not reconcile config on %s before upgrade: %v\n",
			req.node.IP, configErr,
		)
	}

	upgradeErr := p.upgradeNodeTalosVersion(ctx, req.node.IP, req.installerImage, req.desiredTag)
	if upgradeErr != nil {
		return upgradeErr
	}

	// Nothing to uncordon (and no storage gate) when the node was never cordoned.
	if nodeName == "" {
		return nil
	}

	p.uncordonAfterUpgrade(ctx, req.clientset, nodeName)

	// Gate progression to the next node on replicated-storage volume health so a
	// one-replica-per-node volume is not faulted by rebooting consecutive replica
	// holders before a rebuild completes (#5467). No-op when the gate is disabled or
	// no backend was detected (prober == nil).
	storageErr := p.waitForStorageHealthy(ctx, req.prober, p.storageHealthTimeout())
	if storageErr != nil {
		return fmt.Errorf("storage health gate: %w", storageErr)
	}

	return nil
}

// uncordonAfterUpgrade waits for the upgraded node to report Ready, then uncordons
// it. Both steps are best-effort: the OS upgrade already succeeded, so a readiness
// or uncordon hiccup is warned rather than failing the roll (the next reconcile
// re-evaluates schedulability).
func (p *Provisioner) uncordonAfterUpgrade(
	ctx context.Context,
	clientset kubernetes.Interface,
	nodeName string,
) {
	readyErr := p.waitForK8sNodeReady(ctx, clientset, nodeName, nodeReadinessTimeout)
	if readyErr != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"    ⚠ %s did not report Ready before uncordon: %v\n", nodeName, readyErr)
	}

	_, _ = fmt.Fprintf(p.logWriter, "    Uncordoning %s...\n", nodeName)

	uncordonErr := p.uncordonNode(ctx, clientset, nodeName)
	if uncordonErr != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"    ⚠ Failed to uncordon %s (best-effort): %v\n", nodeName, uncordonErr)
	}
}

// k8sClientOrWarnForUpgrade builds the Kubernetes clientset used to drain nodes
// during a rolling OS upgrade. It returns nil (with a warning) when the API is
// unreachable, so the upgrade degrades to reboot-without-drain instead of aborting.
func (p *Provisioner) k8sClientOrWarnForUpgrade(clusterName string) kubernetes.Interface {
	clientset, err := p.createK8sClient(clusterName)
	if err != nil {
		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ Kubernetes API unreachable; upgrading without graceful drain: %v\n", err)

		return nil
	}

	return clientset
}

// reconcileNodeConfigBeforeUpgrade applies a node's desired machine config with
// NO_REBOOT mode immediately before its Talos OS upgrade, so the new installer
// validates the desired config rather than the stale committed one (issue #5294).
//
// The Talos upgrade installer reads each node's *active* machine config — machined's
// Upgrade task pipes the running config to the installer container's stdin, which it
// validates before installing. A config change that a newer release requires as a
// prerequisite (e.g. the v1.13.4 install-section validation in the issue) must
// therefore be the *active* config before the upgrade runs. Merely STAGING it is not
// enough: a staged config only becomes active on the next boot, which happens after
// the installer has already validated and run. NO_REBOOT makes the desired config
// active up front; the install section is immediate-apply-safe in Talos, so the
// apply needs no reboot of its own (the upgrade reboots anyway).
//
// It is a no-op when no Talos config is loaded (nothing to reconcile). secretsSource
// supplies the cluster PKI for the rebuild and must be a control-plane config (see
// buildDesiredNodeConfig).
func (p *Provisioner) reconcileNodeConfigBeforeUpgrade(
	ctx context.Context,
	node nodeWithRole,
	secretsSource talosconfig.Provider,
) error {
	return p.applyDesiredNodeConfig(
		ctx, node, secretsSource,
		machineapi.ApplyConfigurationRequest_NO_REBOOT, "Reconciling",
	)
}
