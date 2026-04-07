package talosprovisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	omniprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
)

// omniProvider extracts the Omni provider from the infra provider.
func (p *Provisioner) omniProvider() (*omniprovider.Provider, error) {
	omniProv, ok := p.infraProvider.(*omniprovider.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: got %T", ErrOmniProviderRequired, p.infraProvider)
	}

	return omniProv, nil
}

// createOmniCluster handles cluster creation for Omni-managed Talos clusters.
// It creates the cluster in Omni using the cluster template sync mechanism,
// waits for the cluster to become ready, and saves kubeconfig/talosconfig.
func (p *Provisioner) createOmniCluster(ctx context.Context, clusterName string) error {
	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Creating Talos cluster %q via Omni...\n", clusterName)

	// Check if cluster already exists
	exists, err := omniProv.ClusterExists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists in Omni: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	// Resolve Talos and Kubernetes versions from Omni options
	talosVersion, kubernetesVersion, err := p.resolveOmniVersions(ctx, omniProv)
	if err != nil {
		return fmt.Errorf("failed to resolve versions: %w", err)
	}

	// Resolve machine allocation: auto-discover available machines when neither
	// machineClass nor machines is configured.
	machines, err := p.resolveOmniMachines(ctx, omniProv)
	if err != nil {
		return fmt.Errorf("failed to resolve machines: %w", err)
	}

	// Sync the cluster template to Omni and wait for readiness
	err = p.syncAndWaitOmniCluster(ctx, omniProv, omniprovider.TemplateParams{
		ClusterName:       clusterName,
		TalosVersion:      talosVersion,
		KubernetesVersion: kubernetesVersion,
		ControlPlanes:     p.options.ControlPlaneNodes,
		Workers:           p.options.WorkerNodes,
		MachineClass:      p.omniMachineClass(),
		Machines:          machines,
		Patches:           p.buildOmniPatchInfos(),
	})
	if err != nil {
		return err
	}

	// Save kubeconfig
	if p.options.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "  Fetching kubeconfig from Omni...\n")

		err = p.saveOmniKubeconfig(ctx, omniProv, clusterName)
		if err != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", err)
		}
	}

	// Save talosconfig
	if p.options.TalosconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "  Fetching talosconfig from Omni...\n")

		err = p.saveOmniTalosconfig(ctx, omniProv, clusterName)
		if err != nil {
			return fmt.Errorf("failed to save talosconfig: %w", err)
		}
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"\nSuccessfully created Talos cluster %q via Omni\n",
		clusterName,
	)

	return nil
}

// buildOmniPatchInfos converts talosConfigs patches into the PatchInfo format used by the Omni template builder.
// Patches that are incompatible with Omni are excluded:
//   - cluster-name.yaml: Omni manages cluster names via its Cluster document
//   - image-verification.yaml: ImageVerificationConfig is a Talos 1.13+ document
//     type not recognized by the Omni SDK
func (p *Provisioner) buildOmniPatchInfos() []omniprovider.PatchInfo {
	if p.talosConfigs == nil {
		return nil
	}

	// Patches that Omni cannot process as config patches.
	omniIncompatible := map[string]bool{
		"cluster-name.yaml":       true,
		"image-verification.yaml": true,
	}

	rawPatches := p.talosConfigs.Patches()
	patches := make([]omniprovider.PatchInfo, 0, len(rawPatches))

	for _, patch := range rawPatches {
		if omniIncompatible[filepath.Base(patch.Path)] {
			continue
		}

		patches = append(patches, omniprovider.PatchInfo{
			Path:    patch.Path,
			Scope:   patch.Scope,
			Content: patch.Content,
		})
	}

	return patches
}

// syncAndWaitOmniCluster builds a cluster template, syncs it to Omni, and waits for readiness.
func (p *Provisioner) syncAndWaitOmniCluster(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	params omniprovider.TemplateParams,
) error {
	templateReader, err := omniprovider.BuildClusterTemplate(params)
	if err != nil {
		return fmt.Errorf("failed to build cluster template: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  Syncing cluster template to Omni...\n")

	err = omniProv.CreateCluster(ctx, templateReader, p.logWriter)
	if err != nil {
		return fmt.Errorf("failed to create cluster in Omni: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster template synced\n")
	_, _ = fmt.Fprintf(
		p.logWriter,
		"  Waiting for cluster to become ready (timeout: %s)...\n",
		clusterReadinessTimeout,
	)

	err = omniProv.WaitForClusterReady(ctx, params.ClusterName, clusterReadinessTimeout)
	if err != nil {
		return fmt.Errorf("cluster created but not ready: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

	return nil
}

// resolveOmniVersions determines the Talos and Kubernetes versions for the Omni cluster.
// TalosVersion comes from omniOpts; when not set, Omni is queried for the latest available version.
// KubernetesVersion comes from omniOpts, then from Omni's compatible versions (only when Omni is
// queried for TalosVersion), and finally from talosConfigs.
func (p *Provisioner) resolveOmniVersions(
	ctx context.Context,
	omniProv *omniprovider.Provider,
) (string, string, error) {
	var talosVersion, kubernetesVersion string

	if p.omniOpts != nil {
		talosVersion = p.omniOpts.TalosVersion
		kubernetesVersion = p.omniOpts.KubernetesVersion
	}

	// Query Omni for the latest available Talos version when not set explicitly.
	if talosVersion == "" {
		latestVersion, compatK8s, err := omniProv.LatestTalosVersion(ctx)
		if err != nil {
			return "", "", fmt.Errorf("failed to determine Talos version: %w", err)
		}

		talosVersion = latestVersion

		// Use the latest compatible Kubernetes version if not set.
		if kubernetesVersion == "" && len(compatK8s) > 0 {
			kubernetesVersion = compatK8s[len(compatK8s)-1]
		}
	}

	if kubernetesVersion == "" && p.talosConfigs != nil {
		kubernetesVersion = p.talosConfigs.KubernetesVersion()
	}

	return talosVersion, kubernetesVersion, nil
}

// omniMachineClass returns the Omni machine class name from options, or empty if not set.
func (p *Provisioner) omniMachineClass() string {
	if p.omniOpts != nil {
		return p.omniOpts.MachineClass
	}

	return ""
}

// omniMachines returns the Omni machine UUIDs from options, or nil if not set.
func (p *Provisioner) omniMachines() []string {
	if p.omniOpts != nil {
		return p.omniOpts.Machines
	}

	return nil
}

// resolveOmniMachines returns the machine UUIDs to use for Omni cluster creation.
// If both machineClass and machines are set, an error is returned (they are mutually
// exclusive). If machines are explicitly configured, those are returned. If a machineClass
// is set, nil is returned (Omni will use the class for dynamic allocation). When neither
// is configured, Omni is queried for available (unallocated) machines and the required
// number of UUIDs is returned.
func (p *Provisioner) resolveOmniMachines(
	ctx context.Context,
	omniProv *omniprovider.Provider,
) ([]string, error) {
	machineClass := p.omniMachineClass()
	machines := p.omniMachines()

	if machineClass != "" && len(machines) > 0 {
		return nil, omniprovider.ErrMachineAllocationConflict
	}

	if len(machines) > 0 {
		return machines, nil
	}

	if machineClass != "" {
		return nil, nil
	}

	// Neither machineClass nor machines set — auto-discover available machines.
	required := p.options.ControlPlaneNodes + p.options.WorkerNodes

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  No machineClass or machines configured; discovering %d available machine(s) in Omni...\n",
		required,
	)

	resolved, err := omniProv.ListAvailableMachines(ctx, required)
	if err != nil {
		return nil, fmt.Errorf("auto-discover available machines: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Discovered %d available machine(s)\n", len(resolved))

	return resolved, nil
}

// saveOmniConfig writes already-fetched config data to disk. It expands/canonicalizes
// the output path, creates parent dirs, and writes the file using configLabel for logs.
func (p *Provisioner) saveOmniConfig(
	configData []byte,
	rawPath string,
	configLabel string,
) error {
	expandedPath, err := fsutil.ExpandHomePath(rawPath)
	if err != nil {
		return fmt.Errorf("failed to expand %s path: %w", configLabel, err)
	}

	err = os.MkdirAll(filepath.Dir(expandedPath), stateDirectoryPermissions)
	if err != nil {
		return fmt.Errorf("failed to create %s directory: %w", configLabel, err)
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(expandedPath)
	if err != nil {
		return fmt.Errorf("failed to canonicalize %s path: %w", configLabel, err)
	}

	err = os.WriteFile(canonicalPath, configData, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", configLabel, err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ %s saved to %s\n", configLabel, canonicalPath)

	return nil
}

// saveOmniKubeconfig retrieves and saves the kubeconfig from Omni.
func (p *Provisioner) saveOmniKubeconfig(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
) error {
	kubeconfigData, err := omniProv.GetKubeconfig(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig from Omni: %w", err)
	}

	return p.saveOmniConfig(kubeconfigData, p.options.KubeconfigPath, "Kubeconfig")
}

// saveOmniTalosconfig retrieves and saves the talosconfig from Omni.
func (p *Provisioner) saveOmniTalosconfig(
	ctx context.Context,
	omniProv *omniprovider.Provider,
	clusterName string,
) error {
	talosconfigData, err := omniProv.GetTalosconfig(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get talosconfig from Omni: %w", err)
	}

	return p.saveOmniConfig(talosconfigData, p.options.TalosconfigPath, "Talosconfig")
}

// deleteOmniCluster handles cluster deletion for Omni-managed Talos clusters.
// It deletes the cluster in Omni (which deallocates machines) and cleans up
// local config files.
func (p *Provisioner) deleteOmniCluster(ctx context.Context, clusterName string) error {
	omniProv, err := p.omniProvider()
	if err != nil {
		return err
	}

	exists, err := omniProv.ClusterExists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, clusterName)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q in Omni...\n", clusterName)

	err = omniProv.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete Omni cluster: %w", err)
	}

	p.cleanupConfigFiles(clusterName)

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}

// getOmniNodesByRole returns nodes with their roles from the Omni API.
// Omni returns machine IDs as node identifiers; the IP field of nodeWithRole
// stores the machine ID (not an IP address) since Omni nodes are addressed
// through the saved talosconfig endpoints, not by direct IP.
func (p *Provisioner) getOmniNodesByRole(
	ctx context.Context,
	clusterName string,
) ([]nodeWithRole, error) {
	omniProv, err := p.omniProvider()
	if err != nil {
		return nil, err
	}

	listed, err := omniProv.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list Omni nodes: %w", err)
	}

	nodes := make([]nodeWithRole, 0, len(listed))

	for _, node := range listed {
		role := RoleWorker
		if node.Role == "controlplane" {
			role = RoleControlPlane
		}

		nodes = append(nodes, nodeWithRole{IP: node.Name, Role: role})
	}

	return nodes, nil
}
