package talos

import (
	"fmt"
	"slices"
	"strings"

	"github.com/siderolabs/image-factory/pkg/schematic"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
)

const (
	// factoryInstallerRepository is the legacy Image Factory installer repository.
	factoryInstallerRepository = "factory.talos.dev/installer"
	// factoryMetalInstallerRepository is the Talos 1.14+ metal installer repository.
	factoryMetalInstallerRepository = "factory.talos.dev/metal-installer"
)

// Schematic is the upstream Image Factory schematic type.
// Re-exported so callers don't need to import the image-factory package directly.
type Schematic = schematic.Schematic

// NewSchematic creates a Schematic from a list of official extension names and
// extra kernel arguments. The extensions are normalized (trimmed, empty entries
// removed, deduplicated, sorted); the kernel args are only trimmed of whitespace
// and empties, preserving order, so the schematic ID stays deterministic for a
// given configuration.
func NewSchematic(extensions, extraKernelArgs []string) *Schematic {
	normalized := NormalizeExtensions(extensions)

	return &Schematic{
		Customization: schematic.Customization{
			ExtraKernelArgs: NormalizeKernelArgs(extraKernelArgs),
			SystemExtensions: schematic.SystemExtensions{
				OfficialExtensions: normalized,
			},
		},
	}
}

// NormalizeExtensions trims whitespace, drops empty strings, deduplicates,
// and sorts the extension list. This ensures deterministic schematic IDs
// and prevents subtle mismatches against Image Factory.
func NormalizeExtensions(extensions []string) []string {
	seen := make(map[string]struct{}, len(extensions))
	result := make([]string, 0, len(extensions))

	for _, ext := range extensions {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}

		if _, ok := seen[ext]; ok {
			continue
		}

		seen[ext] = struct{}{}

		result = append(result, ext)
	}

	slices.Sort(result)

	return result
}

// NormalizeKernelArgs trims whitespace and drops empty entries from the extra
// kernel argument list. Unlike extensions, kernel args are NOT deduplicated or
// sorted: their order can be semantically meaningful (later args may override
// earlier ones), so the declared order is preserved and is what gets hashed into
// the schematic ID. Returns nil when no non-empty args remain, so an empty list
// omits customization.extraKernelArgs entirely and leaves the schematic ID
// identical to the extensions-only result.
func NormalizeKernelArgs(extraKernelArgs []string) []string {
	result := make([]string, 0, len(extraKernelArgs))

	for _, arg := range extraKernelArgs {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}

		result = append(result, arg)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// SchematicInstallerImage returns the Image Factory installer image reference
// for the given schematic ID and Talos version. Talos 1.14 moved installer
// images to platform-specific repositories, so metal targets use
// factory.talos.dev/metal-installer/{schematicID}:{version}; older versions
// retain factory.talos.dev/installer/{schematicID}:{version}.
func SchematicInstallerImage(schematicID, talosVersion string) string {
	repository := factoryInstallerRepository

	contractVersion := strings.TrimSpace(talosVersion)
	if contractVersion != "" && !strings.HasPrefix(contractVersion, "v") {
		contractVersion = "v" + contractVersion
	}

	contract, err := talosconfig.ParseContractFromVersion(contractVersion)
	if err == nil && contract.MultidocKubernetesConfigSupported() {
		repository = factoryMetalInstallerRepository
	}

	return fmt.Sprintf("%s/%s:%s", repository, schematicID, talosVersion)
}
