package omni

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
)

var (
	// ErrTalosVersionRequired is returned when TalosVersion is not provided.
	ErrTalosVersionRequired = errors.New("TalosVersion is required")
	// ErrKubernetesVersionRequired is returned when KubernetesVersion is not provided.
	ErrKubernetesVersionRequired = errors.New("KubernetesVersion is required")
	// ErrMachineAllocationRequired is returned when neither MachineClass nor Machines is set.
	ErrMachineAllocationRequired = errors.New(
		"either MachineClass or Machines must be set (Omni requires one for node allocation)",
	)
	// ErrMachineAllocationConflict is returned when both MachineClass and Machines are set.
	ErrMachineAllocationConflict = errors.New(
		"MachineClass and Machines are mutually exclusive (set one or the other)",
	)
	// ErrInsufficientMachines is returned when the Machines list is too short for the requested node counts.
	ErrInsufficientMachines = errors.New(
		"not enough machines for the requested control plane and worker node counts",
	)
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
	ControlPlanes int
	// Workers is the number of worker nodes.
	Workers int
	// MachineClass is the Omni machine class name for dynamic allocation.
	// Mutually exclusive with Machines.
	MachineClass string
	// Machines is a list of machine UUIDs for static allocation.
	// Mutually exclusive with MachineClass.
	Machines []string
	// Patches are the loaded Talos config patches from the distribution config directory.
	Patches []PatchInfo
}

// BuildClusterTemplate builds a multi-document YAML cluster template
// compatible with the Omni SDK's template format.
// The template contains Cluster, ControlPlane, and Workers documents.
//
// Machine allocation follows upstream Omni semantics:
//   - MachineClass: dynamic allocation from a named class (size from ControlPlanes/Workers)
//   - Machines: static allocation by UUID (first N = control planes, rest = workers)
func BuildClusterTemplate(params TemplateParams) (io.Reader, error) {
	if params.TalosVersion == "" {
		return nil, ErrTalosVersionRequired
	}

	if params.KubernetesVersion == "" {
		return nil, ErrKubernetesVersionRequired
	}

	if params.MachineClass != "" && len(params.Machines) > 0 {
		return nil, ErrMachineAllocationConflict
	}

	if params.MachineClass == "" && len(params.Machines) == 0 {
		return nil, ErrMachineAllocationRequired
	}

	// Validate that static machine list has enough entries for the requested topology
	if len(params.Machines) > 0 {
		required := params.ControlPlanes + params.Workers
		if len(params.Machines) < required {
			return nil, fmt.Errorf(
				"%w: need %d (controlPlanes=%d + workers=%d), got %d",
				ErrInsufficientMachines,
				required, params.ControlPlanes, params.Workers, len(params.Machines),
			)
		}
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

	if params.MachineClass != "" {
		// Dynamic allocation: machineClass with name and size
		fmt.Fprintf(&buf, "machineClass:\n")
		fmt.Fprintf(&buf, "  name: %s\n", params.MachineClass)
		fmt.Fprintf(&buf, "  size: %d\n", params.ControlPlanes)
	} else {
		// Static allocation: list of machine UUIDs
		cpMachines := params.Machines
		if params.ControlPlanes < len(cpMachines) {
			cpMachines = cpMachines[:params.ControlPlanes]
		}

		writeMachinesList(&buf, cpMachines)
	}

	// Add control-plane-scoped patches
	cpPatches := filterPatchesByScope(params.Patches, PatchScopeControlPlane)
	if len(cpPatches) > 0 {
		writePatchesSection(&buf, cpPatches)
	}

	// Write Workers document (only if workers > 0)
	if params.Workers > 0 {
		fmt.Fprintf(&buf, "---\nkind: Workers\n")

		if params.MachineClass != "" {
			fmt.Fprintf(&buf, "machineClass:\n")
			fmt.Fprintf(&buf, "  name: %s\n", params.MachineClass)
			fmt.Fprintf(&buf, "  size: %d\n", params.Workers)
		} else {
			// Worker machines are the remaining after control planes
			workerMachines := params.Machines
			if params.ControlPlanes < len(workerMachines) {
				workerMachines = workerMachines[params.ControlPlanes:]
			} else {
				workerMachines = nil
			}

			writeMachinesList(&buf, workerMachines)
		}

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

// writeMachinesList writes a machines: list to the YAML buffer.
func writeMachinesList(buf *bytes.Buffer, machines []string) {
	fmt.Fprintf(buf, "machines:\n")

	for _, m := range machines {
		fmt.Fprintf(buf, "  - %s\n", m)
	}
}

// writeInlineContent writes patch YAML content indented under inline:.
// Blank lines are skipped: they are cosmetic in YAML block mappings, and
// emitting them without indentation can confuse strict YAML parsers that
// treat an unindented line as terminating the current block.
func writeInlineContent(buf *bytes.Buffer, content []byte) {
	lines := strings.SplitSeq(strings.TrimRight(string(content), "\n"), "\n")
	for line := range lines {
		if line == "" {
			continue
		}

		fmt.Fprintf(buf, "      %s\n", line)
	}
}

// patchName derives a patch name from the file path.
func patchName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)

	return strings.TrimSuffix(base, ext)
}
