package talosprovisioner

import (
	"context"
	"fmt"

	omniprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
)

// createOmniCluster handles cluster creation for Omni-managed Talos clusters.
// Omni manages node lifecycle externally, so this method does not create any
// Docker registries, Docker networks, or local containers. It verifies that
// the cluster exists in Omni and logs success.
func (p *Provisioner) createOmniCluster(ctx context.Context, clusterName string) error {
	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Setting up Talos cluster %q via Omni...\n", clusterName)

	// Verify nodes exist in Omni
	exists, err := omniProv.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists in Omni: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s (no nodes found in Omni)", clustererr.ErrClusterNotFound, clusterName)
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"Successfully verified Talos cluster %q in Omni\n",
		clusterName,
	)

	return nil
}

// deleteOmniCluster handles cluster deletion for Omni-managed Talos clusters.
// It deletes the cluster in Omni (which deallocates machines) and cleans up
// local config files.
func (p *Provisioner) deleteOmniCluster(ctx context.Context, clusterName string) error {
	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	// Check if cluster exists
	exists, err := omniProv.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, clusterName)
	}

	// Delete cluster in Omni (deallocates machines)
	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q in Omni...\n", clusterName)

	err = omniProv.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete Omni cluster: %w", err)
	}

	// Clean up local config files (kubeconfig and talosconfig)
	p.cleanupConfigFiles(clusterName)

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// omniProvider extracts the Omni provider from the infra provider.
func (p *Provisioner) omniProvider() (*omniprovider.Provider, error) {
	omniProv, ok := p.infraProvider.(*omniprovider.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: got %T", ErrOmniProviderRequired, p.infraProvider)
	}

	return omniProv, nil
}
