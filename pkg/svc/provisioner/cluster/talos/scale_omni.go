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
	oldCPCount, oldWorkerCount int,
	newCPCount, newWorkerCount int,
	result *clusterupdate.UpdateResult,
) error {
	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	err = p.syncOmniScaling(ctx, omniProv, clusterName, newCPCount, newWorkerCount)
	if err != nil {
		return err
	}

	if newCPCount != oldCPCount {
		recordAppliedChange(result, RoleControlPlane, clusterName,
			fmt.Sprintf("scaled to %d", newCPCount))
	}

	if newWorkerCount != oldWorkerCount {
		recordAppliedChange(result, RoleWorker, clusterName,
			fmt.Sprintf("scaled to %d", newWorkerCount))
	}

	return nil
}

// syncOmniScaling builds an updated cluster template with the desired node counts,
// syncs it to Omni, and waits for the cluster to become ready.
func (p *Provisioner) syncOmniScaling(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
	newCPCount, newWorkerCount int,
) error {
	talosVersion, kubernetesVersion, err := p.resolveOmniVersions(ctx, omniProv)
	if err != nil {
		return fmt.Errorf("failed to resolve versions for scaling: %w", err)
	}

	machines, err := p.resolveOmniMachinesForScaling(
		ctx, omniProv, clusterName, newCPCount, newWorkerCount,
	)
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

	return nil
}

// resolveOmniMachinesForScaling resolves machine UUIDs for scaling operations.
// For machine-class-based allocation, nil is returned (Omni handles dynamic allocation).
// For static machine allocation, the configured list is returned; if too short,
// additional available machines are discovered via ListAvailableMachines.
// When neither is configured, existing cluster machines are fetched and supplemented
// with newly discovered machines to reach the required total.
func (p *Provisioner) resolveOmniMachinesForScaling(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
	newCPCount, newWorkerCount int,
) ([]string, error) {
	machineClass := p.omniMachineClass()
	machines := p.omniMachines()

	// Reject ambiguous allocation: machine class and static machines are mutually exclusive.
	if machineClass != "" && len(machines) > 0 {
		return nil, omniprovider.ErrMachineAllocationConflict
	}

	// Machine-class allocation: Omni manages sizing dynamically.
	if machineClass != "" {
		return nil, nil
	}

	// Static machine allocation: check if we have enough machines.
	if len(machines) > 0 {
		return p.expandStaticMachinesForScaling(ctx, omniProv, machines, newCPCount+newWorkerCount)
	}

	// Neither machine class nor static machines: fetch existing cluster machines
	// and discover only the additional ones needed for the new counts.
	return p.discoverMachinesForScaling(ctx, omniProv, clusterName, newCPCount+newWorkerCount)
}

// expandStaticMachinesForScaling appends additional available machines when the
// configured static list is shorter than the required total.
func (p *Provisioner) expandStaticMachinesForScaling(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	machines []string,
	required int,
) ([]string, error) {
	if len(machines) >= required {
		return machines, nil
	}

	additionalNeeded := required - len(machines)

	additional, err := omniProv.ListAvailableMachines(ctx, additionalNeeded)
	if err != nil {
		return nil, fmt.Errorf("need %d more machine(s) for scaling: %w", additionalNeeded, err)
	}

	return append(machines, additional...), nil
}

// discoverMachinesForScaling fetches existing cluster machines and discovers
// only the additional ones needed to reach the required total. The returned list
// preserves role ordering: control-plane machines first, then workers, with any
// newly discovered machines appended at the end. This matches the Omni template
// positional semantics (first N machines become control planes).
func (p *Provisioner) discoverMachinesForScaling(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
	required int,
) ([]string, error) {
	existingNodes, err := omniProv.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list existing cluster machines: %w", err)
	}

	// Build the machine list with control-plane nodes first to preserve
	// Omni template positional semantics.
	cpIDs := make([]string, 0, len(existingNodes))
	workerIDs := make([]string, 0, len(existingNodes))

	for _, n := range existingNodes {
		if n.Role == omniRoleControlPlane {
			cpIDs = append(cpIDs, n.Name)
		} else {
			workerIDs = append(workerIDs, n.Name)
		}
	}

	machines := make([]string, 0, len(existingNodes))
	machines = append(machines, cpIDs...)
	machines = append(machines, workerIDs...)

	additionalNeeded := required - len(machines)
	if additionalNeeded <= 0 {
		return machines, nil
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Discovering %d additional machine(s) in Omni for scaling...\n",
		additionalNeeded,
	)

	additional, err := omniProv.ListAvailableMachines(ctx, additionalNeeded)
	if err != nil {
		return nil, fmt.Errorf("auto-discover machines for scaling: %w", err)
	}

	return append(machines, additional...), nil
}
