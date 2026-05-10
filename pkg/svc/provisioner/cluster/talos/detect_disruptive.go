package talosprovisioner

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config/config"
	talosresconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// detectDisruptiveConfigChanges compares the desired machine config against the
// running config on each node to detect changes that require partition wiping
// (e.g., disk encryption migration). It returns wipe-required changes that
// should be appended to the UpdateResult.
//
// This detection is separate from the ClusterSpec-level diff engine because
// encryption settings come from user-managed Talos patch files, not ksail.yaml fields.
func (p *Provisioner) detectDisruptiveConfigChanges(
	ctx context.Context,
	clusterName string,
) ([]clusterupdate.Change, error) {
	if p.talosConfigs == nil || p.omniOpts != nil {
		return nil, nil
	}

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to discover nodes for disruptive change detection: %w", err)
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

	return detectVolumeEncryptionChanges(
		runningConfig.Machine().SystemDiskEncryption(),
		desiredConfig.Machine().SystemDiskEncryption(),
	), nil
}

// detectVolumeEncryptionChanges compares disk encryption configuration between
// the running and desired Talos machine configs. Returns wipe-required changes
// when encryption is being added, removed, or modified.
//
// Encryption changes require partition wiping because LUKS2 encryption only
// takes effect on empty/unformatted partitions. See:
// https://docs.siderolabs.com/talos/v1.13/configure-your-talos-cluster/storage-and-disk-management/disk-encryption
func detectVolumeEncryptionChanges(
	runningEncryption, desiredEncryption talosconfig.SystemDiskEncryption,
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
	encryption talosconfig.SystemDiskEncryption,
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
