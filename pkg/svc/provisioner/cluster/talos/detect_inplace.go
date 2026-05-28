package talosprovisioner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosresconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// fingerprintLength is the number of hex characters shown for a machine-config
// fingerprint in the change summary — enough to distinguish two renders at a
// glance without dumping the whole config into the table.
const fingerprintLength = 12

// MachineConfigField is the change field reported when the rendered Talos
// machine config drifts from what is running on the nodes.
const MachineConfigField = "machine.config"

// detectInPlaceMachineConfigDrift reports whether the desired Talos machine
// config differs from what is running on the control-plane node, returning a
// single in-place change when it does.
//
// Talos patch files (everything under talos/, e.g. sysctls, kubelet config,
// user namespaces, registries, API-server flags) are NOT part of ksail's
// ClusterSpec, so the spec-level diff engine cannot see them. This compares the
// fully rendered desired config against the running node config, which catches
// any patch change — additions, edits, and removals alike — rather than a
// hand-picked set of fields. Surfacing it from DiffConfig both shows the drift
// in the change summary and triggers applyInPlaceConfigChanges, which re-pushes
// the full machine config to every node (NO_REBOOT).
//
// The desired config is first realigned with the running cluster's PKI and
// endpoint (see alignedDesiredControlPlaneConfig); without that, the
// freshly-generated secrets and CIDR-derived endpoint that p.talosConfigs holds
// at diff time would read as drift on every run. After alignment the only
// remaining differences are genuine config/patch content — which is exactly
// what cluster update should reconcile onto the nodes.
//
// Caveats (accepted: ksail owns the node machine config):
//   - Config not managed by ksail's bundle reads as drift and is reconciled away
//     on the next apply, the same as any other config push.
//   - A ksail/Talos version bump can change the rendered defaults, causing one
//     benign, idempotent re-apply; it is self-limiting once applied.
//   - Only a control-plane node is inspected (matching detectDisruptiveConfigChanges);
//     this covers all cluster-wide patches under talos/cluster/. Worker-only
//     patch drift is a future enhancement.
func (p *Provisioner) detectInPlaceMachineConfigDrift(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	// Omni owns node configuration via its own API; skip when configs are absent.
	if p.talosConfigs == nil || p.omniOpts != nil {
		return nil, nil
	}

	runningConfig, found, err := p.fetchRunningControlPlaneConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	// No reachable control-plane node (e.g. cluster not yet up): nothing to compare.
	if !found {
		return nil, nil
	}

	desiredConfig, err := p.alignedDesiredControlPlaneConfig(runningConfig)
	if err != nil {
		return nil, err
	}

	if desiredConfig == nil {
		return nil, nil
	}

	return diffMachineConfig(runningConfig, desiredConfig)
}

// alignedDesiredControlPlaneConfig rebuilds the desired control-plane config with
// the running cluster's PKI (extracted from the running config) and endpoint, so
// a byte comparison reflects only patch/content drift rather than the
// freshly-generated secrets and default endpoint that p.talosConfigs carries at
// diff time. This mirrors syncSecretsFromCluster, which the apply phase relies on
// so that pushed configs match the running cluster's PKI.
func (p *Provisioner) alignedDesiredControlPlaneConfig(
	runningConfig talosconfig.Provider,
) (talosconfig.Provider, error) {
	bundle := secrets.NewBundleFromConfig(secrets.NewFixedClock(time.Now()), runningConfig)

	aligned, err := p.talosConfigs.WithSecrets(bundle)
	if err != nil {
		return nil, fmt.Errorf("align secrets for config drift comparison: %w", err)
	}

	endpointIP := runningConfig.Cluster().Endpoint().Hostname()
	if endpointIP != "" {
		aligned, err = aligned.WithEndpoint(endpointIP)
		if err != nil {
			return nil, fmt.Errorf("align endpoint for config drift comparison: %w", err)
		}
	}

	return aligned.ControlPlane(), nil
}

// diffMachineConfig compares the rendered bytes of the running and desired
// configs and returns a single in-place change when they differ. Both are
// marshalled by the same Talos encoder, so semantically-equal configs produce
// identical bytes; the fingerprints surface a before/after delta in the summary
// without dumping the whole config.
func diffMachineConfig(
	runningConfig, desiredConfig talosconfig.Provider,
) ([]clusterupdate.Change, error) {
	runningBytes, err := runningConfig.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshal running machine config: %w", err)
	}

	desiredBytes, err := desiredConfig.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshal desired machine config: %w", err)
	}

	if bytes.Equal(runningBytes, desiredBytes) {
		return nil, nil
	}

	return []clusterupdate.Change{
		{
			Field:    MachineConfigField,
			OldValue: configFingerprint(runningBytes),
			NewValue: configFingerprint(desiredBytes),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "Talos machine config (patches) differs from running nodes; re-applied without reboot",
		},
	}, nil
}

// configFingerprint returns a short, stable hex fingerprint of a rendered config.
func configFingerprint(configBytes []byte) string {
	sum := sha256.Sum256(configBytes)

	return hex.EncodeToString(sum[:])[:fingerprintLength]
}

// fetchRunningControlPlaneConfig discovers a control-plane node and returns its
// running Talos machine config provider. It returns (nil, false, nil) when no
// control-plane node is reachable so callers can treat "cannot compare" as "no
// detected drift" rather than failing the update. Shared by
// detectDisruptiveConfigChanges and detectInPlaceMachineConfigDrift.
func (p *Provisioner) fetchRunningControlPlaneConfig(
	ctx context.Context,
	clusterName string,
) (talosconfig.Provider, bool, error) {
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, false, fmt.Errorf("failed to discover nodes for config comparison: %w", err)
	}

	var cpIP string

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			cpIP = node.IP

			break
		}
	}

	if cpIP == "" {
		return nil, false, nil
	}

	talosClient, err := p.createTalosClient(ctx, cpIP)
	if err != nil {
		return nil, false, fmt.Errorf(
			"failed to create Talos client for config comparison: %w",
			err,
		)
	}

	defer talosClient.Close() //nolint:errcheck

	machineConfig, err := safe.StateGet[*talosresconfig.MachineConfig](
		ctx,
		talosClient.COSI,
		talosresconfig.NewMachineConfig(nil).Metadata(),
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"failed to fetch running machine config from %s: %w",
			cpIP,
			err,
		)
	}

	return machineConfig.Provider(), true, nil
}
