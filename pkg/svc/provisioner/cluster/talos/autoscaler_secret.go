package talosprovisioner

import (
	"context"
	"fmt"
	"strconv"

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
// Secret when the node autoscaler is enabled on Hetzner. It is a no-op when
// autoscaling is disabled, the provider is not Hetzner, or the config bundle
// is unavailable. Returns ErrAutoscalerRequiresSchematic early when no
// schematic is configured, before performing any side effects.
//
// When the Secret changes it brings existing autoscaler nodes to the new baseline
// — but only as disruptively as the change demands (see
// propagateAutoscalerBaseline): a NO_REBOOT in-place apply for config-only drift,
// a drain-and-replace recycle only when a new boot image or a reboot-class change
// genuinely requires fresh nodes.
func (p *Provisioner) ensureAutoscalerSecretIfNeeded(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	result *clusterupdate.UpdateResult,
) error {
	if !p.autoscalerSecretApplicable() {
		return nil
	}

	configBundle := p.talosConfigs.Bundle()
	if configBundle == nil {
		return nil
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
	prevImageID := p.currentAutoscalerSnapshotImageID(ctx)

	// Restart the autoscaler when the config changed so it reloads the new
	// Kubernetes version / snapshot baked into the Secret (read as env vars,
	// which Kubernetes does not live-reload).
	changed, err := p.ensureAutoscalerSecret(ctx, configBundle, snapshotImageID, true)
	if err != nil {
		return err
	}

	// The refreshed Secret alone only fixes newly provisioned nodes; existing
	// autoscaler nodes are not KSail-owned, so the in-place rolling apply and
	// rolling reboot never touch them. Bring them to the new baseline only when
	// the Secret actually changed. A no-op when nothing changed.
	if !changed {
		return nil
	}

	imageChanged := prevImageID != "" && prevImageID != strconv.FormatInt(snapshotImageID, 10)

	return p.propagateAutoscalerBaseline(ctx, clusterName, diff, imageChanged, result)
}

// propagateAutoscalerBaseline brings existing autoscaler nodes to the refreshed
// baseline, choosing the least disruptive mechanism the change allows. A new boot
// image (Talos OS bump) or a reboot/wipe/recreate-class change can only reach an
// immutable Talos node by replacing it, so those recycle (drain → delete → the
// autoscaler re-provisions from the new template). A config-only change that Talos
// can apply with NO_REBOOT is pushed in place instead — no drain, so it never
// stalls on a PodDisruptionBudget the way a recycle can.
func (p *Provisioner) propagateAutoscalerBaseline(
	ctx context.Context,
	clusterName string,
	diff *clusterupdate.UpdateResult,
	imageChanged bool,
	result *clusterupdate.UpdateResult,
) error {
	if autoscalerRecycleRequired(diff, imageChanged) {
		return p.recycleAutoscalerNodes(ctx, clusterName)
	}

	return p.applyInPlaceToAutoscalerNodes(ctx, clusterName, result)
}

// autoscalerRecycleRequired reports whether the refreshed baseline can only reach
// existing autoscaler nodes by replacing them. That is true when the Talos boot
// image changed (a new snapshot an already-booted node cannot adopt in place) or
// when the diff carries a reboot/wipe/recreate/rolling-recreate change Talos
// cannot apply with NO_REBOOT. A nil diff (no classification available) defers to
// the image signal alone, defaulting to the non-disruptive in-place path.
func autoscalerRecycleRequired(diff *clusterupdate.UpdateResult, imageChanged bool) bool {
	if imageChanged {
		return true
	}

	if diff == nil {
		return false
	}

	return diff.HasRebootRequired() || diff.HasWipeRequired() ||
		diff.HasRecreateRequired() || diff.HasRollingRecreate()
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
