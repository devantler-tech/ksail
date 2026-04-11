package talosprovisioner

import (
	"context"
	"fmt"

	omniprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clusterupdate"
)

// scaleOmniByRole adjusts node counts for an Omni-managed Talos cluster by
// re-syncing the cluster template with the updated control-plane and worker counts.
//
// Unlike Docker/Hetzner scaling which operates on individual nodes, Omni scaling
// is declarative: we rebuild the cluster template with the desired counts and sync
// it to the Omni API, which handles the actual machine allocation/deallocation.
func (p *Provisioner) scaleOmniByRole(
	ctx context.Context,
	clusterName string,
	newCPCount, newWorkerCount int,
	result *clusterupdate.UpdateResult,
) error {
	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	talosVersion, kubernetesVersion, err := p.resolveOmniVersions(ctx, omniProv)
	if err != nil {
		return fmt.Errorf("failed to resolve versions for scaling: %w", err)
	}

	machines, err := p.resolveOmniMachinesForScaling(ctx, omniProv, newCPCount, newWorkerCount)
	if err != nil {
		return fmt.Errorf("failed to resolve machines for scaling: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter,
		"  Syncing updated cluster template to Omni (CP: %d, Workers: %d)...\n",
		newCPCount, newWorkerCount)

	templateReader, err := omniprovider.BuildClusterTemplate(omniprovider.TemplateParams{
		ClusterName:       clusterName,
		TalosVersion:      talosVersion,
		KubernetesVersion: kubernetesVersion,
		ControlPlanes:     newCPCount,
		Workers:           newWorkerCount,
		MachineClass:      p.omniMachineClass(),
		Machines:          machines,
		Patches:           p.buildOmniPatchInfos(),
	})
	if err != nil {
		return fmt.Errorf("failed to build updated cluster template: %w", err)
	}

	err = omniProv.CreateCluster(ctx, templateReader, p.logWriter)
	if err != nil {
		return fmt.Errorf("failed to sync updated template to Omni: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster template synced\n")
	_, _ = fmt.Fprintf(p.logWriter,
		"  Waiting for cluster to become ready after scaling (timeout: %s)...\n",
		clusterReadinessTimeout)

	err = omniProv.WaitForClusterReady(ctx, clusterName, clusterReadinessTimeout)
	if err != nil {
		return fmt.Errorf("cluster not ready after scaling: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

	recordAppliedChange(result, RoleControlPlane, clusterName,
		fmt.Sprintf("scaled to %d", newCPCount))
	recordAppliedChange(result, RoleWorker, clusterName,
		fmt.Sprintf("scaled to %d", newWorkerCount))

	return nil
}

// resolveOmniMachinesForScaling resolves machine UUIDs for scaling operations.
// For machine-class-based allocation, nil is returned (Omni handles dynamic allocation).
// For static machine allocation, the existing machines from omniOpts are returned
// (the caller must ensure the list has enough machines for the new counts).
func (p *Provisioner) resolveOmniMachinesForScaling(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	newCPCount, newWorkerCount int,
) ([]string, error) {
	machineClass := p.omniMachineClass()
	machines := p.omniMachines()

	// Machine-class allocation: Omni manages sizing dynamically.
	if machineClass != "" {
		return nil, nil
	}

	// Static machine allocation: check if we have enough machines.
	if len(machines) > 0 {
		required := newCPCount + newWorkerCount
		if len(machines) < required {
			// Discover additional available machines to fill the gap.
			additionalNeeded := required - len(machines)

			additional, err := omniProv.ListAvailableMachines(ctx, additionalNeeded)
			if err != nil {
				return nil, fmt.Errorf("need %d more machine(s) for scaling: %w", additionalNeeded, err)
			}

			machines = append(machines, additional...)
		}

		return machines, nil
	}

	// Neither machine class nor static machines: auto-discover.
	required := newCPCount + newWorkerCount

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Discovering %d available machine(s) in Omni for scaling...\n",
		required,
	)

	resolved, err := omniProv.ListAvailableMachines(ctx, required)
	if err != nil {
		return nil, fmt.Errorf("auto-discover machines for scaling: %w", err)
	}

	return resolved, nil
}
