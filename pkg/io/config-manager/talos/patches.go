package talos

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
)

// PatchScope indicates which nodes a patch should be applied to.
type PatchScope int

const (
	// PatchScopeCluster applies to all nodes (control-planes and workers).
	PatchScopeCluster PatchScope = iota
	// PatchScopeControlPlane applies only to control-plane nodes.
	PatchScopeControlPlane
	// PatchScopeWorker applies only to worker nodes.
	PatchScopeWorker
)

// ipv4Offset is the offset for the first control plane IP in the network.
// The Talos SDK uses offset 2 (network + gateway + first node).
const ipv4Offset = 2

// Patch represents a Talos machine config patch with its scope.
type Patch struct {
	// Path is the source file path or identifier for the patch.
	Path string
	// Scope indicates which nodes this patch applies to.
	Scope PatchScope
	// Content is the raw YAML patch content.
	Content []byte
}

// Errors for patch operations.
var (
	// ErrInvalidPatch is returned when a patch cannot be parsed.
	ErrInvalidPatch = errors.New("invalid patch")
	// ErrIPv6NotSupported is returned when IPv6 addresses are used but not supported.
	ErrIPv6NotSupported = errors.New("IPv6 not supported")
	// ErrNegativeOffset is returned when a negative offset is provided for IP calculation.
	ErrNegativeOffset = errors.New("negative offset not allowed")
)

// LoadPatches loads all Talos patches from the configured directories.
// Returns patches from cluster/, control-planes/, and workers/ subdirectories.
func LoadPatches(patchesDir string) ([]Patch, error) {
	var patches []Patch

	clusterDir := filepath.Join(patchesDir, "cluster")
	controlPlanesDir := filepath.Join(patchesDir, "control-planes")
	workersDir := filepath.Join(patchesDir, "workers")

	// Load cluster-wide patches
	clusterPatches, err := loadPatchesFromDir(clusterDir, PatchScopeCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster patches: %w", err)
	}

	patches = append(patches, clusterPatches...)

	// Load control-plane patches
	cpPatches, err := loadPatchesFromDir(controlPlanesDir, PatchScopeControlPlane)
	if err != nil {
		return nil, fmt.Errorf("failed to load control-plane patches: %w", err)
	}

	patches = append(patches, cpPatches...)

	// Load worker patches
	workerPatches, err := loadPatchesFromDir(workersDir, PatchScopeWorker)
	if err != nil {
		return nil, fmt.Errorf("failed to load worker patches: %w", err)
	}

	patches = append(patches, workerPatches...)

	return patches, nil
}

// loadPatchesFromDir loads all YAML patches from a directory.
func loadPatchesFromDir(dir string, scope PatchScope) ([]Patch, error) {
	// Check if directory exists
	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil // Directory doesn't exist, no patches to load
	}

	var patches []Patch

	iterErr := forEachYAMLFile(dir, func(filePath string, content []byte) error {
		patches = append(patches, Patch{
			Path:    filePath,
			Scope:   scope,
			Content: content,
		})

		return nil
	})
	if iterErr != nil {
		return nil, iterErr
	}

	return patches, nil
}

// categorizePatchesByScope separates patches into cluster, control-plane, and worker categories.
func categorizePatchesByScope(
	patches []Patch,
) ([]configpatcher.Patch, []configpatcher.Patch, []configpatcher.Patch, error) {
	var clusterPatches, controlPlanePatches, workerPatches []configpatcher.Patch

	for _, patch := range patches {
		configPatch, loadErr := configpatcher.LoadPatch(patch.Content)
		if loadErr != nil {
			return nil, nil, nil, fmt.Errorf("%w: %s: %w", ErrInvalidPatch, patch.Path, loadErr)
		}

		switch patch.Scope {
		case PatchScopeCluster:
			clusterPatches = append(clusterPatches, configPatch)
		case PatchScopeControlPlane:
			controlPlanePatches = append(controlPlanePatches, configPatch)
		case PatchScopeWorker:
			workerPatches = append(workerPatches, configPatch)
		}
	}

	return clusterPatches, controlPlanePatches, workerPatches, nil
}

// nthIPInNetwork calculates the nth IP in a network.
// offset=0 returns the network address, offset=1 is the gateway, offset=2+ are usable IPs.
func nthIPInNetwork(network netip.Prefix, offset int) (netip.Addr, error) {
	if !network.Addr().Is4() {
		return netip.Addr{}, ErrIPv6NotSupported
	}

	if offset < 0 {
		return netip.Addr{}, ErrNegativeOffset
	}

	addr := network.Addr()
	for range offset {
		addr = addr.Next()
	}

	return addr, nil
}
