package talosprovisioner

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
)

// Update applies configuration changes to all nodes in a running Talos cluster.
// It implements the ClusterUpdater interface.
func (p *TalosProvisioner) Update(
	ctx context.Context,
	name string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	opts types.UpdateOptions,
) (*types.UpdateResult, error) {
	// Compute diff to determine what changed
	diff, err := p.DiffConfig(ctx, name, oldSpec, newSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config diff: %w", err)
	}

	if opts.DryRun {
		return diff, nil
	}

	result := &types.UpdateResult{
		InPlaceChanges:   diff.InPlaceChanges,
		RebootRequired:   diff.RebootRequired,
		RecreateRequired: diff.RecreateRequired,
		AppliedChanges:   make([]types.Change, 0),
		FailedChanges:    make([]types.Change, 0),
	}

	// If there are recreate-required changes, we cannot handle them in Update
	if diff.HasRecreateRequired() {
		return result, fmt.Errorf("%w: %d changes require restart",
			clustererrors.ErrRecreationRequired, len(diff.RecreateRequired))
	}

	clusterName := p.resolveClusterName(name)

	// Handle node scaling changes
	err = p.applyNodeScalingChanges(ctx, clusterName, oldSpec, newSpec, result)
	if err != nil {
		return result, fmt.Errorf("failed to apply node scaling changes: %w", err)
	}

	// Handle in-place config changes (NO_REBOOT mode)
	err = p.applyInPlaceConfigChanges(ctx, clusterName, result)
	if err != nil {
		return result, fmt.Errorf("failed to apply in-place config changes: %w", err)
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
func (p *TalosProvisioner) DiffConfig(
	_ context.Context,
	_ string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
) (*types.UpdateResult, error) {
	result := &types.UpdateResult{
		InPlaceChanges:   make([]types.Change, 0),
		RebootRequired:   make([]types.Change, 0),
		RecreateRequired: make([]types.Change, 0),
	}

	if oldSpec == nil || newSpec == nil {
		return result, nil
	}

	// Compare control plane count
	if oldSpec.Talos.ControlPlanes != newSpec.Talos.ControlPlanes {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "talos.controlPlanes",
			OldValue: strconv.Itoa(int(oldSpec.Talos.ControlPlanes)),
			NewValue: strconv.Itoa(int(newSpec.Talos.ControlPlanes)),
			Category: types.ChangeCategoryInPlace,
			Reason:   "control-plane nodes can be added/removed via provider",
		})
	}

	// Compare worker count
	if oldSpec.Talos.Workers != newSpec.Talos.Workers {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "talos.workers",
			OldValue: strconv.Itoa(int(oldSpec.Talos.Workers)),
			NewValue: strconv.Itoa(int(newSpec.Talos.Workers)),
			Category: types.ChangeCategoryInPlace,
			Reason:   "worker nodes can be added/removed via provider",
		})
	}

	// Check for network CIDR changes (requires recreate)
	if p.hetznerOpts != nil {
		oldCIDR := oldSpec.Hetzner.NetworkCIDR
		newCIDR := newSpec.Hetzner.NetworkCIDR

		if oldCIDR != newCIDR && oldCIDR != "" && newCIDR != "" {
			result.RecreateRequired = append(result.RecreateRequired, types.Change{
				Field:    "hetzner.networkCidr",
				OldValue: oldCIDR,
				NewValue: newCIDR,
				Category: types.ChangeCategoryRecreateRequired,
				Reason:   "network CIDR change requires PKI regeneration",
			})
		}
	}

	return result, nil
}

// applyNodeScalingChanges handles adding or removing nodes.
//
//nolint:unparam // result will be used when node scaling is fully implemented
func (p *TalosProvisioner) applyNodeScalingChanges(
	_ context.Context,
	clusterName string,
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	_ *types.UpdateResult,
) error {
	if oldSpec == nil || newSpec == nil {
		return nil
	}

	// Calculate differences
	cpDelta := int(newSpec.Talos.ControlPlanes - oldSpec.Talos.ControlPlanes)
	workerDelta := int(newSpec.Talos.Workers - oldSpec.Talos.Workers)

	if cpDelta == 0 && workerDelta == 0 {
		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "  Scaling cluster %s: CP %+d, Workers %+d\n",
		clusterName, cpDelta, workerDelta)

	// For now, log what would happen - actual implementation depends on provider
	if cpDelta > 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  TODO: Add %d control-plane node(s)\n", cpDelta)
	} else if cpDelta < 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  TODO: Remove %d control-plane node(s)\n", -cpDelta)
	}

	if workerDelta > 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  TODO: Add %d worker node(s)\n", workerDelta)
	} else if workerDelta < 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  TODO: Remove %d worker node(s)\n", -workerDelta)
	}

	return nil
}

// applyInPlaceConfigChanges applies configuration changes that don't require reboots.
// Uses ApplyConfiguration with NO_REBOOT mode for Talos-supported fields.
func (p *TalosProvisioner) applyInPlaceConfigChanges(
	ctx context.Context,
	clusterName string,
	_ *types.UpdateResult,
) error {
	if p.talosConfigs == nil {
		return nil
	}

	// Get node IPs from the cluster
	nodeIPs, err := p.getNodeIPs(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get node IPs: %w", err)
	}

	if len(nodeIPs) == 0 {
		_, _ = fmt.Fprintf(p.logWriter, "  No nodes found for cluster %s\n", clusterName)

		return nil
	}

	// Apply config to each node with NO_REBOOT mode
	for _, nodeIP := range nodeIPs {
		err := p.applyConfigWithMode(
			ctx,
			nodeIP,
			p.talosConfigs.ControlPlane(),
			machineapi.ApplyConfigurationRequest_NO_REBOOT,
		)
		if err != nil {
			_, _ = fmt.Fprintf(p.logWriter, "  ⚠ Failed to apply config to %s: %v\n", nodeIP, err)
			// Continue with other nodes
		} else {
			_, _ = fmt.Fprintf(p.logWriter, "  ✓ Config applied to %s (no reboot)\n", nodeIP)
		}
	}

	return nil
}

// applyRebootRequiredChanges applies changes that require node reboots.
// Uses rolling reboot strategy when opts.RollingReboot is true.
//
//nolint:unparam // result will be used when rolling reboot is implemented
func (p *TalosProvisioner) applyRebootRequiredChanges(
	_ context.Context,
	_ string,
	result *types.UpdateResult,
	opts types.UpdateOptions,
) error {
	_, _ = fmt.Fprintf(p.logWriter,
		"  %d changes require reboot (rolling=%v)\n",
		len(result.RebootRequired), opts.RollingReboot)

	// Rolling reboot strategy (not yet implemented):
	// 1. Get list of nodes
	// 2. For each node:
	//    a. Apply config with STAGED mode
	//    b. Cordon the node (drain workloads)
	//    c. Reboot the node
	//    d. Wait for node to be Ready
	//    e. Uncordon the node
	// 3. Move to next node

	return nil
}

// applyConfigWithMode applies configuration to a single node with the specified mode.
func (p *TalosProvisioner) applyConfigWithMode(
	ctx context.Context,
	nodeIP string,
	config talosconfig.Provider,
	mode machineapi.ApplyConfigurationRequest_Mode,
) error {
	if config == nil {
		return clustererrors.ErrConfigNil
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

// createTalosClient creates a Talos client for the given node, using TalosConfig credentials if available.
func (p *TalosProvisioner) createTalosClient(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, error) {
	// If we have talos config bundle, use its TLS credentials
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

	// Fallback: insecure client for maintenance mode
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // Local development clusters use self-signed certs
	}

	client, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithTLSConfig(tlsConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create insecure Talos client: %w", err)
	}

	return client, nil
}

// getNodeIPs returns the IPs of all nodes in the cluster.
func (p *TalosProvisioner) getNodeIPs(ctx context.Context, clusterName string) ([]string, error) {
	// For Docker provider, get IPs from Docker containers
	if p.dockerClient != nil {
		return p.getDockerNodeIPs(ctx, clusterName)
	}

	// For Hetzner provider, get IPs from Hetzner API
	if p.infraProvider != nil {
		return p.getHetznerNodeIPs(ctx, clusterName)
	}

	return nil, clustererrors.ErrNoProviderConfigured
}

// getDockerNodeIPs gets node IPs from Docker containers.
func (p *TalosProvisioner) getDockerNodeIPs(
	ctx context.Context,
	clusterName string,
) ([]string, error) {
	if p.dockerClient == nil {
		return nil, clustererrors.ErrDockerClientNotConfigured
	}

	// List containers with the Talos cluster label
	containers, err := p.dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", LabelTalosClusterName+"="+clusterName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	ips := make([]string, 0, len(containers))

	for _, c := range containers {
		for _, network := range c.NetworkSettings.Networks {
			if network.IPAddress != "" {
				ips = append(ips, network.IPAddress)

				break
			}
		}
	}

	return ips, nil
}

// getHetznerNodeIPs gets node IPs from Hetzner servers.
func (p *TalosProvisioner) getHetznerNodeIPs(
	_ context.Context,
	_ string,
) ([]string, error) {
	// For now, return empty - would need Hetzner client to list servers
	// The actual implementation would query Hetzner API for servers with matching labels
	return nil, nil
}

// getTalosNoRebootPaths returns the list of machine config paths that can be changed without reboot.
// Based on Talos documentation:
// https://www.talos.dev/v1.9/talos-guides/configuration/editing-machine-configuration/
func getTalosNoRebootPaths() []string {
	return []string{
		".cluster",
		".machine.network",
		".machine.kubelet",
		".machine.registries",
		".machine.nodeLabels",
		".machine.nodeTaints",
		".machine.time",
		".machine.sysfs",
		".machine.sysctls",
		".machine.logging",
		".machine.pods",
		".machine.kernel",
	}
}

// getTalosRebootRequiredPaths returns the list of machine config paths that require reboot.
func getTalosRebootRequiredPaths() []string {
	return []string{
		".machine.install",
		".machine.disks",
	}
}

// ClassifyTalosPatch determines the reboot requirement for a given Talos config path.
func ClassifyTalosPatch(path string) types.ChangeCategory {
	// Check no-reboot paths first
	for _, p := range getTalosNoRebootPaths() {
		if pathMatches(path, p) {
			return types.ChangeCategoryInPlace
		}
	}

	// Check reboot-required paths
	for _, p := range getTalosRebootRequiredPaths() {
		if pathMatches(path, p) {
			return types.ChangeCategoryRebootRequired
		}
	}

	// Default to reboot for unknown paths (safer)
	return types.ChangeCategoryRebootRequired
}

// pathMatches checks if a config path matches a pattern.
func pathMatches(path, pattern string) bool {
	// Simple prefix matching for now
	return len(path) >= len(pattern) && path[:len(pattern)] == pattern
}

// GetCurrentConfig retrieves the current cluster configuration.
// For Talos clusters, we return the configuration from the TalosConfigs.
func (p *TalosProvisioner) GetCurrentConfig() (*v1alpha1.ClusterSpec, error) {
	spec := &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionTalos,
	}

	// Determine provider
	if p.dockerClient != nil {
		spec.Provider = v1alpha1.ProviderDocker
	} else if p.infraProvider != nil {
		spec.Provider = v1alpha1.ProviderHetzner
	}

	// Set Talos-specific options from the provisioner state
	spec.Talos = v1alpha1.OptionsTalos{
		ControlPlanes: 1, // Default, actual value would need cluster inspection
		Workers:       0,
	}

	// If we have Hetzner options configured
	if p.hetznerOpts != nil {
		spec.Hetzner = v1alpha1.OptionsHetzner{
			NetworkCIDR: p.hetznerOpts.NetworkCIDR,
		}
	}

	return spec, nil
}
