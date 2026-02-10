package scaffolder

import (
	"fmt"
	"path/filepath"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
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

	config := &talosgenerator.TalosConfig{
		PatchesDir:                TalosConfigDir,
		MirrorRegistries:          s.MirrorRegistries,
		WorkerNodes:               workers,
		DisableDefaultCNI:         disableDefaultCNI,
		EnableKubeletCertRotation: enableKubeletCertRotation,
		ClusterName:               s.ClusterName,
	}

	opts := yamlgenerator.Options{
		Output: output,
		Force:  force,
	}

	_, err := s.TalosGenerator.Generate(config, opts)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTalosConfigGeneration, err)
	}

	s.notifyTalosGenerated(workers, disableDefaultCNI, enableKubeletCertRotation)

	return nil
}

// notifyTalosGenerated sends notifications about generated Talos files.
func (s *Scaffolder) notifyTalosGenerated(
	workers int,
	disableDefaultCNI, enableKubeletCertRotation bool,
) {
	// Determine which directories have patches (no .gitkeep generated there)
	clusterHasPatches := workers == 0 || len(s.MirrorRegistries) > 0 || disableDefaultCNI ||
		enableKubeletCertRotation || s.ClusterName != ""

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
		filename  string
	}{
		{workers == 0, "allow-scheduling-on-control-planes.yaml"},
		{len(s.MirrorRegistries) > 0, "mirror-registries.yaml"},
		{disableDefaultCNI, "disable-default-cni.yaml"},
		{enableKubeletCertRotation, "kubelet-cert-rotation.yaml"},
		{enableKubeletCertRotation, "kubelet-csr-approver.yaml"},
		{s.ClusterName != "", "cluster-name.yaml"},
	}

	for _, patch := range patches {
		if patch.condition {
			s.notifyTalosPatchCreated("cluster", patch.filename)
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
