package talos

import (
	"fmt"
	"sort"
	"strings"

	"github.com/siderolabs/image-factory/pkg/schematic"
)

// factoryInstallerRepository is the OCI repository for Talos Image Factory installer images.
const factoryInstallerRepository = "factory.talos.dev/installer"

// Schematic is the upstream Image Factory schematic type.
// Re-exported so callers don't need to import the image-factory package directly.
type Schematic = schematic.Schematic

// NewSchematic creates a Schematic from a list of official extension names.
// The extensions are normalized (trimmed, empty entries removed, deduplicated)
// and sorted to ensure deterministic schematic IDs.
func NewSchematic(extensions []string) *Schematic {
	normalized := normalizeExtensions(extensions)

	return &Schematic{
		Customization: schematic.Customization{
			SystemExtensions: schematic.SystemExtensions{
				OfficialExtensions: normalized,
			},
		},
	}
}

// normalizeExtensions trims whitespace, drops empty strings, deduplicates,
// and sorts the extension list. This ensures deterministic schematic IDs
// and prevents subtle mismatches against Image Factory.
func normalizeExtensions(extensions []string) []string {
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

	sort.Strings(result)

	return result
}

// SchematicInstallerImage returns the Image Factory installer image reference
// for the given schematic ID and Talos version.
// Format: factory.talos.dev/installer/{schematicID}:{version}
func SchematicInstallerImage(schematicID, talosVersion string) string {
	return fmt.Sprintf("%s/%s:%s", factoryInstallerRepository, schematicID, talosVersion)
}
