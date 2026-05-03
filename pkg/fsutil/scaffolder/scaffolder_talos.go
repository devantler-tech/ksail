package scaffolder

import (
	"fmt"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
)

// generateTalosConfig generates the Talos patches directory structure.
func (s *Scaffolder) generateTalosConfig(output string, force bool) error {
	config, clusterHasPatches := s.buildTalosGeneratorConfig()

	opts := yamlgenerator.Options{
		Output: output,
		Force:  force,
	}

	_, err := s.TalosGenerator.Generate(config, opts)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTalosConfigGeneration, err)
	}

	s.notifyTalosGenerated(config, clusterHasPatches)

	return nil
}

// buildTalosGeneratorConfig derives the Talos generator configuration from KSailConfig.
func (s *Scaffolder) buildTalosGeneratorConfig() (*talosgenerator.Config, bool) {
	workers := int(s.KSailConfig.Spec.Cluster.Workers)

	// Disable default CNI (Flannel) if using any non-default CNI (e.g., Cilium, Calico, None)
	// Empty string is treated as default CNI (for imperative mode without config file)
	disableDefaultCNI := s.KSailConfig.Spec.Cluster.CNI != v1alpha1.CNIDefault &&
		s.KSailConfig.Spec.Cluster.CNI != ""

	// Enable kubelet certificate rotation when metrics-server is explicitly enabled.
	enableKubeletCertRotation := s.KSailConfig.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled

	// Enable image verification scaffolding when explicitly enabled for Talos.
	enableImageVerification := s.KSailConfig.Spec.Cluster.Talos.ImageVerification == v1alpha1.ImageVerificationEnabled

	// Disable CDI when explicitly disabled. Talos 1.13+ enables CDI by default.
	disableCDI := s.KSailConfig.Spec.Cluster.CDI == v1alpha1.CDIDisabled

	// Enable external cloud provider when using a cloud provider (e.g., Hetzner).
	enableExternalCloudProvider := s.KSailConfig.Spec.Cluster.Provider == v1alpha1.ProviderHetzner

	// Compute Hetzner ingress firewall settings.
	enableIngressFirewall, networkCIDR, cniPort := s.hetznerIngressFirewallConfig()

	// Enable OIDC API server configuration when OIDC is configured.
	enableOIDC := s.KSailConfig.Spec.Cluster.OIDC.Enabled()

	// Mirror the conditions in generator.getDirectoriesWithPatches() exactly so
	// .gitkeep notifications match the files the generator actually writes.
	clusterHasPatches := talosClusterHasPatches(
		workers, s.MirrorRegistries, disableDefaultCNI, enableKubeletCertRotation,
		s.ClusterName, enableImageVerification, disableCDI, enableExternalCloudProvider,
		enableIngressFirewall, enableOIDC,
	)

	config := &talosgenerator.Config{
		PatchesDir:                  TalosConfigDir,
		MirrorRegistries:            s.MirrorRegistries,
		WorkerNodes:                 workers,
		DisableDefaultCNI:           disableDefaultCNI,
		EnableKubeletCertRotation:   enableKubeletCertRotation,
		ClusterName:                 s.ClusterName,
		EnableImageVerification:     enableImageVerification,
		DisableCDI:                  disableCDI,
		EnableExternalCloudProvider: enableExternalCloudProvider,
		EnableIngressFirewall:       enableIngressFirewall,
		NetworkCIDR:                 networkCIDR,
		CNIPort:                     cniPort,
		EnableOIDC:                  enableOIDC,
		OIDCIssuerURL:               s.KSailConfig.Spec.Cluster.OIDC.IssuerURL,
		OIDCClientID:                s.KSailConfig.Spec.Cluster.OIDC.ClientID,
		OIDCUsernameClaim:           s.KSailConfig.Spec.Cluster.OIDC.UsernameClaim,
		OIDCUsernamePrefix:          s.KSailConfig.Spec.Cluster.OIDC.UsernamePrefix,
		OIDCGroupsClaim:             s.KSailConfig.Spec.Cluster.OIDC.GroupsClaim,
		OIDCGroupsPrefix:            s.KSailConfig.Spec.Cluster.OIDC.GroupsPrefix,
		OIDCCAFile:                  s.KSailConfig.Spec.Cluster.OIDC.CAFile,
	}

	return config, clusterHasPatches
}

// notifyTalosGenerated sends notifications about generated Talos files.
func (s *Scaffolder) notifyTalosGenerated(
	config *talosgenerator.Config,
	clusterHasPatches bool,
) {
	// Notify about .gitkeep files only for directories without patches
	subdirs := []string{"cluster", "control-planes", "workers"}
	for _, subdir := range subdirs {
		// Skip .gitkeep notification for cluster/ if it has patches
		if subdir == "cluster" && clusterHasPatches {
			continue
		}

		s.notifyTalosPatchCreated(subdir, ".gitkeep")
	}

	// Notify about conditional patches using a slice to reduce complexity
	patches := []struct {
		condition bool
		subdir    string
		filename  string
	}{
		{config.WorkerNodes == 0, "cluster", "allow-scheduling-on-control-planes.yaml"},
		{len(s.MirrorRegistries) > 0, "cluster", "mirror-registries.yaml"},
		{config.DisableDefaultCNI, "cluster", "disable-default-cni.yaml"},
		{config.EnableKubeletCertRotation, "cluster", "kubelet-cert-rotation.yaml"},
		{config.EnableKubeletCertRotation, "cluster", "kubelet-csr-approver.yaml"},
		{config.ClusterName != "", "cluster", "cluster-name.yaml"},
		{config.EnableImageVerification, "cluster", "image-verification.yaml"},
		{config.DisableCDI, "cluster", "disable-cdi.yaml"},
		{config.EnableExternalCloudProvider, "cluster", "external-cloud-provider.yaml"},
		{config.EnableIngressFirewall, "cluster", "ingress-firewall-default-action.yaml"},
		{config.EnableIngressFirewall, "control-planes", "ingress-firewall-rules.yaml"},
		{config.EnableIngressFirewall, "workers", "ingress-firewall-rules.yaml"},
		{config.EnableOIDC, "cluster", "oidc.yaml"},
	}

	for _, patch := range patches {
		if patch.condition {
			s.notifyTalosPatchCreated(patch.subdir, patch.filename)
		}
	}
}

// notifyTalosPatchCreated sends a notification about a created Talos patch file.
func (s *Scaffolder) notifyTalosPatchCreated(subdir, filename string) {
	displayPath := filepath.Join(TalosConfigDir, subdir, filename)
	notify.WriteMessage(notify.Message{
		Type:    notify.GenerateType,
		Content: "created '%s'",
		Args:    []any{displayPath},
		Writer:  s.Writer,
	})
}

// hetznerIngressFirewallConfig returns whether the Talos ingress firewall should
// be enabled for this cluster, along with the network CIDR and CNI VXLAN port.
func (s *Scaffolder) hetznerIngressFirewallConfig() (bool, string, int) {
	enabled := s.KSailConfig.Spec.Cluster.Provider == v1alpha1.ProviderHetzner &&
		s.KSailConfig.Spec.Provider.Hetzner.IngressFirewall != v1alpha1.IngressFirewallDisabled

	return enabled,
		v1alpha1.HetznerNetworkCIDR(s.KSailConfig.Spec),
		v1alpha1.HetznerCNIPort(s.KSailConfig.Spec)
}

// talosClusterHasPatches returns true when at least one patch file will be written
// into the cluster/ directory (so the generator skips creating a .gitkeep there).
func talosClusterHasPatches(
	workers int,
	mirrorRegistries []string,
	disableDefaultCNI, enableKubeletCertRotation bool,
	clusterName string,
	enableImageVerification, disableCDI, enableExternalCloudProvider, enableIngressFirewall,
	enableOIDC bool,
) bool {
	return workers == 0 ||
		len(mirrorRegistries) > 0 ||
		disableDefaultCNI ||
		enableKubeletCertRotation ||
		clusterName != "" ||
		enableImageVerification ||
		disableCDI ||
		enableExternalCloudProvider ||
		enableIngressFirewall ||
		enableOIDC
}
