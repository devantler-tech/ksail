package talosprovisioner

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"
)

// ensureHetznerInfra creates the network, firewall, placement group, and retrieves
// the SSH key needed for Hetzner cluster provisioning.
func (p *Provisioner) ensureHetznerInfra(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
) (HetznerInfra, error) {
	_, _ = fmt.Fprintf(p.logWriter, "Creating infrastructure resources...\n")

	network, err := hzProvider.EnsureNetwork(ctx, clusterName, p.hetznerOpts.NetworkCIDR)
	if err != nil {
		return HetznerInfra{}, fmt.Errorf("failed to create network: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Network %s created\n", network.Name)

	firewall, err := hzProvider.EnsureFirewall(ctx, clusterName, p.hetznerOpts.AllowedCIDRs)
	if err != nil {
		return HetznerInfra{}, fmt.Errorf("failed to create firewall: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Firewall %s created\n", firewall.Name)

	placementGroup, err := hzProvider.EnsurePlacementGroup(
		ctx,
		clusterName,
		p.hetznerOpts.PlacementGroupStrategy.String(),
		p.hetznerOpts.PlacementGroup,
	)
	if err != nil {
		return HetznerInfra{}, fmt.Errorf("failed to create placement group: %w", err)
	}

	var placementGroupID int64

	if placementGroup != nil {
		placementGroupID = placementGroup.ID
		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Placement group %s created\n", placementGroup.Name)
	} else {
		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Placement group disabled (strategy: None)\n")
	}

	// Get SSH key if configured
	var sshKeyID int64

	if p.hetznerOpts.SSHKeyName != "" {
		sshKey, keyErr := hzProvider.GetSSHKey(ctx, p.hetznerOpts.SSHKeyName)
		if keyErr != nil {
			return HetznerInfra{}, fmt.Errorf("failed to get SSH key: %w", keyErr)
		}

		if sshKey != nil {
			sshKeyID = sshKey.ID
		}
	}

	return HetznerInfra{
		NetworkID:        network.ID,
		FirewallID:       firewall.ID,
		PlacementGroupID: placementGroupID,
		SSHKeyID:         sshKeyID,
	}, nil
}

// createHetznerNodeGroups creates both control plane and worker node groups.
// When imageID > 0 (snapshot-based), servers boot directly from the snapshot image.
// When imageID == 0 (ISO-based, legacy), servers use the public Talos ISO.
func (p *Provisioner) createHetznerNodeGroups(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	infra HetznerInfra,
	clusterName string,
	imageID int64,
) ([]*hcloud.Server, []*hcloud.Server, error) {
	isoID := p.talosOpts.ISO
	if imageID > 0 {
		isoID = 0 // snapshot takes precedence; ISO is not used
	}

	controlPlaneServers, err := p.createHetznerNodes(ctx, hzProvider, infra, HetznerNodeGroupOpts{
		ClusterName: clusterName,
		Role:        RoleControlPlane,
		Count:       p.options.ControlPlaneNodes,
		ServerType:  p.hetznerOpts.ControlPlaneServerType,
		ISOID:       isoID,
		ImageID:     imageID,
		Location:    p.hetznerOpts.Location,
	})
	if err != nil {
		return nil, nil, err
	}

	workerServers, err := p.createHetznerNodes(ctx, hzProvider, infra, HetznerNodeGroupOpts{
		ClusterName: clusterName,
		Role:        RoleWorker,
		Count:       p.options.WorkerNodes,
		ServerType:  p.hetznerOpts.WorkerServerType,
		ISOID:       isoID,
		ImageID:     imageID,
		Location:    p.hetznerOpts.Location,
	})
	if err != nil {
		return nil, nil, err
	}

	return controlPlaneServers, workerServers, nil
}

// updateConfigsWithEndpoint regenerates Talos configs with the correct endpoint IP.
func (p *Provisioner) updateConfigsWithEndpoint(
	controlPlaneServers []*hcloud.Server,
) error {
	if len(controlPlaneServers) == 0 {
		return clustererr.ErrNoControlPlaneNodes
	}

	// Regenerate configs with the first control-plane node's reachable IP as the
	// endpoint. This is necessary because:
	// 1. The original configs were generated with internal network IPs
	// 2. KSail reaches Hetzner nodes via their public IPv4, or their private-network
	//    IP when the node is IPv4-less
	// 3. The endpoint IP is embedded in certificates and must match
	firstCPIP, addrErr := hetznerNodeTalosAddress(controlPlaneServers[0])
	if addrErr != nil {
		return addrErr
	}

	_, _ = fmt.Fprintf(p.logWriter, "Regenerating configs with endpoint IP %s...\n", firstCPIP)

	updatedConfigs, err := p.talosConfigs.WithEndpoint(firstCPIP)
	if err != nil {
		return fmt.Errorf("failed to regenerate configs with endpoint: %w", err)
	}

	// Update the stored configs
	p.talosConfigs = updatedConfigs

	return nil
}

// prepareAndApplyConfigs prepares config bundle and applies configuration to all nodes.
func (p *Provisioner) prepareAndApplyConfigs(
	ctx context.Context,
	clusterName string,
	controlPlaneServers, workerServers, allServers []*hcloud.Server,
) error {
	configBundle := p.talosConfigs.Bundle()

	// Wait for Talos API to be reachable on all nodes (maintenance mode)
	_, _ = fmt.Fprintf(p.logWriter, "Waiting for Talos API on %d nodes...\n", len(allServers))

	err := p.waitForHetznerTalosAPI(ctx, allServers)
	if err != nil {
		return fmt.Errorf("failed waiting for Talos API: %w", err)
	}

	// Apply machine configuration to all nodes
	_, _ = fmt.Fprintf(p.logWriter, "Applying machine configuration to nodes...\n")

	return p.applyHetznerConfigs(
		ctx,
		clusterName,
		controlPlaneServers,
		workerServers,
		configBundle,
	)
}

// bootstrapAndFinalize bootstraps etcd, saves configs, and waits for cluster readiness.
// When the node autoscaler is enabled and a snapshot image was used, it also creates
// the cluster-autoscaler-config Secret in kube-system so the Cluster Autoscaler chart
// can provision new worker nodes via the Hetzner Cloud API.
func (p *Provisioner) bootstrapAndFinalize(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
	controlPlaneServers, workerServers, allServers []*hcloud.Server,
	snapshotImageID int64,
) error {
	err := p.detachOrWaitForReboot(ctx, hzProvider, allServers)
	if err != nil {
		return err
	}

	// Bootstrap etcd cluster
	_, _ = fmt.Fprintf(p.logWriter, "Bootstrapping etcd cluster...\n")

	configBundle := p.talosConfigs.Bundle()

	err = p.bootstrapHetznerCluster(ctx, controlPlaneServers[0], configBundle)
	if err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	// Save talosconfig
	if p.options.TalosconfigPath != "" {
		err = p.saveTalosconfig(configBundle)
		if err != nil {
			return fmt.Errorf("failed to save talosconfig: %w", err)
		}
	}

	// Save kubeconfig and wait for readiness
	if p.options.KubeconfigPath != "" {
		_, _ = fmt.Fprintf(p.logWriter, "Fetching and saving kubeconfig...\n")

		err = p.saveHetznerKubeconfig(ctx, controlPlaneServers[0], configBundle)
		if err != nil {
			return fmt.Errorf("failed to save kubeconfig: %w", err)
		}

		// Wait for cluster to be fully ready before reporting success
		_, _ = fmt.Fprintf(p.logWriter, "Waiting for cluster to be ready...\n")

		err = p.waitForHetznerClusterReady(
			ctx,
			clusterName,
			controlPlaneServers,
			workerServers,
			configBundle,
		)
		if err != nil {
			return fmt.Errorf("cluster readiness check failed: %w", err)
		}

		_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster is ready\n")
	}

	// Create the cluster-autoscaler-config Secret when applicable. No restart on
	// create: the autoscaler Deployment is installed afterwards and starts with
	// this config.
	err = p.ensureAutoscalerSecret(ctx, configBundle, snapshotImageID, false)
	if err != nil {
		return err
	}

	return nil
}

const (
	hcloudSecretName      = "hcloud"
	hcloudSecretNamespace = "kube-system"
)

// newSecretKubeclient builds a Kubernetes clientset from the provisioner's
// configured kubeconfig for managing Hetzner-related secrets. purpose is woven
// into the error messages to identify the calling context.
func (p *Provisioner) newSecretKubeclient(purpose string) (kubernetes.Interface, error) {
	if p.options.KubeconfigPath == "" {
		return nil, fmt.Errorf(
			"creating kubeclient for %s: %w",
			purpose,
			k8s.ErrKubeconfigPathEmpty,
		)
	}

	expandedPath, err := fsutil.ExpandHomePath(p.options.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("expanding kubeconfig path for %s: %w", purpose, err)
	}

	kubeconfigPath, err := fsutil.EvalCanonicalPath(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing kubeconfig path for %s: %w", purpose, err)
	}

	kubeclient, err := k8s.NewClientset(kubeconfigPath, p.options.KubeconfigContext)
	if err != nil {
		return nil, fmt.Errorf("creating kubeclient for %s: %w", purpose, err)
	}

	return kubeclient, nil
}

// ensureHcloudSecret creates or updates the "hcloud" Secret in kube-system with
// the API token and network name. The cluster autoscaler Helm chart reads both
// keys from this secret. Normally created by hcloud-ccm during cluster create,
// but must be ensured during update when enabling the autoscaler for the first
// time (the secret may be missing or lack the "network" key).
func (p *Provisioner) ensureHcloudSecret(ctx context.Context, clusterName string) error {
	tokenEnvVar := p.hetznerOpts.TokenEnvVar
	if tokenEnvVar == "" {
		tokenEnvVar = v1alpha1.DefaultHetznerTokenEnvVar
	}

	token := os.Getenv(tokenEnvVar)
	if token == "" {
		return fmt.Errorf("%w: %s", ErrHcloudTokenNotSet, tokenEnvVar)
	}

	networkName := p.hetznerOpts.NetworkName
	if networkName == "" {
		networkName = clusterName + hetzner.NetworkSuffix
	}

	kubeclient, err := p.newSecretKubeclient("hcloud secret")
	if err != nil {
		return err
	}

	desiredData := map[string][]byte{
		"token":   []byte(token),
		"network": []byte(networkName),
	}

	secretsClient := kubeclient.CoreV1().Secrets(hcloudSecretNamespace)

	needsUpdate, err := p.getOrCreateHcloudSecret(ctx, secretsClient, desiredData)
	if err != nil {
		return err
	}

	if !needsUpdate {
		return nil
	}

	return p.updateHcloudSecret(ctx, secretsClient, desiredData)
}

// getOrCreateHcloudSecret attempts to get the hcloud secret, creating it if it
// doesn't exist. Returns true if an update is still needed (secret exists but
// has stale data), or false when no further action is required.
func (p *Provisioner) getOrCreateHcloudSecret(
	ctx context.Context,
	secretsClient corev1client.SecretInterface,
	desiredData map[string][]byte,
) (bool, error) {
	existing, err := secretsClient.Get(ctx, hcloudSecretName, metav1.GetOptions{})
	if err == nil {
		return k8s.MergeSecretData(existing, desiredData), nil
	}

	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("get hcloud secret: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hcloudSecretName,
			Namespace: hcloudSecretNamespace,
		},
		Data: desiredData,
	}

	_, createErr := secretsClient.Create(ctx, secret, metav1.CreateOptions{})
	if createErr == nil {
		return false, nil
	}

	if !apierrors.IsAlreadyExists(createErr) {
		return false, fmt.Errorf("create hcloud secret: %w", createErr)
	}

	// Race: another caller created it between Get and Create — fetch for update.
	existing, err = secretsClient.Get(ctx, hcloudSecretName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get hcloud secret after conflict: %w", err)
	}

	return k8s.MergeSecretData(existing, desiredData), nil
}

// updateHcloudSecret performs a conflict-retrying update of the hcloud secret.
func (p *Provisioner) updateHcloudSecret(
	ctx context.Context,
	secretsClient corev1client.SecretInterface,
	desiredData map[string][]byte,
) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, getErr := secretsClient.Get(ctx, hcloudSecretName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("get hcloud secret for update: %w", getErr)
		}

		if !k8s.MergeSecretData(latest, desiredData) {
			return nil
		}

		_, updateErr := secretsClient.Update(ctx, latest, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("updating hcloud secret: %w", updateErr)
		}

		return nil
	})
	if retryErr != nil {
		return fmt.Errorf("update hcloud secret: %w", retryErr)
	}

	return nil
}

// ensureAutoscalerSecret creates the cluster-autoscaler-config Secret when the
// node autoscaler is enabled and bootstrap used a snapshot image.
//
// When restartIfChanged is true (cluster update) and the Secret's data actually
// changed, it rolls the running cluster-autoscaler Deployment so it reloads the
// new config — the autoscaler reads the Secret as environment variables, which
// Kubernetes does not live-reload, so without the restart new nodes would keep
// booting from the stale Kubernetes version / snapshot. On cluster create
// restartIfChanged is false: the Deployment does not exist yet (the installer
// runs afterwards and starts with the correct config).
func (p *Provisioner) ensureAutoscalerSecret(
	ctx context.Context,
	configBundle *bundle.Bundle,
	snapshotImageID int64,
	restartIfChanged bool,
) error {
	if p.hetznerOpts == nil || !p.hetznerOpts.NodeAutoscalerEnabled || snapshotImageID <= 0 {
		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "Applying cluster-autoscaler config secret...\n")

	kubeclient, err := p.newSecretKubeclient("autoscaler secret")
	if err != nil {
		return err
	}

	workerConfigYAML, err := GenerateAutoscalerWorkerConfig(configBundle.Worker())
	if err != nil {
		return fmt.Errorf("generating autoscaler worker config: %w", err)
	}

	changed, err := ApplyAutoscalerConfigSecret(
		ctx,
		kubeclient,
		strconv.FormatInt(snapshotImageID, 10),
		workerConfigYAML,
	)
	if err != nil {
		return fmt.Errorf("applying autoscaler config secret: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Cluster autoscaler config secret created\n")

	if restartIfChanged && changed {
		return p.restartAutoscalerAfterConfigChange(ctx, kubeclient)
	}

	return nil
}

// restartAutoscalerAfterConfigChange rolls the cluster-autoscaler Deployment so it
// reloads the cluster-autoscaler-config Secret. A missing Deployment is not an
// error — the autoscaler may not be installed yet, in which case its next install
// starts with the current config.
func (p *Provisioner) restartAutoscalerAfterConfigChange(
	ctx context.Context,
	kubeclient kubernetes.Interface,
) error {
	restarted, err := k8s.RolloutRestartDeploymentsByLabel(
		ctx,
		kubeclient,
		autoscalerConfigSecretNamespace,
		autoscalerDeploymentSelector,
	)
	if err != nil {
		return fmt.Errorf("restarting cluster-autoscaler after config change: %w", err)
	}

	if restarted == 0 {
		_, _ = fmt.Fprintf(
			p.logWriter,
			"  ⓘ cluster-autoscaler not running; it will use the updated config when installed\n",
		)

		return nil
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Restarted cluster-autoscaler to load the updated config\n")

	return nil
}

// detachOrWaitForReboot handles the post-config boot sequence.
// For ISO-based boot: detaches ISOs from all servers and waits for auto-reboot.
// For snapshot-based boot: no ISO is attached, so only wait for servers to be reachable.
func (p *Provisioner) detachOrWaitForReboot(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	allServers []*hcloud.Server,
) error {
	schematicID := ""
	if p.talosOpts != nil {
		schematicID = strings.TrimSpace(p.talosOpts.SchematicID)
	}

	// Fall back to extensions-derived schematic
	if schematicID == "" && p.talosConfigs != nil {
		schematicID = p.talosConfigs.SchematicID()
	}

	if schematicID != "" {
		_, _ = fmt.Fprintf(p.logWriter, "Waiting for nodes to be reachable after reboot...\n")

		return p.waitForServersToBeReachable(ctx, allServers)
	}

	_, _ = fmt.Fprintf(p.logWriter, "Detaching ISOs and rebooting nodes...\n")

	return p.detachISOsAndReboot(ctx, hzProvider, allServers)
}

// createHetznerCluster creates a Talos cluster on Hetzner Cloud infrastructure.
func (p *Provisioner) createHetznerCluster(ctx context.Context, clusterName string) error {
	hzProvider, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "Creating Talos cluster %q on Hetzner Cloud...\n", clusterName)

	// Check if cluster already exists
	exists, err := hzProvider.NodesExist(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}

	if exists {
		return fmt.Errorf("%w: %s", ErrClusterAlreadyExists, clusterName)
	}

	snapshotImageID, err := p.ensureSnapshotImage(ctx, clusterName)
	if err != nil {
		return err
	}

	// Verify server type availability before creating infrastructure resources
	err = p.checkHetznerAvailability(ctx, hzProvider)
	if err != nil {
		return err
	}

	// Create infrastructure resources
	infra, err := p.ensureHetznerInfra(ctx, hzProvider, clusterName)
	if err != nil {
		return err
	}

	// Create node groups
	controlPlaneServers, workerServers, err := p.createHetznerNodeGroups(
		ctx, hzProvider, infra, clusterName, snapshotImageID,
	)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(p.logWriter, "\nInfrastructure created. Bootstrapping Talos cluster...\n")

	err = p.applyConfigsAndBootstrap(
		ctx, hzProvider, clusterName, controlPlaneServers, workerServers, snapshotImageID,
	)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"\nSuccessfully created Talos cluster %q on Hetzner Cloud\n",
		clusterName,
	)

	return nil
}

// ensureSnapshotImage ensures a Talos snapshot image exists when a schematic ID is configured.
// It returns the snapshot image ID (0 when snapshot boot is not configured).
// The schematic ID can come from either:
//   - Explicit spec.cluster.talos.schematicId (takes precedence)
//   - Auto-computed from spec.cluster.talos.extensions via the Configs
func (p *Provisioner) ensureSnapshotImage(ctx context.Context, clusterName string) (int64, error) {
	if p.snapshotManager == nil || p.talosOpts == nil {
		return 0, nil
	}

	schematicID := strings.TrimSpace(p.talosOpts.SchematicID)
	version := strings.TrimSpace(p.talosOpts.Version)

	// Fall back to auto-computed schematic from extensions
	if schematicID == "" && p.talosConfigs != nil {
		schematicID = p.talosConfigs.SchematicID()
	}

	if schematicID == "" {
		return 0, nil
	}

	if version == "" {
		return 0, ErrSchematicRequiresVersion
	}

	if strings.HasPrefix(strings.ToLower(p.hetznerOpts.ControlPlaneServerType), "cax") ||
		strings.HasPrefix(strings.ToLower(p.hetznerOpts.WorkerServerType), "cax") {
		return 0, ErrARM64SnapshotNotSupported
	}

	_, _ = fmt.Fprintf(p.logWriter, "Ensuring Talos snapshot image...\n")

	snapshotImageID, err := p.snapshotManager.EnsureTalosSnapshot(
		ctx,
		clusterName,
		version,
		schematicID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure Talos snapshot: %w", err)
	}

	return snapshotImageID, nil
}

// checkHetznerAvailability verifies that the configured server types are available
// in the primary location or any fallback locations. This check runs before
// infrastructure resource creation to avoid creating networks, firewalls, and
// placement groups that would need cleanup if servers cannot be provisioned.
func (p *Provisioner) checkHetznerAvailability(
	ctx context.Context,
	hzProvider *hetzner.Provider,
) error {
	if p.hetznerOpts == nil {
		return nil
	}

	serverTypes := uniqueServerTypes(
		p.hetznerOpts.ControlPlaneServerType,
		p.hetznerOpts.WorkerServerType,
	)

	_, _ = fmt.Fprintf(p.logWriter, "Checking server type availability...\n")

	err := hzProvider.CheckServerAvailabilityWithRetry(
		ctx,
		serverTypes,
		p.hetznerOpts.Location,
		p.hetznerOpts.FallbackLocations,
		hetzner.DefaultMaxAvailabilityCheckRetries,
		p.logWriter,
	)
	if err != nil {
		return fmt.Errorf("server availability check failed: %w", err)
	}

	_, _ = fmt.Fprintf(p.logWriter, "  ✓ Server types available\n")

	return nil
}

// uniqueServerTypes returns a deduplicated list of non-empty server type names.
func uniqueServerTypes(types ...string) []string {
	seen := make(map[string]struct{}, len(types))
	result := make([]string, 0, len(types))

	for _, serverType := range types {
		if serverType == "" {
			continue
		}

		if _, ok := seen[serverType]; ok {
			continue
		}

		seen[serverType] = struct{}{}
		result = append(result, serverType)
	}

	return result
}

// applyConfigsAndBootstrap updates machine configs with the correct endpoint,
// applies them to all nodes, and bootstraps the Talos cluster.
func (p *Provisioner) applyConfigsAndBootstrap(
	ctx context.Context,
	hzProvider *hetzner.Provider,
	clusterName string,
	controlPlaneServers, workerServers []*hcloud.Server,
	snapshotImageID int64,
) error {
	err := p.updateConfigsWithEndpoint(controlPlaneServers)
	if err != nil {
		return err
	}

	allServers := slices.Concat(controlPlaneServers, workerServers)

	err = p.prepareAndApplyConfigs(ctx, clusterName, controlPlaneServers, workerServers, allServers)
	if err != nil {
		return err
	}

	return p.bootstrapAndFinalize(
		ctx, hzProvider, clusterName,
		controlPlaneServers, workerServers, allServers,
		snapshotImageID,
	)
}

func (p *Provisioner) deleteHetznerCluster(ctx context.Context, clusterName string) error {
	hetznerProv, err := p.hetznerProvider()
	if err != nil {
		return err
	}

	// Check cluster existence via the KSail-managed network rather than
	// KSail-owned nodes. The network persists even when all KSail nodes are
	// gone but autoscaler-created nodes remain, so this guard holds in the
	// mixed-state scenario and still prevents accidental deletion when the
	// cluster name is wrong.
	networkExists, err := hetznerProv.NetworkExists(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to check if cluster network exists: %w", err)
	}

	if !networkExists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, clusterName)
	}

	// Delete autoscaler-managed nodes before KSail-managed infrastructure.
	if len(p.hetznerOpts.AutoscalerNodePoolNames) > 0 {
		deleteErr := hetznerProv.DeleteAutoscalerNodes(
			ctx, clusterName, p.hetznerOpts.AutoscalerNodePoolNames,
		)
		if deleteErr != nil {
			return fmt.Errorf("failed to delete autoscaler nodes: %w", deleteErr)
		}
	}

	// Delete all nodes and infrastructure
	_, _ = fmt.Fprintf(p.logWriter, "Deleting Talos cluster %q on Hetzner...\n", clusterName)

	err = hetznerProv.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete Hetzner nodes: %w", err)
	}

	// Delete Talos snapshot images when --delete-storage is set
	if p.deleteStorage && p.snapshotManager != nil {
		snapshotErr := p.snapshotManager.DeleteTalosSnapshots(ctx, clusterName)
		if snapshotErr != nil {
			return fmt.Errorf("failed to delete Talos snapshots: %w", snapshotErr)
		}
	}

	// Clean up config files (kubeconfig and talosconfig)
	p.cleanupConfigFiles(clusterName)

	_, _ = fmt.Fprintf(p.logWriter, "Successfully deleted Talos cluster %q\n", clusterName)

	return nil
}
