package hetzner

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/apricote/hcloud-upload-image/hcloudimages"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// talosFactoryImageURLFormat is the URL template for Talos factory raw disk images for Hetzner Cloud.
// Parameters: schematicID, talosVersion.
const talosFactoryImageURLFormat = "https://factory.talos.dev/image/%s/%s/hcloud-amd64.raw.xz"

// snapshotUploader is the interface used by SnapshotManager to create a snapshot from a raw image URL.
// It is satisfied by *hcloudimages.Client and can be replaced in tests.
type snapshotUploader interface {
	Upload(ctx context.Context, opts hcloudimages.UploadOptions) (*hcloud.Image, error)
}

// SnapshotManager manages Talos OS disk snapshots on Hetzner Cloud.
// It looks up existing snapshots by label selectors, and builds new ones using hcloud-upload-image.
type SnapshotManager struct {
	hcloudClient *hcloud.Client
	uploader     snapshotUploader
	logWriter    io.Writer
}

// NewSnapshotManager creates a new SnapshotManager backed by the given Hetzner Cloud client.
func NewSnapshotManager(hcloudClient *hcloud.Client, logWriter io.Writer) *SnapshotManager {
	return &SnapshotManager{
		hcloudClient: hcloudClient,
		uploader:     hcloudimages.NewClient(hcloudClient),
		logWriter:    logWriter,
	}
}

// EnsureTalosSnapshot ensures a Talos snapshot image exists for the given version and schematic.
// It first looks up existing images by labels (ksail.io/talos-version + ksail.io/talos-schematic),
// and if not found, builds one using hcloud-upload-image from the Talos factory URL.
// The resulting snapshot is labeled with LabelTalosVersion, LabelTalosSchematic, and LabelTalosCluster.
func (sm *SnapshotManager) EnsureTalosSnapshot(
	ctx context.Context,
	clusterName string,
	talosVersion string,
	schematicID string,
) (int64, error) {
	if !strings.HasPrefix(talosVersion, "v") {
		talosVersion = "v" + talosVersion
	}

	imageID, err := sm.findExistingSnapshot(ctx, talosVersion, schematicID)
	if err != nil {
		return 0, err
	}

	if imageID > 0 {
		_, _ = fmt.Fprintf(sm.logWriter, "  ✓ Found existing Talos snapshot (ID: %d)\n", imageID)

		return imageID, nil
	}

	_, _ = fmt.Fprintf(sm.logWriter,
		"  Building Talos snapshot (version: %s, schematic: %s)...\n",
		talosVersion, schematicID,
	)

	imageURL, err := url.Parse(fmt.Sprintf(talosFactoryImageURLFormat, schematicID, talosVersion))
	if err != nil {
		return 0, fmt.Errorf("failed to parse Talos image URL: %w", err)
	}

	image, err := sm.uploader.Upload(ctx, hcloudimages.UploadOptions{
		ImageURL:         imageURL,
		ImageCompression: hcloudimages.CompressionXZ,
		Architecture:     hcloud.ArchitectureX86,
		Labels: map[string]string{
			LabelTalosVersion:   talosVersion,
			LabelTalosSchematic: schematicID,
			LabelTalosCluster:   clusterName,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrSnapshotBuildFailed, err)
	}

	_, _ = fmt.Fprintf(sm.logWriter, "  ✓ Talos snapshot built (ID: %d)\n", image.ID)

	return image.ID, nil
}

// DeleteTalosSnapshots deletes all ksail-managed Talos snapshot images for the given cluster.
func (sm *SnapshotManager) DeleteTalosSnapshots(ctx context.Context, clusterName string) error {
	images, err := sm.hcloudClient.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("%s=%s", LabelTalosCluster, clusterName),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to list Talos snapshots for cluster %q: %w", clusterName, err)
	}

	if len(images) == 0 {
		return nil
	}

	_, _ = fmt.Fprintf(
		sm.logWriter,
		"Deleting %d Talos snapshot(s) for cluster %q...\n",
		len(images), clusterName,
	)

	for _, image := range images {
		_, err := sm.hcloudClient.Image.Delete(ctx, image)
		if err != nil {
			return fmt.Errorf("failed to delete Talos snapshot %d: %w", image.ID, err)
		}

		_, _ = fmt.Fprintf(sm.logWriter, "  ✓ Deleted snapshot %d\n", image.ID)
	}

	return nil
}

// findExistingSnapshot looks up an existing Talos snapshot by version and schematic labels.
// Returns the image ID if found, or 0 if not found.
func (sm *SnapshotManager) findExistingSnapshot(
	ctx context.Context,
	talosVersion string,
	schematicID string,
) (int64, error) {
	images, err := sm.hcloudClient.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("%s=%s,%s=%s",
				LabelTalosVersion, talosVersion,
				LabelTalosSchematic, schematicID,
			),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to look up Talos snapshot: %w", err)
	}

	if len(images) == 0 {
		return 0, nil
	}

	return images[0].ID, nil
}
