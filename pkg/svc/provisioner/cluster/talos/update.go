package talosprovisioner

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
)

// Update applies configuration changes to all nodes in a running Talos cluster.
// It implements the ClusterUpdater interface.
func (p *Provisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	// Compute diff to determine what changed
	diff, diffErr := p.DiffConfig(ctx, name, oldSpec, newSpec)

	result, proceed, prepErr := clusterupdate.PrepareUpdate(
		diff, diffErr, opts, clustererr.ErrRecreationRequired,
	)
	if !proceed {
		return result, prepErr //nolint:wrapcheck // error context added in PrepareUpdate
	}

	clusterName := p.resolveClusterName(name)

	// Handle node scaling changes
	scaleErr := p.applyNodeScalingChanges(ctx, clusterName, oldSpec, newSpec, result)
	if scaleErr != nil {
		return result, fmt.Errorf("failed to apply node scaling changes: %w", scaleErr)
	}

	// Handle in-place config changes (NO_REBOOT mode).
	// Only re-apply machine configs when the provisioner detected actual changes;
	// component-level changes (e.g. loadBalancer) are handled by the reconciler.
	if diff.TotalChanges() > 0 {
		cfgErr := p.applyInPlaceConfigChanges(ctx, clusterName, result)
		if cfgErr != nil {
			return result, fmt.Errorf("failed to apply in-place config changes: %w", cfgErr)
		}
	}

	// Handle reboot-required changes (STAGED mode with rolling reboot)
	if diff.HasRebootRequired() && opts.RollingReboot {
		err := p.applyRebootRequiredChanges(ctx, clusterName, result, opts)
		if err != nil {
			return result, fmt.Errorf("failed to apply reboot-required changes: %w", err)
		}
	}

	return result, nil
}

// DiffConfig computes the differences between current and desired configurations.
func (p *Provisioner) DiffConfig(
	_ context.Context,
	_ string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	// Talos clusters support in-place changes for most config paths.
	result, ok := clusterupdate.NewDiffResult(oldSpec, newSpec)
	if !ok {
		return result, nil
	}

	// Guard: control-plane count must remain >= 1
	if newSpec.Talos.ControlPlanes < 1 {
		return nil, ErrMinimumControlPlanes
	}

	// Compare control plane count
	if oldSpec.Talos.ControlPlanes != newSpec.Talos.ControlPlanes {
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    "talos.controlPlanes",
			OldValue: strconv.Itoa(int(oldSpec.Talos.ControlPlanes)),
			NewValue: strconv.Itoa(int(newSpec.Talos.ControlPlanes)),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "control-plane nodes can be added/removed via provider",
		})
	}

	// Compare worker count
	if oldSpec.Talos.Workers != newSpec.Talos.Workers {
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    "talos.workers",
			OldValue: strconv.Itoa(int(oldSpec.Talos.Workers)),
			NewValue: strconv.Itoa(int(newSpec.Talos.Workers)),
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "worker nodes can be added/removed via provider",
		})
	}

	// Check for network CIDR changes (requires recreate)
	if p.hetznerOpts != nil {
		oldCIDR := oldSpec.Hetzner.NetworkCIDR
		newCIDR := newSpec.Hetzner.NetworkCIDR

		if oldCIDR != newCIDR && oldCIDR != "" && newCIDR != "" {
			result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
				Field:    "hetzner.networkCidr",
				OldValue: oldCIDR,
				NewValue: newCIDR,
				Category: clusterupdate.ChangeCategoryRecreateRequired,
				Reason:   "network CIDR change requires PKI regeneration",
			})
		}
	}

	return result, nil
}

// applyNodeScalingChanges handles adding or removing Talos nodes.
// For Docker: creates or removes containers with static IPs and Talos config.
// For Hetzner: creates or deletes servers via the Hetzner API.
func (p *Provisioner) applyNodeScalingChanges(
	ctx context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) error {
	if oldSpec == nil || newSpec == nil {
		return nil
	}

	cpDelta := int(newSpec.Talos.ControlPlanes - oldSpec.Talos.ControlPlanes)
	workerDelta := int(newSpec.Talos.Workers - oldSpec.Talos.Workers)

	if cpDelta == 0 && workerDelta == 0 {
		return nil
	}

	// Prevent scaling control-plane nodes below 1
	if newSpec.Talos.ControlPlanes < 1 {
		return ErrMinimumControlPlanes
	}

	_, _ = fmt.Fprintf(p.logWriter, "  Node scaling for Talos cluster %q: CP %+d, Workers %+d\n",
		clusterName, cpDelta, workerDelta)

	return p.scaleByProvider(ctx, clusterName, cpDelta, workerDelta, result)
}

// scaleByProvider applies node scaling changes using the appropriate provider backend.
func (p *Provisioner) scaleByProvider(
	ctx context.Context,
	clusterName string,
	cpDelta, workerDelta int,
	result *clusterupdate.UpdateResult,
) error {
	scaleRole := p.scaleDockerByRole
	if p.hetznerOpts != nil {
		scaleRole = p.scaleHetznerByRole
	}

	if cpDelta != 0 {
		err := scaleRole(ctx, clusterName, RoleControlPlane, cpDelta, result)
		if err != nil {
			return err
		}
	}

	if workerDelta != 0 {
		err := scaleRole(ctx, clusterName, RoleWorker, workerDelta, result)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyInPlaceConfigChanges applies configuration changes that don't require reboots.
// Uses ApplyConfiguration with NO_REBOOT mode for Talos-supported fields.
// Control-plane nodes receive the ControlPlane() config and worker nodes receive the Worker() config.
func (p *Provisioner) applyInPlaceConfigChanges(
	ctx context.Context,
	clusterName string,
	result *clusterupdate.UpdateResult,
) error {
	if p.talosConfigs == nil {
		return nil
	}

	// Get nodes with role information from the cluster
	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}

	if len(nodes) == 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  No nodes found for cluster %s\n", clusterName)

		return nil
	}

	// Apply the appropriate config to each node based on its role
	for _, node := range nodes {
		config := p.talosConfigs.ControlPlane()
		if node.Role == RoleWorker {
			config = p.talosConfigs.Worker()
		}

		if config == nil {
			_, _ = fmt.Fprintf(
				p.logWriter, "  ⚠ No config available for %s node %s\n",
				node.Role, node.IP,
			)

			continue
		}

		p.applyNodeConfig(ctx, node, config, result)
	}

	return nil
}

// applyNodeConfig applies the appropriate config to a single node and records the result.
func (p *Provisioner) applyNodeConfig(
	ctx context.Context,
	node nodeWithRole,
	config talosconfig.Provider,
	result *clusterupdate.UpdateResult,
) {
	err := p.applyConfigWithMode(
		ctx,
		node.IP,
		config,
		machineapi.ApplyConfigurationRequest_NO_REBOOT,
	)
	if err != nil {
		_, _ = fmt.Fprintf(
			p.logWriter, "  ⚠ Failed to apply config to %s (%s): %v\n",
			node.IP, node.Role, err,
		)

		result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
			Field:    "talos.config",
			NewValue: node.IP,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   fmt.Sprintf("failed to apply %s config: %v", node.Role, err),
		})
	} else {
		_, _ = fmt.Fprintf(
			p.logWriter, "  ✓ Config applied to %s (%s, no reboot)\n",
			node.IP, node.Role,
		)

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    "talos.config",
			NewValue: node.IP,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   node.Role + " config applied successfully",
		})
	}
}

// applyRebootRequiredChanges applies changes that require node reboots.
// Uses rolling reboot strategy: for each node, apply config with STAGED mode,
// cordon the node (drain workloads), reboot, wait for Ready, then uncordon.
//
// This is not yet implemented because it requires:
//   - Kubernetes client for cordon/drain/uncordon operations
//   - Node readiness polling after reboot
//   - Proper ordering (workers first, then control-planes)
func (p *Provisioner) applyRebootRequiredChanges(
	_ context.Context,
	_ string,
	result *clusterupdate.UpdateResult,
	opts clusterupdate.UpdateOptions,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"  %d changes require reboot (rolling=%v)\n",
		len(result.RebootRequired), opts.RollingReboot)

	// Record as failed changes
	for i := range result.RebootRequired {
		result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
			Field:    result.RebootRequired[i].Field,
			OldValue: result.RebootRequired[i].OldValue,
			NewValue: result.RebootRequired[i].NewValue,
			Category: clusterupdate.ChangeCategoryRebootRequired,
			Reason:   "Talos rolling reboot is not yet implemented",
		})
	}

	return fmt.Errorf("%w: Talos rolling reboot for %d change(s)",
		ErrNotImplemented, len(result.RebootRequired))
}

// applyConfigWithMode applies configuration to a single node with the specified mode.
func (p *Provisioner) applyConfigWithMode(
	ctx context.Context,
	nodeIP string,
	config talosconfig.Provider,
	mode machineapi.ApplyConfigurationRequest_Mode,
) error {
	if config == nil {
		return clustererr.ErrConfigNil
	}

	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return err
	}

	defer talosClient.Close() //nolint:errcheck

	_, err = talosClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
		Mode: mode,
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	return nil
}

// createTalosClient creates a Talos client for the given node.
// It prefers the saved talosconfig on disk (written during cluster creation)
// because it contains the CA and client certificates the running cluster trusts.
// The in-memory talosConfigs bundle may hold freshly generated PKI that the
// cluster has never seen, so it is used only as a fallback.
func (p *Provisioner) createTalosClient(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, error) {
	// Prefer the saved talosconfig (written during cluster creation).
	talosconfigPath, expandErr := fsutil.ExpandHomePath(p.options.TalosconfigPath)
	if expandErr == nil {
		savedCfg, openErr := clientconfig.Open(talosconfigPath)
		if openErr == nil {
			client, err := talosclient.New(ctx,
				talosclient.WithEndpoints(nodeIP),
				talosclient.WithConfig(savedCfg),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create Talos client from saved config: %w", err)
			}

			return client, nil
		}
	}

	// Fallback: use the in-memory bundle's TalosConfig (works for first-time creation).
	if p.talosConfigs != nil && p.talosConfigs.Bundle() != nil {
		if talosConf := p.talosConfigs.Bundle().TalosConfig(); talosConf != nil {
			client, err := talosclient.New(ctx,
				talosclient.WithEndpoints(nodeIP),
				talosclient.WithConfig(talosConf),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create Talos client with config: %w", err)
			}

			return client, nil
		}
	}

	return nil, clustererr.ErrTalosConfigRequired
}

// nodeWithRole holds an IP address and its role for role-aware config application.
type nodeWithRole struct {
	IP   string
	Role string // "control-plane" or "worker"
}

// getNodesByRole returns nodes with their roles for the cluster.
func (p *Provisioner) getNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.dockerClient != nil {
		return p.getDockerNodesByRole(ctx, clusterName)
	}

	return p.getHetznerNodesByRole(ctx, clusterName)
}

// getHetznerNodesByRole gets node IPs and roles from Hetzner servers.
func (p *Provisioner) getHetznerNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.infraProvider == nil {
		return nil, nil
	}

	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return nil, err
	}

	listed, err := p.infraProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list Hetzner nodes: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(listed))

	for _, node := range listed {
		server, serverErr := hzProvider.GetServerByName(ctx, node.Name)
		if serverErr != nil || server == nil {
			continue
		}

		ip := server.PublicNet.IPv4.IP.String()

		nodes = append(nodes, nodeWithRole{IP: ip, Role: node.Role})
	}

	return nodes, nil
}

// getDockerNodesByRole gets node IPs and roles from Docker containers.
// Role is inferred from container names: names containing "controlplane" are control-plane nodes,
// all others are workers.
func (p *Provisioner) getDockerNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	if p.dockerClient == nil {
		return nil, clustererr.ErrDockerClientNotConfigured
	}

	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(containers))

	for _, ctr := range containers {
		role := RoleWorker

		for _, name := range ctr.Names {
			// Match both "controlplane" (KSail-scaled nodes) and "control-plane"
			// (Talos SDK-created nodes) naming conventions.
			if strings.Contains(name, "controlplane") || strings.Contains(name, "control-plane") {
				role = RoleControlPlane

				break
			}
		}

		for _, network := range ctr.NetworkSettings.Networks {
			if network.IPAddress != "" {
				nodes = append(nodes, nodeWithRole{
					IP:   network.IPAddress,
					Role: role,
				})

				break
			}
		}
	}

	return nodes, nil
}

// GetCurrentConfig retrieves the current cluster configuration by probing the
// running cluster through the Kubernetes API and Docker/Hetzner providers.
func (p *Provisioner) GetCurrentConfig(ctx context.Context) (*v1alpha1.ClusterSpec, error) {
	var provider v1alpha1.Provider
	if p.dockerClient != nil {
		provider = v1alpha1.ProviderDocker
	} else if p.infraProvider != nil {
		provider = v1alpha1.ProviderHetzner
	}

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, provider)

	// Detect installed components from the live cluster when the detector is available.
	if p.componentDetector != nil {
		detected, err := p.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionTalos,
			provider,
		)
		if err == nil {
			spec.CNI = detected.CNI
			spec.CSI = detected.CSI
			spec.MetricsServer = detected.MetricsServer
			spec.LoadBalancer = detected.LoadBalancer
			spec.CertManager = detected.CertManager
			spec.PolicyEngine = detected.PolicyEngine
			spec.GitOpsEngine = detected.GitOpsEngine
		}
	}

	// Introspect actual node counts from the running cluster
	// to avoid false-positive diffs from hardcoded defaults.
	cpCount, workerCount := p.introspectNodeCounts(ctx)
	spec.Talos = v1alpha1.OptionsTalos{
		ControlPlanes: int32(cpCount),
		Workers:       int32(workerCount),
	}

	// If we have Hetzner options configured
	if p.hetznerOpts != nil {
		spec.Hetzner = v1alpha1.OptionsHetzner{
			NetworkCIDR: p.hetznerOpts.NetworkCIDR,
		}
	}

	clusterupdate.ApplyGitOpsLocalRegistryDefault(spec)

	return spec, nil
}

// introspectNodeCounts determines the actual control-plane and worker node
// counts from the running cluster. Falls back to safe defaults (1 CP, 0 workers)
// when the cluster cannot be queried.
func (p *Provisioner) introspectNodeCounts(ctx context.Context) (int, int) {
	clusterName := p.resolveClusterName("")

	if p.dockerClient != nil {
		nodes, err := p.getDockerNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	if p.infraProvider != nil {
		nodes, err := p.getHetznerNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	return 1, 0
}

// countNodeRoles counts control-plane and worker nodes from a list of nodeWithRole.
func countNodeRoles(nodes []nodeWithRole) (int, int) {
	var cp, worker int

	for _, n := range nodes {
		switch n.Role {
		case RoleControlPlane:
			cp++
		case RoleWorker:
			worker++
		}
	}

	if cp == 0 {
		cp = 1
	}

	return cp, worker
}
