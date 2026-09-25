package hetzner

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/apricote/hcloud-upload-image/hcloudimages"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// talosFactoryImageURLFormat is the URL template for Talos factory raw disk images for Hetzner Cloud.
// Parameters: schematicID, talosVersion.
const talosFactoryImageURLFormat = "https://factory.talos.dev/image/%s/%s/hcloud-amd64.raw.xz"

// snapshotBuildCleanupTimeout bounds deleting a cancelled build's temporary resources, so an
// unreachable API cannot stop the process from exiting.
const snapshotBuildCleanupTimeout = 30 * time.Second

// snapshotBuildSettleTimeout bounds how long a cancelled build waits for its uploader to return
// before looking for the resources to delete. See waitForUploadToSettle.
const snapshotBuildSettleTimeout = 10 * time.Second

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
	settle       time.Duration
}

// NewSnapshotManager creates a new SnapshotManager backed by the given Hetzner Cloud client.
// A nil logWriter is silently replaced with io.Discard.
func NewSnapshotManager(hcloudClient *hcloud.Client, logWriter io.Writer) *SnapshotManager {
	if logWriter == nil {
		logWriter = io.Discard
	}

	return &SnapshotManager{
		hcloudClient: hcloudClient,
		uploader:     hcloudimages.NewClient(hcloudClient),
		logWriter:    logWriter,
		settle:       snapshotBuildSettleTimeout,
	}
}

// EnsureTalosSnapshot ensures a Talos snapshot image exists for the given version and schematic.
// It first looks up existing images by labels (ksail.io/talos-version + ksail.io/talos-schematic + ksail.io/cluster),
// and if not found, builds one using hcloud-upload-image from the Talos factory URL.
// Snapshots are scoped per cluster so that deleting one cluster does not remove snapshots used by another.
// The resulting snapshot is labeled with LabelTalosVersion, LabelTalosSchematic, LabelTalosCluster, and
// LabelTalosSnapshotBuild. If the build is cancelled, its temporary server and SSH key are deleted.
func (sm *SnapshotManager) EnsureTalosSnapshot(
	ctx context.Context,
	clusterName string,
	talosVersion string,
	schematicID string,
) (int64, error) {
	if !strings.HasPrefix(talosVersion, "v") {
		talosVersion = "v" + talosVersion
	}

	imageID, err := sm.findExistingSnapshot(ctx, clusterName, talosVersion, schematicID)
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

	buildID := rand.Text()

	image, err := sm.buildSnapshot(ctx, buildID, hcloudimages.UploadOptions{
		ImageURL:         imageURL,
		ImageCompression: hcloudimages.CompressionXZ,
		Architecture:     hcloud.ArchitectureX86,
		Labels: map[string]string{
			LabelTalosVersion:       talosVersion,
			LabelTalosSchematic:     SchematicLabelValue(schematicID),
			LabelTalosCluster:       clusterName,
			LabelTalosSnapshotBuild: buildID,
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

// findExistingSnapshot looks up an existing Talos snapshot by cluster, version, and schematic labels.
// Snapshots are scoped per cluster to prevent cross-cluster deletion side-effects.
// Returns the first available image ID if found, or 0 if not found.
func (sm *SnapshotManager) findExistingSnapshot(
	ctx context.Context,
	clusterName string,
	talosVersion string,
	schematicID string,
) (int64, error) {
	images, err := sm.hcloudClient.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("%s=%s,%s=%s,%s=%s",
				LabelTalosVersion, talosVersion,
				LabelTalosSchematic, SchematicLabelValue(schematicID),
				LabelTalosCluster, clusterName,
			),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to look up Talos snapshot: %w", err)
	}

	for _, image := range images {
		if image.Status == hcloud.ImageStatusAvailable {
			return image.ID, nil
		}
	}

	return 0, nil
}

// uploadResult is the outcome of one snapshot upload.
type uploadResult struct {
	image *hcloud.Image
	err   error
}

// buildSnapshot runs one snapshot upload, and deletes the build's temporary server and
// SSH key if the build is cancelled.
//
// The uploader cannot do that itself: its cleanup reuses the cancelled context, and some
// of its steps, such as writing the image over SSH, ignore cancellation. So on
// cancellation this stops waiting for the upload and deletes the build's resources with
// a context detached from the cancelled one.
func (sm *SnapshotManager) buildSnapshot(
	ctx context.Context,
	buildID string,
	opts hcloudimages.UploadOptions,
) (*hcloud.Image, error) {
	// Without this, SIGINT (Ctrl-C) or SIGTERM ends the process with the temporary server
	// still running.
	buildCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := make(chan uploadResult, 1)

	go func() {
		image, err := sm.uploader.Upload(buildCtx, opts)
		done <- uploadResult{image: image, err: err}
	}()

	uploadReturned := false

	select {
	case result := <-done:
		if buildCtx.Err() == nil {
			return result.image, result.err
		}

		uploadReturned = true
	case <-buildCtx.Done():
	}

	if !uploadReturned {
		sm.waitForUploadToSettle(done)
	}

	_, _ = fmt.Fprintln(
		sm.logWriter,
		"  Snapshot build cancelled, deleting its temporary server...",
	)

	cleanupErr := sm.deleteBuildResources(ctx, buildID)
	if cleanupErr != nil {
		return nil, fmt.Errorf(
			"build cancelled: %w; deleting its temporary resources also failed: %w",
			buildCtx.Err(), cleanupErr,
		)
	}

	return nil, fmt.Errorf("build cancelled: %w", buildCtx.Err())
}

// waitForUploadToSettle waits, up to the manager's settle bound, for a cancelled upload to
// return. A create request already in flight at cancellation can still produce a server or
// SSH key on Hetzner's side, and a listing taken before it lands misses a resource that
// keeps billing. Cancellation aborts such requests, so the uploader normally returns at
// once; an upload stuck in a step that ignores cancellation has already created both, so
// the bound only limits how long that case waits.
func (sm *SnapshotManager) waitForUploadToSettle(done <-chan uploadResult) {
	settle := time.NewTimer(sm.settle)
	defer settle.Stop()

	select {
	case <-done:
	case <-settle.C:
	}
}

// deleteBuildResources deletes the temporary server and SSH key of one snapshot build.
// The two deletions run concurrently under one shared deadline, so a slow or retrying
// server deletion cannot use up the time the SSH key deletion needs.
func (sm *SnapshotManager) deleteBuildResources(ctx context.Context, buildID string) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), snapshotBuildCleanupTimeout,
	)
	defer cancel()

	selector := LabelTalosSnapshotBuild + "=" + buildID

	keysErr := make(chan error, 1)

	go func() {
		keysErr <- sm.deleteBuildSSHKeys(cleanupCtx, selector)
	}()

	serversErr := sm.deleteBuildServers(cleanupCtx, selector)

	return errors.Join(serversErr, <-keysErr)
}

// deleteBuildServers deletes the servers matching selector, retrying while a server is
// still locked by the action the build was waiting on.
func (sm *SnapshotManager) deleteBuildServers(ctx context.Context, selector string) error {
	servers, err := sm.hcloudClient.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{LabelSelector: selector},
	})
	if err != nil {
		return fmt.Errorf("failed to list temporary snapshot build servers: %w", err)
	}

	var errs []error

	for _, server := range servers {
		err := retryDelete(
			ctx,
			MaxDeleteRetries,
			DefaultDeleteRetryDelay,
			"context cancelled while retrying snapshot build server deletion",
			func() (bool, error) {
				_, _, err := sm.hcloudClient.Server.DeleteWithResult(ctx, server)
				if err == nil || hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
					return true, nil
				}

				return false, fmt.Errorf("delete server: %w", err)
			},
			func(lastErr error) error {
				return fmt.Errorf(
					"failed to delete temporary snapshot build server %s after %d attempts: %w",
					server.Name, MaxDeleteRetries, lastErr,
				)
			},
		)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		_, _ = fmt.Fprintf(sm.logWriter, "  ✓ Deleted temporary server %s\n", server.Name)
	}

	return errors.Join(errs...)
}

// deleteBuildSSHKeys deletes the SSH keys matching selector.
func (sm *SnapshotManager) deleteBuildSSHKeys(ctx context.Context, selector string) error {
	keys, err := sm.hcloudClient.SSHKey.AllWithOpts(ctx, hcloud.SSHKeyListOpts{
		ListOpts: hcloud.ListOpts{LabelSelector: selector},
	})
	if err != nil {
		return fmt.Errorf("failed to list temporary snapshot build SSH keys: %w", err)
	}

	var errs []error

	for _, key := range keys {
		_, err := sm.hcloudClient.SSHKey.Delete(ctx, key)
		if err != nil && !hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
			errs = append(errs, fmt.Errorf(
				"failed to delete temporary snapshot build SSH key %s: %w", key.Name, err,
			))
		}
	}

	return errors.Join(errs...)
}
