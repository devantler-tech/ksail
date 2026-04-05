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
	talosVersion, kubernetesVersion, err := p.resolveOmniVersions()
	if err != nil {
		return err
	}

	// Load patches from distribution config directory if available
	var patches []omniprovider.PatchInfo

	if p.talosConfigs != nil {
		for _, patch := range p.talosConfigs.Patches() {
			patches = append(patches, omniprovider.PatchInfo{
				Path:    patch.Path,
				Scope:   patch.Scope,
				Content: patch.Content,
			})
		}
	}

	// Build the cluster template
	templateReader, err := omniprovider.BuildClusterTemplate(omniprovider.TemplateParams{
		ClusterName:       clusterName,
		TalosVersion:      talosVersion,
		KubernetesVersion: kubernetesVersion,
		ControlPlanes:     int32(p.options.ControlPlaneNodes),
		Workers:           int32(p.options.WorkerNodes),
		Patches:           patches,
	})
	if err != nil {
		return fmt.Errorf("failed to build cluster template: %w", err)
	}

	// Sync the template to Omni (creates cluster resources)
	_, _ = fmt.Fprintf(p.logWriter, "  Syncing cluster template to Omni...\n")

	err = omniProv.CreateCluster(ctx, templateReader, p.logWriter)
	if err != nil {
		return fmt.Errorf("failed to create cluster in Omni: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster template synced\n")

	// Wait for cluster to become ready
	_, _ = fmt.Fprintf(p.logWriter, "  Waiting for cluster to become ready (timeout: %s)...\n", clusterReadinessTimeout)

	err = omniProv.WaitForClusterReady(ctx, clusterName, clusterReadinessTimeout)
	if err != nil {
		return fmt.Errorf("cluster created but not ready: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")

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

// resolveOmniVersions determines the Talos and Kubernetes versions for the Omni cluster.
// TalosVersion must be explicitly set in omniOpts (Omni manages its own Talos versions
// independently from the local SDK version contract).
// KubernetesVersion resolves with priority: omniOpts > talosConfigs defaults.
func (p *Provisioner) resolveOmniVersions() (talosVersion, kubernetesVersion string, err error) {
	if p.omniOpts != nil {
		talosVersion = p.omniOpts.TalosVersion
		kubernetesVersion = p.omniOpts.KubernetesVersion
	}

	if kubernetesVersion == "" && p.talosConfigs != nil {
		kubernetesVersion = p.talosConfigs.KubernetesVersion()
	}

	if talosVersion == "" {
		return "", "", fmt.Errorf("%w: set spec.cluster.omni.talosVersion in ksail.yaml", omniprovider.ErrTalosVersionRequired)
	}

	if kubernetesVersion == "" {
		return "", "", fmt.Errorf("%w: set spec.cluster.omni.kubernetesVersion in ksail.yaml", omniprovider.ErrKubernetesVersionRequired)
	}

	return talosVersion, kubernetesVersion, nil
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

	if err = os.MkdirAll(filepath.Dir(kubeconfigPath), stateDirectoryPermissions); err != nil {
		return fmt.Errorf("failed to create kubeconfig directory: %w", err)
	}

	kubeconfigPath, err = fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to canonicalize kubeconfig path: %w", err)
	}

	if err = os.WriteFile(kubeconfigPath, kubeconfigData, kubeconfigFileMode); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Kubeconfig saved to %s\n", kubeconfigPath)

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

	if err = os.MkdirAll(filepath.Dir(talosconfigPath), stateDirectoryPermissions); err != nil {
		return fmt.Errorf("failed to create talosconfig directory: %w", err)
	}

	talosconfigPath, err = fsutil.EvalCanonicalPath(talosconfigPath)
	if err != nil {
		return fmt.Errorf("failed to canonicalize talosconfig path: %w", err)
	}

	if err = os.WriteFile(talosconfigPath, talosconfigData, kubeconfigFileMode); err != nil {
		return fmt.Errorf("failed to write talosconfig: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Talosconfig saved to %s\n", talosconfigPath)

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
