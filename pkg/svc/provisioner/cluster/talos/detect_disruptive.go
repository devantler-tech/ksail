package talosprovisioner

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfigtypes "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	talosresconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// machineClusterConfig is a minimal interface covering the Machine() and Cluster()
// accessors shared by config/config.Config and config.Provider. Using this instead
// of the full Config interface allows unit tests to construct lightweight v1alpha1
// structs without satisfying the entire interface.
type machineClusterConfig interface {
	Machine() talosconfigtypes.MachineConfig
	Cluster() talosconfigtypes.ClusterConfig
}

// detectDisruptiveConfigChanges compares the desired machine config against the
// running config on each node to detect changes that require special handling.
// It returns classified changes (wipe-required, reboot-required, etc.) that
// should be routed to the appropriate UpdateResult fields.
//
// This detection is separate from the ClusterSpec-level diff engine because
// these settings come from user-managed Talos patch files, not ksail.yaml fields.
func (p *Provisioner) detectDisruptiveConfigChanges(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	if p.talosConfigs == nil || p.omniOpts != nil {
		return nil, nil
	}

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover nodes for config change detection: %w", err)
	}

	// Find the first control-plane node to fetch the running config.
	var cpIP string

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			cpIP = node.IP

			break
		}
	}

	if cpIP == "" {
		return nil, nil
	}

	talosClient, err := p.createTalosClient(ctx, cpIP)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client for config comparison: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	machineConfig, err := safe.StateGet[*talosresconfig.MachineConfig](
		ctx,
		talosClient.COSI,
		talosresconfig.NewMachineConfig(nil).Metadata(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch running machine config from %s: %w", cpIP, err)
	}

	runningConfig := machineConfig.Config()
	desiredConfig := p.talosConfigs.ControlPlane()

	if desiredConfig == nil {
		return nil, nil
	}

	return classifyMachineConfigChanges(runningConfig, desiredConfig), nil
}

// detectVolumeEncryptionChanges compares disk encryption configuration between
// the running and desired Talos machine configs. Returns wipe-required changes
// when encryption is being added, removed, or modified.
//
// Encryption changes require partition wiping because LUKS2 encryption only
// takes effect on empty/unformatted partitions. See:
// https://docs.siderolabs.com/talos/v1.13/configure-your-talos-cluster/storage-and-disk-management/disk-encryption
func detectVolumeEncryptionChanges(
	runningEncryption, desiredEncryption talosconfigtypes.SystemDiskEncryption,
) []clusterupdate.Change {
	var changes []clusterupdate.Change

	// Check EPHEMERAL partition encryption.
	runningEphemeral := encryptionProviderName(runningEncryption, constants.EphemeralPartitionLabel)
	desiredEphemeral := encryptionProviderName(desiredEncryption, constants.EphemeralPartitionLabel)

	if runningEphemeral != desiredEphemeral {
		changes = append(changes, clusterupdate.Change{
			Field:    "machine.systemDiskEncryption.ephemeral",
			OldValue: runningEphemeral,
			NewValue: desiredEphemeral,
			Category: clusterupdate.ChangeCategoryWipeRequired,
			Reason:   "EPHEMERAL partition encryption change requires partition wipe",
		})
	}

	// Check STATE partition encryption.
	runningState := encryptionProviderName(runningEncryption, constants.StatePartitionLabel)
	desiredState := encryptionProviderName(desiredEncryption, constants.StatePartitionLabel)

	if runningState != desiredState {
		changes = append(changes, clusterupdate.Change{
			Field:    "machine.systemDiskEncryption.state",
			OldValue: runningState,
			NewValue: desiredState,
			Category: clusterupdate.ChangeCategoryWipeRequired,
			Reason:   "STATE partition encryption change requires partition wipe and maintenance mode",
		})
	}

	return changes
}

// encryptionProviderName extracts the encryption provider name for a partition.
// Returns "none" if no encryption is configured.
func encryptionProviderName(
	encryption talosconfigtypes.SystemDiskEncryption,
	partitionLabel string,
) string {
	if encryption == nil {
		return "none"
	}

	partConfig := encryption.Get(partitionLabel)
	if partConfig == nil {
		return "none"
	}

	provider := partConfig.Provider()

	return provider.String()
}

// classifyMachineConfigChanges compares the running and desired Talos machine
// configs and returns changes classified by their required apply mode.
// This extends beyond encryption to cover CNI, disk quota, and other Talos-specific
// machine config fields that require special handling.
//
// The Talos SDK handles most changes via NO_REBOOT mode automatically.
// This classifier identifies changes that need special orchestration:
//   - Encryption → ChangeCategoryWipeRequired (partition wipe)
//   - CNI name change → ChangeCategoryRebootRequired (cluster.network.cni change)
//   - Disk quota toggle → ChangeCategoryRebootRequired (machine feature change)
func classifyMachineConfigChanges(
	runningConfig, desiredConfig machineClusterConfig,
) []clusterupdate.Change {
	var changes []clusterupdate.Change

	changes = append(changes, detectVolumeEncryptionChanges(
		runningConfig.Machine().SystemDiskEncryption(),
		desiredConfig.Machine().SystemDiskEncryption(),
	)...)
	changes = append(changes, detectCNIChanges(runningConfig, desiredConfig)...)
	changes = append(changes, detectDiskQuotaChanges(runningConfig, desiredConfig)...)

	return changes
}

// detectCNIChanges compares CNI (Container Network Interface) configuration.
// Changing cluster.network.cni.name (e.g., disabling Flannel) requires a reboot.
func detectCNIChanges(
	runningConfig, desiredConfig machineClusterConfig,
) []clusterupdate.Change {
	runningName := cniName(runningConfig)
	desiredName := cniName(desiredConfig)

	if runningName == desiredName {
		return nil
	}

	return []clusterupdate.Change{
		{
			Field:    "cluster.network.cni.name",
			OldValue: runningName,
			NewValue: desiredName,
			Category: clusterupdate.ChangeCategoryRebootRequired,
			Reason:   "CNI plugin change requires node reboot",
		},
	}
}

// cniName extracts the CNI name from a Talos config.
// Returns empty string if the CNI config is not set.
func cniName(cfg machineClusterConfig) string {
	if cfg == nil {
		return ""
	}

	cluster := cfg.Cluster()
	if cluster == nil {
		return ""
	}

	network := cluster.Network()
	if network == nil {
		return ""
	}

	cni := network.CNI()
	if cni == nil {
		return ""
	}

	return cni.Name()
}

// detectDiskQuotaChanges compares disk quota support configuration.
// Disk quota (machine.features.diskQuotaSupport) requires a reboot to apply
// because it affects the EPHEMERAL partition filesystem mount options.
func detectDiskQuotaChanges(
	runningConfig, desiredConfig machineClusterConfig,
) []clusterupdate.Change {
	runningEnabled := diskQuotaEnabled(runningConfig)
	desiredEnabled := diskQuotaEnabled(desiredConfig)

	if runningEnabled == desiredEnabled {
		return nil
	}

	return []clusterupdate.Change{
		{
			Field:    "machine.features.diskQuotaSupport",
			OldValue: strconv.FormatBool(runningEnabled),
			NewValue: strconv.FormatBool(desiredEnabled),
			Category: clusterupdate.ChangeCategoryRebootRequired,
			Reason:   "disk quota support change requires node reboot",
		},
	}
}

// diskQuotaEnabled extracts the disk quota enabled state from a Talos config.
// Returns false if the features config is not set.
func diskQuotaEnabled(cfg machineClusterConfig) bool {
	if cfg == nil {
		return false
	}

	m := cfg.Machine()
	if m == nil {
		return false
	}

	features := m.Features()
	if features == nil {
		return false
	}

	return features.DiskQuotaSupportEnabled()
}
