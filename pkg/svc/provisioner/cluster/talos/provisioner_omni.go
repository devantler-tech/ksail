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
	talosVersion, kubernetesVersion := p.resolveOmniVersions()

	// Sync the cluster template to Omni and wait for readiness
	err = p.syncAndWaitOmniCluster(ctx, omniProv, omniprovider.TemplateParams{
		ClusterName:       clusterName,
		TalosVersion:      talosVersion,
		KubernetesVersion: kubernetesVersion,
		ControlPlanes:     p.options.ControlPlaneNodes,
		Workers:           p.options.WorkerNodes,
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

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Kubeconfig saved to %s\n", p.options.KubeconfigPath)
	}

	// Save talosconfig
	if p.options.TalosconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "  Fetching talosconfig from Omni...\n")

		err = p.saveOmniTalosconfig(ctx, omniProv, clusterName)
		if err != nil {
			return fmt.Errorf("failed to save talosconfig: %w", err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Talosconfig saved to %s\n", p.options.TalosconfigPath)
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"\nSuccessfully created Talos cluster %q via Omni\n",
		clusterName,
	)

	return nil
}

// buildOmniPatchInfos converts talosConfigs patches into the PatchInfo format used by the Omni template builder.
func (p *Provisioner) buildOmniPatchInfos() []omniprovider.PatchInfo {
	if p.talosConfigs == nil {
		return nil
	}

	rawPatches := p.talosConfigs.Patches()
	patches := make([]omniprovider.PatchInfo, 0, len(rawPatches))

	for _, patch := range rawPatches {
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
	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for cluster to become ready (timeout: %s)...\n", clusterReadinessTimeout)

	err = omniProv.WaitForClusterReady(ctx, params.ClusterName, clusterReadinessTimeout)
	if err != nil {
		return fmt.Errorf("cluster created but not ready: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

	return nil
}

// resolveOmniVersions determines the Talos and Kubernetes versions for the Omni cluster.
// Priority: omniOpts > talosConfigs defaults.
func (p *Provisioner) resolveOmniVersions() (string, string) {
	var talosVersion, kubernetesVersion string

	if p.omniOpts != nil {
		talosVersion = p.omniOpts.TalosVersion
		kubernetesVersion = p.omniOpts.KubernetesVersion
	}

	if kubernetesVersion == "" && p.talosConfigs != nil {
		kubernetesVersion = p.talosConfigs.KubernetesVersion()
	}

	return talosVersion, kubernetesVersion
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

	kubeconfigPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(kubeconfigPath), stateDirectoryPermissions)
	if err != nil {
		return fmt.Errorf("failed to create kubeconfig directory: %w", err)
	}

	err = os.WriteFile(kubeconfigPath, kubeconfigData, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	return nil
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

	talosconfigPath, err := fsutil.ExpandHomePath(p.options.TalosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to expand talosconfig path: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(talosconfigPath), stateDirectoryPermissions)
	if err != nil {
		return fmt.Errorf("failed to create talosconfig directory: %w", err)
	}

	err = os.WriteFile(talosconfigPath, talosconfigData, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("failed to write talosconfig: %w", err)
	}

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
