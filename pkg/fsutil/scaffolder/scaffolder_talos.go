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
	// Get worker count from Talos options (default 0)
	workers := int(s.KSailConfig.Spec.Cluster.Talos.Workers)

	// Disable default CNI (Flannel) if using any non-default CNI (e.g., Cilium, Calico, None)
	// Empty string is treated as default CNI (for imperative mode without config file)
	disableDefaultCNI := s.KSailConfig.Spec.Cluster.CNI != v1alpha1.CNIDefault &&
		s.KSailConfig.Spec.Cluster.CNI != ""

	// Enable kubelet certificate rotation when metrics-server is explicitly enabled.
	// This is required for secure TLS communication between metrics-server and kubelets.
	enableKubeletCertRotation := s.KSailConfig.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled

	// Enable image verification scaffolding when explicitly enabled for Talos.
	enableImageVerification := s.KSailConfig.Spec.Cluster.Talos.ImageVerification == v1alpha1.ImageVerificationEnabled

	// Disable CDI when explicitly disabled. Talos 1.13+ enables CDI by default,
	// so we only need a patch when CDI should be turned off.
	disableCDI := s.KSailConfig.Spec.Cluster.CDI == v1alpha1.CDIDisabled

	// Enable external cloud provider when using a cloud provider (e.g., Hetzner) that
	// requires the CCM to initialize nodes with a providerID and write node labels.
	enableExternalCloudProvider := s.KSailConfig.Spec.Cluster.Provider == v1alpha1.ProviderHetzner

	// Mirror the conditions in generator.getDirectoriesWithPatches() exactly so
	// .gitkeep notifications match the files the generator actually writes.
	clusterHasPatches := workers == 0 || len(s.MirrorRegistries) > 0 || disableDefaultCNI ||
		enableKubeletCertRotation || s.ClusterName != "" || enableImageVerification || disableCDI ||
		enableExternalCloudProvider

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
	}

	opts := yamlgenerator.Options{
		Output: output,
		Force:  force,
	}

	_, err := s.TalosGenerator.Generate(config, opts)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTalosConfigGeneration, err)
	}

	s.notifyTalosGenerated(
		workers,
		disableDefaultCNI,
		enableKubeletCertRotation,
		enableImageVerification,
		disableCDI,
		enableExternalCloudProvider,
		clusterHasPatches,
	)

	return nil
}

// notifyTalosGenerated sends notifications about generated Talos files.
func (s *Scaffolder) notifyTalosGenerated(
	workers int,
	disableDefaultCNI, enableKubeletCertRotation, enableImageVerification, disableCDI,
	enableExternalCloudProvider bool,
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
		{workers == 0, "cluster", "allow-scheduling-on-control-planes.yaml"},
		{len(s.MirrorRegistries) > 0, "cluster", "mirror-registries.yaml"},
		{disableDefaultCNI, "cluster", "disable-default-cni.yaml"},
		{enableKubeletCertRotation, "cluster", "kubelet-cert-rotation.yaml"},
		{enableKubeletCertRotation, "cluster", "kubelet-csr-approver.yaml"},
		{s.ClusterName != "", "cluster", "cluster-name.yaml"},
		{enableImageVerification, "cluster", "image-verification.yaml"},
		{disableCDI, "cluster", "disable-cdi.yaml"},
		{enableExternalCloudProvider, "cluster", "external-cloud-provider.yaml"},
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
