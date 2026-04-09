package talosprovisioner

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
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

// getRunningTalosVersion queries a Talos node for its running version tag.
func (p *Provisioner) getRunningTalosVersion(ctx context.Context, nodeIP string) (string, error) {
	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return "", fmt.Errorf("failed to create client for version check: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	resp, err := talosClient.Version(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to query Talos version: %w", err)
	}

	if len(resp.Messages) == 0 || resp.Messages[0].Version == nil {
		return "", fmt.Errorf("empty version response from node %s", nodeIP)
	}

	return resp.Messages[0].Version.Tag, nil
}
