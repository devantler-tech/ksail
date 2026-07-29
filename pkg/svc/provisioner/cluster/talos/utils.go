package talosprovisioner

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
	talosimages "github.com/siderolabs/talos/pkg/images"
)

// nthIPInNetwork returns the nth IP in the network (1-indexed).
// The offset parameter specifies how many addresses to skip from the network base address.
func nthIPInNetwork(prefix netip.Prefix, offset int) (netip.Addr, error) {
	addr := prefix.Addr()

	// Convert to byte slice for manipulation
	if addr.Is4() {
		ipBytes := addr.As4()
		ipValue := uint32(ipBytes[0])<<byte0Shift |
			uint32(ipBytes[1])<<byte1Shift |
			uint32(ipBytes[2])<<byte2Shift |
			uint32(ipBytes[3])

		// Safe conversion: offset should always be small and positive for valid cluster sizes
		if offset < 0 {
			return netip.Addr{}, ErrNegativeOffset
		}

		//nolint:gosec // G115: offset validated above and bounded by cluster size
		ipValue += uint32(offset)

		return netip.AddrFrom4([4]byte{
			byte(ipValue >> byte0Shift), //nolint:gosec,nolintlint // G115
			byte(ipValue >> byte1Shift), //nolint:gosec,nolintlint // G115
			byte(ipValue >> byte2Shift), //nolint:gosec,nolintlint // G115
			byte(ipValue),               //nolint:gosec,nolintlint // G115
		}), nil
	}

	return netip.Addr{}, ErrIPv6NotSupported
}

// getStateDirectory returns the state directory for Talos clusters.
func getStateDirectory() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	stateDir := filepath.Join(homeDir, ".talos", "clusters")

	mkdirErr := os.MkdirAll(stateDir, stateDirectoryPermissions)
	if mkdirErr != nil {
		return "", fmt.Errorf("failed to create state directory: %w", mkdirErr)
	}

	return stateDir, nil
}

// extractTagFromImage extracts the tag from a container image reference.
// For example, "ghcr.io/siderolabs/talos:v1.13.0-beta.1" returns "v1.13.0-beta.1".
// Returns empty string if no tag is present.
func extractTagFromImage(image string) string {
	// Handle digest references (image@sha256:...)
	if idx := strings.LastIndex(image, "@"); idx != -1 {
		image = image[:idx]
	}

	if idx := strings.LastIndex(image, ":"); idx != -1 {
		tag := image[idx+1:]
		// Ensure we're not splitting on a port number (e.g., "localhost:5000/image")
		if !strings.Contains(tag, "/") {
			return tag
		}
	}

	return ""
}

// installerImageRepository is the OCI repository for the Talos installer image.
// The installer is distinct from the node image and contains the OS assets used
// by the LifecycleService API to perform in-place upgrades.
const installerImageRepository = "ghcr.io/siderolabs/installer"

// installerImageFromTag constructs a Talos installer image reference from a version tag.
// The installer image is used by the LifecycleService API for upgrades.
func installerImageFromTag(tag string) string {
	return installerImageRepository + ":" + tag
}

// resolveSchematicID returns the configured Talos Image Factory schematic ID,
// preferring an explicit talosOpts.SchematicID and falling back to the schematic
// auto-computed from spec.cluster.talos.extensions (talosConfigs.SchematicID()).
// Returns "" when no schematic is configured. This is the single source of truth
// for schematic resolution shared by the upgrade and snapshot/create lifecycle.
func (p *Provisioner) resolveSchematicID() string {
	if p.talosOpts != nil {
		if id := strings.TrimSpace(p.talosOpts.SchematicID); id != "" {
			return id
		}
	}

	if p.talosConfigs != nil {
		if id := strings.TrimSpace(p.talosConfigs.SchematicID()); id != "" {
			return id
		}
	}

	return ""
}

// resolveInstallerImage returns the Talos installer image reference for an OS
// upgrade to the given version tag. When a schematic is configured (explicit
// schematicId or auto-computed from extensions) it returns the version-appropriate
// Image Factory installer so the upgrade preserves system extensions — matching
// the create/snapshot/autoscaler paths. Talos 1.14+ uses the platform-specific
// factory.talos.dev/metal-installer/<schematicID>:<tag> repository.
// Without a custom schematic, Talos 1.14+ uses the Image Factory's default
// metal-installer schematic because release installers are no longer published
// to ghcr.io; older releases retain the legacy ghcr.io installer.
func (p *Provisioner) resolveInstallerImage(toVersion string) string {
	if schematicID := p.resolveSchematicID(); schematicID != "" {
		return talosconfigmanager.SchematicInstallerImage(schematicID, toVersion)
	}

	parsed, err := versionresolver.ParseVersion(toVersion)
	if err == nil && (parsed.Major > 1 || parsed.Major == 1 && parsed.Minor >= 14) {
		return talosimages.InstallerImageRepository("metal") + ":" + toVersion
	}

	return installerImageFromTag(toVersion)
}

// getRunningTalosVersion queries a Talos node for its running version tag. The
// version read is idempotent, so a transient apid failure retries the whole
// create-and-read with a fresh client per attempt.
func (p *Provisioner) getRunningTalosVersion(ctx context.Context, nodeIP string) (string, error) {
	var tag string

	err := p.retryTransientTalosAPICall(ctx, nodeIP, "Version check",
		func(ctx context.Context) error {
			talosClient, clientErr := p.createTalosClient(ctx, nodeIP)
			if clientErr != nil {
				return clientErr
			}

			defer talosClient.Close() //nolint:errcheck

			version, verErr := versionTagFromClient(ctx, talosClient)
			if verErr != nil {
				return fmt.Errorf("node %s: %w", nodeIP, verErr)
			}

			tag = version

			return nil
		})
	if err != nil {
		return "", fmt.Errorf("version check for node %s: %w", nodeIP, err)
	}

	return tag, nil
}
