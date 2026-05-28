package talosprovisioner

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosresconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// detectInPlaceMachineConfigDrift compares in-place-applicable machine config
// fields (currently machine.sysctls and machine.sysfs) between the running
// control-plane node and the desired configuration, returning in-place changes
// when they differ.
//
// These fields come from user-managed Talos patch files (e.g.
// talos/cluster/sysctls.yaml) and are NOT represented in ksail's ClusterSpec, so
// the spec-level diff engine cannot see them. Without this live comparison,
// `ksail cluster update` has no signal that a patch file changed: the update
// either short-circuits ("no changes") or runs without ever pushing the new
// config to existing nodes. Surfacing the drift here (via DiffConfig) both shows
// it in the change summary and triggers applyInPlaceConfigChanges, which
// re-pushes the full machine config — carrying every patch — to each node.
//
// The comparison is intentionally secret-independent (it only inspects the
// tunable maps), so it never false-positives on PKI differences. That matters
// because detection runs before syncSecretsFromCluster: p.talosConfigs still
// holds freshly-generated PKI here, but the tunables it carries are already the
// patched, final values. By the time the change is applied, secret sync has run
// (the in-place change makes needsSecretSync return true), so the pushed config
// has the cluster's real PKI and the new tunables together.
//
// Only a control-plane node is inspected, matching detectDisruptiveConfigChanges:
// cluster-wide patches under talos/cluster/ apply to every node, and the apply
// step pushes the per-role config to all nodes regardless. Worker-only patch
// drift is not detected here (a future enhancement, like per-node comparison).
func (p *Provisioner) detectInPlaceMachineConfigDrift(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	// Omni owns node configuration via its own API; pushing config directly is
	// not how Omni-managed clusters reconcile. Skip when configs are unavailable.
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

	desiredConfig := p.talosConfigs.ControlPlane()
	if desiredConfig == nil {
		return nil, nil
	}

	return detectInPlaceMachineConfigChanges(runningConfig, desiredConfig), nil
}

// detectInPlaceMachineConfigChanges compares the machine tunables that Talos can
// apply to a running node without a reboot. Both sysctls and sysfs are written
// live to /proc/sys and /sys respectively, so a change is classified in-place.
func detectInPlaceMachineConfigChanges(
	runningConfig, desiredConfig machineClusterConfig,
) []clusterupdate.Change {
	// At most one change per inspected field (sysctls, sysfs).
	changes := make([]clusterupdate.Change, 0, initialChangeCapacity)

	changes = append(changes, detectStringMapFieldChange(
		"machine.sysctls",
		"kernel sysctls re-applied to existing nodes without reboot",
		sysctlsOf(runningConfig),
		sysctlsOf(desiredConfig),
	)...)
	changes = append(changes, detectStringMapFieldChange(
		"machine.sysfs",
		"sysfs attributes re-applied to existing nodes without reboot",
		sysfsOf(runningConfig),
		sysfsOf(desiredConfig),
	)...)

	return changes
}

// detectStringMapFieldChange returns a single in-place change when two string
// maps differ. OldValue/NewValue report the entry count, matching the compact
// before/after style of the change summary table.
func detectStringMapFieldChange(
	field, reason string,
	running, desired map[string]string,
) []clusterupdate.Change {
	if stringMapsEqual(running, desired) {
		return nil
	}

	return []clusterupdate.Change{
		{
			Field:    field,
			OldValue: strconv.Itoa(len(running)),
			NewValue: strconv.Itoa(len(desired)),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   reason,
		},
	}
}

// stringMapsEqual reports whether two string maps have identical entries. A nil
// map and an empty map are treated as equal (both represent "unset").
func stringMapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}

	for key, value := range left {
		if other, ok := right[key]; !ok || other != value {
			return false
		}
	}

	return true
}

// sysctlsOf returns the machine sysctls map, guarding against nil config/machine
// sections so live configs missing a machine stanza compare as "unset".
func sysctlsOf(cfg machineClusterConfig) map[string]string {
	if cfg == nil {
		return nil
	}

	machine := cfg.Machine()
	if machine == nil {
		return nil
	}

	return machine.Sysctls()
}

// sysfsOf returns the machine sysfs map, with the same nil-safety as sysctlsOf.
func sysfsOf(cfg machineClusterConfig) map[string]string {
	if cfg == nil {
		return nil
	}

	machine := cfg.Machine()
	if machine == nil {
		return nil
	}

	return machine.Sysfs()
}

// fetchRunningControlPlaneConfig discovers a control-plane node and returns its
// running Talos machine config. It returns (nil, nil) when no control-plane node
// is reachable so callers can treat "cannot compare" as "no detected drift"
// rather than failing the update. Shared by detectDisruptiveConfigChanges and
// detectInPlaceMachineConfigDrift.
func (p *Provisioner) fetchRunningControlPlaneConfig(
	ctx context.Context,
	clusterName string,
) (machineClusterConfig, bool, error) {
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

	return machineConfig.Config(), true, nil
}
