package omni

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
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
	// ErrControlPlanesRequired is returned when ControlPlanes is less than 1.
	ErrControlPlanesRequired = errors.New(
		"at least one control plane node is required",
	)
	// ErrWorkersNegative is returned when Workers is negative.
	ErrWorkersNegative = errors.New(
		"workers count must not be negative",
	)
	// ErrClusterNameRequired is returned when ClusterName is empty or whitespace.
	ErrClusterNameRequired = errors.New(
		"cluster name is required",
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
	err := validateTemplateParams(params)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	writeClusterDocument(&buf, params)
	writeControlPlaneDocument(&buf, params)

	if params.Workers > 0 {
		writeWorkersDocument(&buf, params)
	}

	return &buf, nil
}

// validateTemplateParams checks that all required fields are set and allocation is valid.
func validateTemplateParams(params TemplateParams) error {
	if strings.TrimSpace(params.ClusterName) == "" {
		return ErrClusterNameRequired
	}

	if params.TalosVersion == "" {
		return ErrTalosVersionRequired
	}

	if params.KubernetesVersion == "" {
		return ErrKubernetesVersionRequired
	}

	if params.ControlPlanes < 1 {
		return ErrControlPlanesRequired
	}

	if params.Workers < 0 {
		return ErrWorkersNegative
	}

	return validateMachineAllocation(params)
}

// validateMachineAllocation checks that the machine allocation strategy is valid.
func validateMachineAllocation(params TemplateParams) error {
	if params.MachineClass != "" && len(params.Machines) > 0 {
		return ErrMachineAllocationConflict
	}

	if params.MachineClass == "" && len(params.Machines) == 0 {
		return ErrMachineAllocationRequired
	}

	if len(params.Machines) > 0 {
		required := params.ControlPlanes + params.Workers
		if len(params.Machines) < required {
			return fmt.Errorf(
				"%w: need %d (controlPlanes=%d + workers=%d), got %d",
				ErrInsufficientMachines,
				required, params.ControlPlanes, params.Workers, len(params.Machines),
			)
		}
	}

	return nil
}

// writeClusterDocument writes the Cluster YAML document.
func writeClusterDocument(buf *bytes.Buffer, params TemplateParams) {
	talosVersion := ensureVPrefix(params.TalosVersion)
	k8sVersion := ensureVPrefix(params.KubernetesVersion)

	fmt.Fprintf(buf, "kind: Cluster\n")
	fmt.Fprintf(buf, "name: %s\n", params.ClusterName)
	fmt.Fprintf(buf, "kubernetes:\n")
	fmt.Fprintf(buf, "  version: %s\n", k8sVersion)
	fmt.Fprintf(buf, "talos:\n")
	fmt.Fprintf(buf, "  version: %s\n", talosVersion)

	clusterPatches := filterPatchesByScope(params.Patches, PatchScopeCluster)
	if len(clusterPatches) > 0 {
		writePatchesSection(buf, clusterPatches)
	}
}

// writeControlPlaneDocument writes the ControlPlane YAML document.
func writeControlPlaneDocument(buf *bytes.Buffer, params TemplateParams) {
	fmt.Fprintf(buf, "---\nkind: ControlPlane\n")

	var cpMachines []string
	if len(params.Machines) > 0 {
		cpMachines = params.Machines[:min(params.ControlPlanes, len(params.Machines))]
	}

	writeMachineAllocation(buf, params.MachineClass, params.ControlPlanes, cpMachines)

	cpPatches := filterPatchesByScope(params.Patches, PatchScopeControlPlane)
	if len(cpPatches) > 0 {
		writePatchesSection(buf, cpPatches)
	}
}

// writeWorkersDocument writes the Workers YAML document.
func writeWorkersDocument(buf *bytes.Buffer, params TemplateParams) {
	fmt.Fprintf(buf, "---\nkind: Workers\n")

	var workerMachines []string
	if len(params.Machines) > 0 && params.ControlPlanes < len(params.Machines) {
		workerMachines = params.Machines[params.ControlPlanes : params.ControlPlanes+params.Workers]
	}

	writeMachineAllocation(buf, params.MachineClass, params.Workers, workerMachines)

	workerPatches := filterPatchesByScope(params.Patches, PatchScopeWorker)
	if len(workerPatches) > 0 {
		writePatchesSection(buf, workerPatches)
	}
}

// writeMachineAllocation writes either machineClass or machines list to the buffer.
func writeMachineAllocation(buf *bytes.Buffer, machineClass string, size int, machines []string) {
	if machineClass != "" {
		fmt.Fprintf(buf, "machineClass:\n")
		fmt.Fprintf(buf, "  name: %s\n", machineClass)
		fmt.Fprintf(buf, "  size: %d\n", size)
	} else {
		writeMachinesList(buf, machines)
	}
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
