package omni

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
)

// PatchScope indicates which nodes a patch should be applied to.
// These values mirror talosconfigmanager.PatchScope.
const (
	PatchScopeCluster      = talosconfigmanager.PatchScopeCluster
	PatchScopeControlPlane = talosconfigmanager.PatchScopeControlPlane
	PatchScopeWorker       = talosconfigmanager.PatchScopeWorker
)

// PatchInfo holds patch data for building an Omni cluster template.
type PatchInfo struct {
	// Path is the source file path or identifier for the patch.
	Path string
	// Scope indicates which nodes this patch applies to.
	Scope talosconfigmanager.PatchScope
	// Content is the raw YAML patch content.
	Content []byte
}

// TemplateParams holds the parameters for building an Omni cluster template.
type TemplateParams struct {
	// ClusterName is the name of the cluster.
	ClusterName string
	// TalosVersion is the Talos version (e.g., "1.11.2").
	TalosVersion string
	// KubernetesVersion is the Kubernetes version (e.g., "1.32.0").
	KubernetesVersion string
	// ControlPlanes is the number of control-plane nodes.
	ControlPlanes int32
	// Workers is the number of worker nodes.
	Workers int32
	// Patches are the loaded Talos config patches from the distribution config directory.
	Patches []PatchInfo
}

// ErrTalosVersionRequired is returned when the Talos version is not specified.
var ErrTalosVersionRequired = fmt.Errorf("omni talosVersion is required for cluster creation")

// ErrKubernetesVersionRequired is returned when the Kubernetes version is not specified.
var ErrKubernetesVersionRequired = fmt.Errorf("omni kubernetesVersion is required for cluster creation")

// BuildClusterTemplate builds a multi-document YAML cluster template
// compatible with the Omni SDK's template format.
// The template contains Cluster, ControlPlane, and Workers documents.
func BuildClusterTemplate(params TemplateParams) (io.Reader, error) {
	if params.TalosVersion == "" {
		return nil, ErrTalosVersionRequired
	}

	if params.KubernetesVersion == "" {
		return nil, ErrKubernetesVersionRequired
	}

	var buf bytes.Buffer

	talosVersion := ensureVPrefix(params.TalosVersion)
	k8sVersion := ensureVPrefix(params.KubernetesVersion)

	// Write Cluster document
	fmt.Fprintf(&buf, "kind: Cluster\n")
	fmt.Fprintf(&buf, "name: %s\n", params.ClusterName)
	fmt.Fprintf(&buf, "kubernetes:\n")
	fmt.Fprintf(&buf, "  version: %s\n", k8sVersion)
	fmt.Fprintf(&buf, "talos:\n")
	fmt.Fprintf(&buf, "  version: %s\n", talosVersion)

	// Add cluster-scoped patches
	clusterPatches := filterPatchesByScope(params.Patches, PatchScopeCluster)
	if len(clusterPatches) > 0 {
		writePatchesSection(&buf, clusterPatches)
	}

	// Write ControlPlane document
	fmt.Fprintf(&buf, "---\nkind: ControlPlane\n")

	// Add machine class for control planes
	fmt.Fprintf(&buf, "machineClass:\n")
	fmt.Fprintf(&buf, "  name: any\n")
	fmt.Fprintf(&buf, "  size: %d\n", params.ControlPlanes)

	// Add control-plane-scoped patches
	cpPatches := filterPatchesByScope(params.Patches, PatchScopeControlPlane)
	if len(cpPatches) > 0 {
		writePatchesSection(&buf, cpPatches)
	}

	// Write Workers document (only if workers > 0)
	if params.Workers > 0 {
		fmt.Fprintf(&buf, "---\nkind: Workers\n")

		fmt.Fprintf(&buf, "machineClass:\n")
		fmt.Fprintf(&buf, "  name: any\n")
		fmt.Fprintf(&buf, "  size: %d\n", params.Workers)

		// Add worker-scoped patches
		workerPatches := filterPatchesByScope(params.Patches, PatchScopeWorker)
		if len(workerPatches) > 0 {
			writePatchesSection(&buf, workerPatches)
		}
	}

	return &buf, nil
}

// ensureVPrefix ensures the version string starts with "v".
func ensureVPrefix(version string) string {
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}

	return version
}

// filterPatchesByScope returns patches matching the given scope.
func filterPatchesByScope(
	patches []PatchInfo,
	scope talosconfigmanager.PatchScope,
) []PatchInfo {
	var filtered []PatchInfo

	for _, p := range patches {
		if p.Scope == scope {
			filtered = append(filtered, p)
		}
	}

	return filtered
}

// writePatchesSection writes the patches section to the YAML buffer.
// Patches are written using inline format with their content.
func writePatchesSection(buf *bytes.Buffer, patches []PatchInfo) {
	fmt.Fprintf(buf, "patches:\n")

	for _, patch := range patches {
		name := patchName(patch.Path)
		fmt.Fprintf(buf, "  - name: %s\n", name)
		fmt.Fprintf(buf, "    inline:\n")

		writeInlineContent(buf, patch.Content)
	}
}

// writeInlineContent writes patch YAML content indented under inline:.
func writeInlineContent(buf *bytes.Buffer, content []byte) {
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			fmt.Fprintf(buf, "      \n")
		} else {
			fmt.Fprintf(buf, "      %s\n", line)
		}
	}
}

// patchName derives a patch name from the file path.
func patchName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)

	return strings.TrimSuffix(base, ext)
}

// loadPatchesFromDistributionConfig loads Talos config patches from a distribution config directory.
// The directory should contain cluster/, control-planes/, and workers/ subdirectories.
func loadPatchesFromDistributionConfig(distributionConfigPath string) ([]talosconfigmanager.Patch, error) {
	if distributionConfigPath == "" {
		return nil, nil
	}

	// Check if the directory exists
	info, err := os.Stat(distributionConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to access distribution config path: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("distribution config path is not a directory: %s", distributionConfigPath)
	}

	return talosconfigmanager.LoadPatches(distributionConfigPath)
}
