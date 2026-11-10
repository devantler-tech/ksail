package talosprovisioner

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	talosconfigtypes "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// errUnknownAutoscalerPool reports an autoscaler server whose pool is not configured,
// so the worker config it booted from cannot be rebuilt.
var errUnknownAutoscalerPool = errors.New(
	"autoscaler node pool is not in spec.cluster.autoscaler.node.pools, so its boot config is unknown",
)

var errInvalidAutoscalerPoolPatch = errors.New("invalid Talos machine configuration")

// autoscalerNode returns the worker node for a server the cluster autoscaler
// provisioned, carrying the pool whose autoscaler worker config the server booted
// from. A server of a pool that is not configured is refused before anything
// touches it, because rebuilding its config would have to guess its shape.
func (p *Provisioner) autoscalerNode(
	server *hcloud.Server,
	talosAddress string,
) (nodeWithRole, error) {
	poolName := server.Labels[hetzner.LabelAutoscalerNodeGroup]

	_, err := p.autoscalerNodePool(poolName)
	if err != nil {
		return nodeWithRole{}, fmt.Errorf("autoscaler node %s: %w", server.Name, err)
	}

	return nodeWithRole{IP: talosAddress, Role: RoleWorker, AutoscalerPool: poolName}, nil
}

// autoscalerNodePool returns the configured autoscaler pool with the given name.
func (p *Provisioner) autoscalerNodePool(name string) (v1alpha1.NodePool, error) {
	if p.hetznerOpts != nil {
		for _, pool := range p.hetznerOpts.AutoscalerNodePools {
			if pool.Name == name {
				return pool, nil
			}
		}
	}

	return v1alpha1.NodePool{}, fmt.Errorf("%w: %q", errUnknownAutoscalerPool, name)
}

// buildDesiredConfigForNode rebuilds the machine config KSail wants on node. A node
// the cluster autoscaler provisioned booted from its pool's autoscaler worker config
// rather than the static worker config, so its rebuild gets the same shape: the
// ksail.io/autoscaled marker and the pool's labels and taints stay, and the Longhorn
// default-disk label and machine.disks stay off (#7013).
func (p *Provisioner) buildDesiredConfigForNode(
	running, secretsSource talosconfig.Provider,
	node nodeWithRole,
) (talosconfig.Provider, error) {
	if node.AutoscalerPool == "" {
		return p.buildDesiredNodeConfig(running, secretsSource, node.Role)
	}

	pool, err := p.autoscalerNodePool(node.AutoscalerPool)
	if err != nil {
		return nil, err
	}

	desired, err := p.buildDesiredNodeConfig(
		running, secretsSource, node.Role, pool.TalosPatches...,
	)
	if err != nil {
		return nil, fmt.Errorf("building config for autoscaler pool %q: %w", pool.Name, err)
	}

	return shapeAutoscalerWorker(desired, pool.Labels, poolTaintsToCoreV1(pool.Taints))
}

// applyAutoscalerPoolPatches uses Talos's own patch semantics without changing
// the shared worker config. Errors identify the patch without printing its values.
func applyAutoscalerPoolPatches(
	worker talosconfig.Provider,
	patches []string,
) (talosconfig.Provider, error) {
	if len(patches) == 0 {
		return worker, nil
	}

	if worker == nil {
		return nil, ErrNilWorkerConfig
	}

	parsed := make([]configpatcher.Patch, 0, len(patches))
	for index, patch := range patches {
		loaded, err := configpatcher.LoadPatch([]byte(patch))
		if err != nil {
			return nil, fmt.Errorf(
				"pool machine config patch %d: %w",
				index+1,
				errInvalidAutoscalerPoolPatch,
			)
		}

		parsed = append(parsed, loaded)
	}

	patched, err := configpatcher.Apply(configpatcher.WithConfig(worker), parsed)
	if err != nil {
		return nil, fmt.Errorf(
			"applying pool machine config patches: %w",
			errInvalidAutoscalerPoolPatch,
		)
	}

	// Reload the merged documents before validation and drift analysis. The
	// Talos SDK can retain the previous size sign in memory when a patch
	// replaces a negative reserve with an absolute cap.
	_, err = patched.Bytes()
	if err != nil {
		return nil, fmt.Errorf(
			"encoding patched pool machine config: %w",
			errInvalidAutoscalerPoolPatch,
		)
	}

	config, err := patched.Config()
	if err != nil {
		return nil, fmt.Errorf(
			"loading patched pool machine config: %w",
			errInvalidAutoscalerPoolPatch,
		)
	}

	return config, nil
}

// validateAutoscalerPoolPatches rejects invalid configured pool templates before
// provisioning or planning an update. Unconfigured pools keep the existing path.
func (p *Provisioner) validateAutoscalerPoolPatches() error {
	if p.hetznerOpts == nil || !p.hetznerOpts.NodeAutoscalerEnabled {
		return nil
	}

	for _, pool := range p.hetznerOpts.AutoscalerNodePools {
		if len(pool.TalosPatches) == 0 {
			continue
		}

		if p.talosConfigs == nil {
			return ErrNilWorkerConfig
		}

		err := validateAutoscalerPoolPatch(p.talosConfigs.Worker(), pool)
		if err != nil {
			return fmt.Errorf("validating autoscaler pool %q: %w", pool.Name, err)
		}
	}

	return nil
}

func validateAutoscalerPoolPatch(worker talosconfig.Provider, pool v1alpha1.NodePool) error {
	config, err := applyAutoscalerPoolPatches(worker, pool.TalosPatches)
	if err != nil {
		return err
	}

	_, err = config.ValidateAsClient(hetznerPoolValidationMode{})
	if err != nil {
		return errInvalidAutoscalerPoolPatch
	}

	// The update planner classifies reboot and wipe operations at cluster scope.
	// A pool override must never enter the ordinary NO_REBOOT convergence path.
	if changes := classifyMachineConfigChanges(worker, config); len(changes) > 0 {
		return fmt.Errorf(
			"%w: %s requires cluster-wide update orchestration; configure it in the shared Talos patches",
			errInvalidAutoscalerPoolPatch,
			changes[0].Field,
		)
	}

	// Talos also stores system-volume encryption in separate VolumeConfig
	// documents. Neither a provider change nor a change to an existing
	// system-volume encryption policy belongs in a pool's NO_REBOOT update.
	systemVolumes := []string{
		constants.StatePartitionLabel,
		constants.EphemeralPartitionLabel,
		constants.ImageCachePartitionLabel,
	}
	for _, volume := range systemVolumes {
		sharedEncryption := systemVolumeEncryption(worker, volume)
		poolEncryption := systemVolumeEncryption(config, volume)

		if !reflect.DeepEqual(sharedEncryption, poolEncryption) {
			return fmt.Errorf(
				"%w: VolumeConfig/%s encryption cannot change in pool patches",
				errInvalidAutoscalerPoolPatch,
				volume,
			)
		}
	}

	bootConfig, err := GenerateAutoscalerWorkerConfig(
		config, pool.Labels, poolTaintsToCoreV1(pool.Taints),
	)
	if err != nil {
		return err
	}

	_, err = compressWorkerConfigToUserData(bootConfig)

	return err
}

func systemVolumeEncryption(
	provider talosconfig.Provider,
	name string,
) talosconfigtypes.EncryptionConfig {
	volume, _ := provider.Volumes().ByName(name)
	if encryption := volume.Encryption(); encryption != nil {
		return encryption
	}

	if encryption := provider.Machine().SystemDiskEncryption(); encryption != nil {
		return encryption.Get(name)
	}

	return nil
}

// Hetzner nodes boot a Talos cloud image and do not use the metal installer.
type hetznerPoolValidationMode struct{}

func (hetznerPoolValidationMode) String() string        { return "cloud" }
func (hetznerPoolValidationMode) RequiresInstall() bool { return false }
func (hetznerPoolValidationMode) InContainer() bool     { return false }

// externallyManagedNodeGroup reports whether name is a node group the user declared
// in autoscalerNodePoolNames without defining it as a KSail autoscaler pool. Such a
// group belongs to an autoscaler the user runs themselves: KSail has no pool
// definition to converge its servers with, and nothing about them is left over, so
// they are neither acted on nor reported. A name KSail derived from its own pools is
// in both lists and is therefore never externally managed.
func (p *Provisioner) externallyManagedNodeGroup(name string) bool {
	if p.hetznerOpts == nil || !slices.Contains(p.hetznerOpts.AutoscalerNodePoolNames, name) {
		return false
	}

	return !slices.ContainsFunc(
		p.hetznerOpts.AutoscalerNodePools,
		func(pool v1alpha1.NodePool) bool { return pool.Name == name },
	)
}
