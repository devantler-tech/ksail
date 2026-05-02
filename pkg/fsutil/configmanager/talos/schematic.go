package talos

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"go.yaml.in/yaml/v4"
)

// factoryInstallerRepository is the OCI repository for Talos Image Factory installer images.
const factoryInstallerRepository = "factory.talos.dev/installer"

// Schematic represents the requested image customization, matching the
// Talos Image Factory schematic format exactly. The struct layout and yaml
// tags must match github.com/siderolabs/image-factory/pkg/schematic.Schematic
// so that marshalling produces identical bytes and therefore identical IDs.
//
// See https://factory.talos.dev for the schematic format documentation.
type Schematic struct {
	// Owner is the name of the schematic owner (Enterprise feature, unused by KSail).
	Owner string `yaml:"owner,omitempty"`
	// Overlay represents overlay options for image generation.
	Overlay SchematicOverlay `yaml:"overlay,omitempty"`
	// Customization represents the Talos image customization.
	Customization SchematicCustomization `yaml:"customization"`
}

// SchematicOverlay represents the overlay options for image generation.
type SchematicOverlay struct {
	Image   string         `yaml:"image,omitempty"`
	Name    string         `yaml:"name,omitempty"`
	Options map[string]any `yaml:"options,omitempty"`
}

// SchematicCustomization represents the Talos image customization.
// Field order matches the Image Factory Customization struct exactly.
type SchematicCustomization struct {
	ExtraKernelArgs  []string             `yaml:"extraKernelArgs,omitempty"`
	Meta             []SchematicMetaValue `yaml:"meta,omitempty"`
	SystemExtensions SchematicExtensions  `yaml:"systemExtensions,omitempty"`
	// Bootloader is the bootloader kind (int). Zero value = default, omitted from YAML.
	Bootloader int                 `yaml:"bootloader,omitempty"`
	SecureBoot SchematicSecureBoot `yaml:"secureboot,omitempty"`
}

// SchematicMetaValue provides initial META contents for the image.
type SchematicMetaValue struct { //nolint:govet
	Key   uint8  `yaml:"key"`
	Value string `yaml:"value"`
}

// SchematicExtensions represents the Talos system extensions to be installed.
type SchematicExtensions struct {
	OfficialExtensions []string `yaml:"officialExtensions,omitempty"`
}

// SchematicSecureBoot represents the secure boot options for the image.
type SchematicSecureBoot struct {
	IncludeWellKnownCertificates bool `yaml:"includeWellKnownCertificates,omitempty"`
}

// NewSchematic creates a Schematic from a list of official extension names.
// The extensions are sorted to ensure deterministic schematic IDs.
func NewSchematic(extensions []string) *Schematic {
	sorted := make([]string, len(extensions))
	copy(sorted, extensions)
	sort.Strings(sorted)

	return &Schematic{
		Customization: SchematicCustomization{
			SystemExtensions: SchematicExtensions{
				OfficialExtensions: sorted,
			},
		},
	}
}

// ID returns the deterministic identifier for this schematic.
// It matches the Talos Image Factory algorithm: hex(sha256(yaml.Marshal(schematic))).
// Uses go.yaml.in/yaml/v4 for marshalling, the same library as the Image Factory.
func (s *Schematic) ID() (string, error) {
	data, err := yaml.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("failed to marshal schematic: %w", err)
	}

	hash := sha256.Sum256(data)

	return hex.EncodeToString(hash[:]), nil
}

// SchematicInstallerImage returns the Image Factory installer image reference
// for the given schematic ID and Talos version.
// Format: factory.talos.dev/installer/{schematicID}:{version}
func SchematicInstallerImage(schematicID, talosVersion string) string {
	return factoryInstallerRepository + "/" + schematicID + ":" + talosVersion
}
