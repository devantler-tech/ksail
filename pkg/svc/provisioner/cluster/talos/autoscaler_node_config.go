package talosprovisioner

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
)

// errUnknownAutoscalerPool reports an autoscaler server whose pool is not configured,
// so the worker config it booted from cannot be rebuilt.
var errUnknownAutoscalerPool = errors.New(
	"autoscaler node pool is not in spec.cluster.autoscaler.node.pools, so its boot config is unknown",
)

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
	desired, err := p.buildDesiredNodeConfig(running, secretsSource, node.Role)
	if err != nil {
		return nil, err
	}

	if node.AutoscalerPool == "" {
		return desired, nil
	}

	pool, err := p.autoscalerNodePool(node.AutoscalerPool)
	if err != nil {
		return nil, err
	}

	return shapeAutoscalerWorker(desired, pool.Labels, poolTaintsToCoreV1(pool.Taints))
}
