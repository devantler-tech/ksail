package talosindockerprovisioner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
)

// GenerateMirrorPatchYAML generates a Talos machine config patch for registry mirrors.
// The patch follows the Talos v1alpha1 machine.registries.mirrors format.
//
// Example output:
//
//	machine:
//	  registries:
//	    mirrors:
//	      docker.io:
//	        endpoints:
//	          - https://registry.example.com
//	          - https://registry-1.docker.io
//	        overridePath: true
func GenerateMirrorPatchYAML(specs []registry.MirrorSpec) string {
	if len(specs) == 0 {
		return ""
	}

	// Sort specs by host for deterministic output
	sortedSpecs := make([]registry.MirrorSpec, len(specs))
	copy(sortedSpecs, specs)
	sort.Slice(sortedSpecs, func(i, j int) bool {
		return sortedSpecs[i].Host < sortedSpecs[j].Host
	})

	var builder strings.Builder
	builder.WriteString("machine:\n")
	builder.WriteString("  registries:\n")
	builder.WriteString("    mirrors:\n")

	for _, spec := range sortedSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Determine the upstream URL
		upstream := strings.TrimSpace(spec.Remote)
		if upstream == "" {
			upstream = registry.GenerateUpstreamURL(host)
		}

		// Generate the mirror entry
		builder.WriteString(fmt.Sprintf("      %s:\n", host))
		builder.WriteString("        endpoints:\n")
		builder.WriteString(fmt.Sprintf("          - %s\n", upstream))

		// Add the default upstream as fallback
		defaultUpstream := registry.GenerateUpstreamURL(host)
		if upstream != defaultUpstream {
			builder.WriteString(fmt.Sprintf("          - %s\n", defaultUpstream))
		}

		builder.WriteString("        overridePath: true\n")
	}

	return builder.String()
}

// CreateMirrorConfigPatch creates an in-memory Talos config patch for registry mirrors.
// This is used when --mirror-registry flag is provided but no declarative config exists.
//
//nolint:ireturn // configpatcher.Patch is the SDK's interface type
func CreateMirrorConfigPatch(specs []registry.MirrorSpec) (configpatcher.Patch, error) {
	if len(specs) == 0 {
		return nil, nil //nolint:nilnil // nil patch means no mirrors configured
	}

	patchYAML := GenerateMirrorPatchYAML(specs)
	if patchYAML == "" {
		return nil, nil //nolint:nilnil // empty patch content
	}

	patches, err := configpatcher.LoadPatches([]string{patchYAML})
	if err != nil {
		return nil, fmt.Errorf("failed to load mirror config patch: %w", err)
	}

	if len(patches) == 0 {
		return nil, nil //nolint:nilnil // no patches loaded
	}

	return patches[0], nil
}
