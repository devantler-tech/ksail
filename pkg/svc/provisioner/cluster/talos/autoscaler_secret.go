package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// syncHetznerFirewallRules synchronizes the Hetzner Cloud Firewall rules to the
// hardened set, migrating clusters created with the old insecure rules.
// No-ops when the provisioner was not initialized with Hetzner opts.
func (p *Provisioner) syncHetznerFirewallRules(
	ctx context.Context,
	clusterName string,
) error {
	if p.hetznerOpts == nil {
		return nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	syncErr := hzProvider.SyncFirewallRules(ctx, clusterName, p.hetznerOpts.AllowedCIDRs)
	if syncErr != nil {
		return fmt.Errorf("failed to sync Hetzner firewall rules for %s: %w", clusterName, syncErr)
	}

	return nil
}

// ensureAutoscalerSecretIfNeeded creates or updates the cluster-autoscaler-config
// Secret when the node autoscaler is enabled on Hetzner. It manages no Secret when
// autoscaling is disabled, the provider is not Hetzner, or the config bundle is
// unavailable; with autoscaling disabled on Hetzner it still reports the servers the
// autoscaler left behind (reportServersOfDisabledAutoscaler). Returns
// ErrAutoscalerRequiresSchematic early when no
// schematic is configured, before performing any side effects.
//
// When the Secret changes it brings existing autoscaler nodes to the new baseline
// — but only as disruptively as the change demands (see
// propagateAutoscalerBaseline): a NO_REBOOT in-place apply for config-only drift,
// an in-place reboot of the same servers for a reboot-required change, and a
// drain-and-replace recycle only when a new boot image or a wipe/recreate-class
// change genuinely requires fresh nodes.
func (p *Provisioner) ensureAutoscalerSecretIfNeeded(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	result *clusterupdate.UpdateResult,
) error {
	if !p.autoscalerSecretApplicable() {
		return p.reportServersOfDisabledAutoscaler(ctx, clusterName, result)
	}

	configBundle := p.talosConfigs.Bundle()
	if configBundle == nil {
		return p.auditAutoscalerServersWithoutBundle(ctx, clusterName, result)
	}

	// Fail fast: check that a schematic is available before performing
	// side effects (creating secrets, uploading snapshots). The autoscaler
	// requires a snapshot image to provision new nodes.
	if !p.hasSchematicConfigured() {
		return ErrAutoscalerRequiresSchematic
	}

	// Ensure the hcloud secret (token + network) exists. The autoscaler Helm
	// chart references this secret for HCLOUD_TOKEN and HCLOUD_NETWORK.
	err := p.ensureHcloudSecret(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("ensuring hcloud secret for autoscaler: %w", err)
	}

	snapshotImageID, err := p.ensureSnapshotImage(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("looking up snapshot image for autoscaler secret: %w", err)
	}

	// Read the snapshot image existing nodes booted from before the Secret is
	// overwritten, so a Talos OS bump (new boot image) can be detected below.
	prevImageID, pendingImage, err := p.currentAutoscalerSnapshotBaseline(ctx)
	if err != nil {
		return fmt.Errorf("reading autoscaler snapshot baseline: %w", err)
	}

	if pendingImage && snapshotImageID <= 0 {
		return errAutoscalerSnapshotImageUnavailable
	}

	// Restart the autoscaler when the config changed so it reloads the new
	// Kubernetes version / snapshot baked into the Secret (read as env vars,
	// which Kubernetes does not live-reload).
	changed, err := p.ensureAutoscalerSecret(ctx, configBundle, snapshotImageID, true)
	if err != nil {
		return err
	}

	desiredImageID := strconv.FormatInt(snapshotImageID, 10)
	imageChanged := autoscalerImageChanged(changed, pendingImage, prevImageID, desiredImageID)

	return p.convergeAutoscalerBaseline(
		ctx,
		clusterName,
		diff,
		result,
		changed,
		imageChanged,
		desiredImageID,
	)
}

// convergeAutoscalerBaseline activates a changed boot image, reconciles the existing
// autoscaler nodes, and acknowledges the image only once every node converged.
func (p *Provisioner) convergeAutoscalerBaseline(
	ctx context.Context,
	clusterName string,
	diff, result *clusterupdate.UpdateResult,
	changed, imageChanged bool,
	desiredImageID string,
) error {
	if imageChanged {
		err := p.activateAutoscalerImage(ctx, !changed)
		if err != nil {
			return err
		}
	}

	secretChanged := p.autoscalerSecretChangedThisUpdate(changed, diff)

	err := p.reconcileAutoscalerNodes(ctx, clusterName, diff, secretChanged, imageChanged, result)
	if err != nil || !imageChanged {
		return err
	}

	if failed := autoscalerConvergenceFailures(result); failed > 0 {
		return fmt.Errorf("autoscaler image convergence: %d changes failed: %w",
			failed, errAutoscalerNodeConfigurationChangesFailed)
	}

	return p.completeAutoscalerImageBaseline(ctx, desiredImageID)
}

// autoscalerSecretChangedThisUpdate reports whether the autoscaler Secret changed
// during this update, counting a refresh an earlier pass of the same update made.
//
// A same-version image roll refreshes the Secret before the regular update computes
// its diff (diff == nil). That pass can only apply NO_REBOOT, so it is remembered: the
// classified pass then finds the Secret unchanged, yet a reboot- or recreate-class
// change in its diff must still reach the existing nodes. Without an earlier refresh
// an unchanged Secret means the worker template did not change, and the existing
// nodes are left alone whatever the diff carries for other nodes.
func (p *Provisioner) autoscalerSecretChangedThisUpdate(
	changed bool,
	diff *clusterupdate.UpdateResult,
) bool {
	if diff == nil {
		p.autoscalerSecretRefreshedEarly = p.autoscalerSecretRefreshedEarly || changed

		return changed
	}

	refreshedEarly := p.autoscalerSecretRefreshedEarly
	p.autoscalerSecretRefreshedEarly = false

	return shouldPropagateAutoscalerBaseline(changed, refreshedEarly, diff)
}

// shouldPropagateAutoscalerBaseline reports whether the classified pass must bring
// the existing autoscaler nodes to the baseline: the Secret changed now, or an
// earlier pass of this update refreshed it and the diff still carries a change.
func shouldPropagateAutoscalerBaseline(
	changed, refreshedEarly bool,
	diff *clusterupdate.UpdateResult,
) bool {
	if changed {
		return true
	}

	if !refreshedEarly || diff == nil {
		return false
	}

	return autoscalerRecycleRequired(diff, false) || autoscalerRebootRequired(diff) ||
		diff.HasInPlaceChanges()
}

// errAutoscalerDisabled reports a server the cluster autoscaler created that outlived
// the autoscaler: nothing scales it down or replaces it, and KSail does not own it.
var errAutoscalerDisabled = errors.New(
	"the node autoscaler is disabled, so nothing manages the servers it created",
)

// reportServersOfDisabledAutoscaler reports every server the cluster autoscaler
// created for the cluster while the node autoscaler is disabled. Disabling it
// uninstalls the autoscaler but leaves its servers running: no update path converges
// them, so without this report they would keep running unnoticed, whether or not
// their pool is still listed. Each server is recorded as a failed change and left
// untouched; the user resolves it by re-enabling the autoscaler or by draining the
// node and deleting its server.
//
// A server of a node group the user declared in autoscalerNodePoolNames without a
// KSail pool is not reported: an autoscaler the user runs themselves manages it.
//
// It is report-only and independent of listAutoscalerServers, whose disabled-guard
// keeps the recycle, reboot and in-place paths from acting on servers while the
// autoscaler is off. It is a silent no-op when the autoscaler is enabled, when the
// infrastructure provider is not Hetzner, and when the cluster has no such servers.
func (p *Provisioner) reportServersOfDisabledAutoscaler(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	if p.hetznerOpts == nil || p.hetznerOpts.NodeAutoscalerEnabled {
		return nil
	}

	hzProvider, ok := p.infraProvider.(*hetzner.Provider)
	if !ok {
		return nil
	}

	servers, err := hzProvider.ListClusterAutoscalerNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("listing autoscaler nodes: %w", err)
	}

	for _, server := range sortServersByName(servers) {
		if p.externallyManagedNodeGroup(server.Labels[hetzner.LabelAutoscalerNodeGroup]) {
			continue
		}

		_, _ = fmt.Fprintf(p.logWriter,
			"  ⚠ Autoscaler node %s is left untouched: %v\n", server.Name, errAutoscalerDisabled)

		recordFailedChange(result, RoleWorker, server.Name, fmt.Errorf(
			"%w; re-enable the autoscaler, or drain the node and delete its server",
			errAutoscalerDisabled,
		))
	}

	return nil
}

// reconcileAutoscalerNodes follows the autoscaler Secret refresh. The refreshed Secret
// alone only fixes newly provisioned nodes; existing autoscaler nodes are not
// KSail-owned, so the in-place rolling apply and rolling reboot never touch them.
// They are brought to the new baseline only when the Secret changed during this
// update or a changed boot image is still waiting for them.
//
// A server of a pool that is no longer configured is reported on every update, not
// only on the one that rewrites the Secret: removing a pool changes the Secret once,
// so a later update would otherwise succeed while the server keeps running
// unreported. Propagation lists the servers itself and reports such a server as it
// does, so the audit runs only when nothing is propagated and each server is
// reported once per update.
func (p *Provisioner) reconcileAutoscalerNodes(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	secretChanged bool,
	imageChanged bool,
	result *clusterupdate.UpdateResult,
) error {
	if !secretChanged && !imageChanged {
		_, err := p.listAutoscalerServers(ctx, clusterName, result)

		return err
	}

	return p.propagateAutoscalerBaseline(ctx, clusterName, diff, imageChanged, result)
}

// propagateAutoscalerBaseline brings existing autoscaler nodes to the refreshed
// baseline, choosing the least disruptive mechanism the change allows.
// Configuration requiring fresh servers takes priority. Otherwise, changed images
// selectively replace stale servers before configuration reaches the survivors:
//
//   - recycle (drain → delete → the autoscaler re-provisions a fresh node from the
//     new template) — only when a fresh server is unavoidable: a new boot image
//     (Talos OS bump) or a wipe/recreate/rolling-recreate change.
//   - in-place reboot of the SAME servers (rollingRebootAutoscalerNodes) — for a
//     reboot-required change (CNI swap, disk-quota toggle) an already-booted node
//     can adopt by rebooting, with no fresh server. This keeps a capacity-constrained
//     project at its Hetzner server limit converging where a recycle could not (#5219).
//   - in-place NO_REBOOT apply (applyInPlaceToAutoscalerNodes) — for config-only
//     drift; no drain, so it never stalls on a PodDisruptionBudget.
func (p *Provisioner) propagateAutoscalerBaseline(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	imageChanged bool,
	result *clusterupdate.UpdateResult,
) error {
	if autoscalerRecycleRequired(diff, false) {
		return p.recycleAutoscalerNodes(ctx, clusterName, result)
	}

	if imageChanged {
		err := p.recycleAutoscalerImageNodes(ctx, clusterName, result)
		if err != nil {
			return err
		}
	}

	if autoscalerRebootRequired(diff) {
		return p.rollingRebootAutoscalerNodes(ctx, clusterName, result)
	}

	return p.applyInPlaceToAutoscalerNodes(ctx, clusterName, result)
}

// autoscalerRecycleRequired reports whether the refreshed baseline can only reach
// existing autoscaler nodes by replacing them with fresh servers. That is true when
// the Talos boot image changed (a new snapshot an already-booted node cannot adopt
// in place) or when the diff carries a wipe/recreate/rolling-recreate change that no
// apply against the running server can land. A nil diff (no classification
// available) defers to the image signal alone, defaulting to the non-disruptive
// in-place path.
//
// A reboot-required change (CNI swap, disk-quota toggle) is deliberately NOT here:
// an already-booted server can adopt it via an in-place reboot of the SAME server
// (autoscalerRebootRequired → rollingRebootAutoscalerNodes) — no fresh server, so a
// capacity-constrained project at its Hetzner server limit still converges (#5219).
// Wipe/recreate-class configuration replaces the entire tier. An image-only
// replacement filters out completed target-image servers before the reboot path.
func autoscalerRecycleRequired(diff *clusterupdate.UpdateResult, imageChanged bool) bool {
	if imageChanged {
		return true
	}

	if diff == nil {
		return false
	}

	return diff.HasWipeRequired() ||
		diff.HasRecreateRequired() || diff.HasRollingRecreate()
}

// autoscalerRebootRequired reports whether the refreshed baseline carries a
// reboot-required change (CNI swap, disk-quota toggle) that an in-place NO_REBOOT
// apply cannot land but an in-place reboot of the SAME server can — no fresh server
// needed. Wipe/recreate-class configuration takes precedence. A changed image
// selectively replaces stale servers first, then this path updates the survivors.
func autoscalerRebootRequired(diff *clusterupdate.UpdateResult) bool {
	return diff != nil && diff.HasRebootRequired()
}

// autoscalerSecretApplicable reports whether the cluster-autoscaler-config Secret
// can be managed: the provider is Hetzner, the node autoscaler is enabled, and a
// Talos config bundle is loaded to derive the worker config from.
func (p *Provisioner) autoscalerSecretApplicable() bool {
	return p.hetznerOpts != nil && p.hetznerOpts.NodeAutoscalerEnabled && p.talosConfigs != nil
}

// hasSchematicConfigured reports whether a Talos schematic ID is available
// (either explicit via talosOpts.SchematicID or auto-computed from extensions
// via talosConfigs.SchematicID()).
func (p *Provisioner) hasSchematicConfigured() bool {
	return p.resolveSchematicID() != ""
}

// autoscalerConvergenceFailures counts the failed changes that mean a node KSail
// converges did not reach the baseline. The inventory reports — a server of a removed
// pool, a server that outlived a disabled autoscaler — are failed changes too, so the
// update that carries them fails, but KSail never converges those servers: they must
// not hold a boot-image rollout of the managed nodes pending, nor stop the static
// nodes from rolling.
func autoscalerConvergenceFailures(result *clusterupdate.UpdateResult) int {
	if result == nil {
		return 0
	}

	failed := 0

	for _, change := range result.FailedChanges {
		if !isAutoscalerInventoryReport(change) {
			failed++
		}
	}

	return failed
}

// isAutoscalerInventoryReport reports whether a failed change is one of the two
// report-only inventory findings (excludeUnconfiguredPoolServers,
// reportServersOfDisabledAutoscaler).
func isAutoscalerInventoryReport(change clusterupdate.Change) bool {
	return strings.Contains(change.Reason, errUnknownAutoscalerPool.Error()) ||
		strings.Contains(change.Reason, errAutoscalerDisabled.Error())
}

// auditAutoscalerServersWithoutBundle runs the removed-pool audit when no config
// bundle is loaded. Nothing can be converged without a bundle, but a server of a pool
// that is no longer configured must still be reported on this update. Without a
// Hetzner provider there is no inventory to read.
func (p *Provisioner) auditAutoscalerServersWithoutBundle(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	if _, ok := p.infraProvider.(*hetzner.Provider); !ok {
		return nil
	}

	_, err := p.listAutoscalerServers(ctx, clusterName, result)

	return err
}
