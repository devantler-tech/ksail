package hetzner

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/apricote/hcloud-upload-image/hcloudimages"
	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
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

// snapshotDownloadAttempts bounds how many times a snapshot build is attempted when the
// temporary server fails to download the Talos image from the Image Factory.
const snapshotDownloadAttempts = 3

// snapshotDownloadRetryDelay is the wait before the second attempt of a snapshot build whose
// image download failed. It doubles before each later attempt, up to
// snapshotDownloadRetryMaxDelay.
const (
	snapshotDownloadRetryDelay    = 15 * time.Second
	snapshotDownloadRetryMaxDelay = time.Minute
)

// snapshotImageProbeTimeout bounds the request that checks whether the Image Factory serves the
// image a failed download asked for.
const snapshotImageProbeTimeout = 30 * time.Second

// imageDownloadFailure is the message hcloud-upload-image (hcloudimages v1.4.0) wraps around a
// failure of the command that downloads the image on the temporary server and writes it to
// disk. The library returns no typed error for that step, so its message is what tells a
// download failure apart from the steps that are not worth repeating, such as authentication,
// quota or server creation.
const imageDownloadFailure = "failed to download and write the image"

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
	retryDelay   time.Duration
	probeImage   imageProbe
}

// imageProbe reports the HTTP status the Image Factory answers for an image URL.
type imageProbe func(ctx context.Context, imageURL *url.URL) (int, error)

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
		retryDelay:   snapshotDownloadRetryDelay,
		probeImage:   headImageURL,
	}
}

// EnsureTalosSnapshot ensures a Talos snapshot image exists for the given version and schematic.
// It first looks up existing images by labels (ksail.io/talos-version + ksail.io/talos-schematic + ksail.io/cluster),
// and if not found, builds one using hcloud-upload-image from the Talos factory URL.
// Snapshots are scoped per cluster so that deleting one cluster does not remove snapshots used by another.
// The resulting snapshot is labeled with LabelTalosVersion, LabelTalosSchematic, LabelTalosCluster, and
// LabelTalosSnapshotBuild. If the build is cancelled, its temporary server and SSH key are deleted.
// beforeBuild, when non-nil, runs only when a snapshot has to be built, before the factory image is
// requested; an error from it aborts the build.
func (sm *SnapshotManager) EnsureTalosSnapshot(
	ctx context.Context,
	clusterName string,
	talosVersion string,
	schematicID string,
	beforeBuild func(context.Context) error,
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

	if beforeBuild != nil {
		err = beforeBuild(ctx)
		if err != nil {
			return 0, fmt.Errorf("preparing the Talos snapshot image: %w", err)
		}
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

	image, err := sm.buildSnapshotRetryingDownload(ctx, buildID, hcloudimages.UploadOptions{
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
		Type:          []hcloud.ImageType{hcloud.ImageTypeSnapshot},
		LabelSelector: fmt.Sprintf("%s=%s", LabelTalosCluster, clusterName),
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
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s,%s=%s",
			LabelTalosVersion, talosVersion,
			LabelTalosSchematic, SchematicLabelValue(schematicID),
			LabelTalosCluster, clusterName,
		),
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

// buildSnapshotRetryingDownload runs the snapshot build and repeats it, up to
// snapshotDownloadAttempts times with a growing wait, when the temporary server fails to
// download the Talos image. One dropped download from the Image Factory otherwise fails the
// whole create or update. Any other failure, and a cancelled build, is returned at once.
//
// A download also fails when the Image Factory has no image for the version and schematic,
// which no retry fixes. So before repeating, it asks the Image Factory for the image and stops
// when the answer is a client error such as 404. Before each new attempt it deletes the failed
// attempt's temporary server and SSH key; if that deletion fails, it stops rather than leave a
// second server running. It deletes them too when a download failure ends the build.
//
// SIGINT and SIGTERM cancel the whole sequence, including the probe, the cleanup and the wait
// between attempts, so an interrupt there ends the build instead of killing the process.
func (sm *SnapshotManager) buildSnapshotRetryingDownload(
	parent context.Context,
	buildID string,
	opts hcloudimages.UploadOptions,
) (*hcloud.Image, error) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	for attempt := 1; ; attempt++ {
		image, err := sm.buildSnapshot(ctx, buildID, opts)
		if err == nil || !strings.Contains(err.Error(), imageDownloadFailure) {
			return image, err
		}

		if attempt == snapshotDownloadAttempts {
			return nil, sm.withBuildCleanup(ctx, buildID, fmt.Errorf(
				"downloading the Talos image from the Image Factory failed %d times: %w",
				attempt, errors.Join(err, ctx.Err()),
			))
		}

		retryErr := sm.prepareDownloadRetry(ctx, buildID, opts.ImageURL, attempt, err)
		if retryErr != nil {
			return nil, retryErr
		}
	}
}

// prepareDownloadRetry decides whether a failed image download is worth another attempt,
// deletes the failed attempt's temporary resources and waits out the backoff. It returns the
// error to report when the build must stop instead.
func (sm *SnapshotManager) prepareDownloadRetry(
	ctx context.Context,
	buildID string,
	imageURL *url.URL,
	attempt int,
	downloadErr error,
) error {
	status, probeErr := sm.probeImage(ctx, imageURL)
	if probeErr == nil && imageMissing(status) {
		// A build cancelled while the probe was answered is reported as cancelled, as every
		// other cancelled step of the build is.
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return sm.withBuildCleanup(ctx, buildID, fmt.Errorf(
				"cancelled while checking the Talos image: %w", errors.Join(ctxErr, downloadErr),
			))
		}

		return sm.withBuildCleanup(ctx, buildID, fmt.Errorf(
			"the Image Factory has no image at %s (HTTP %d), so the download is not retried: %w",
			imageURL, status, downloadErr,
		))
	}

	delay := netretry.ExponentialDelay(
		attempt, sm.retryDelay, max(sm.retryDelay, snapshotDownloadRetryMaxDelay),
	)

	_, _ = fmt.Fprintf(sm.logWriter,
		"  Talos image download failed (attempt %d of %d), retrying in %s...\n",
		attempt, snapshotDownloadAttempts, delay,
	)

	cleanupErr := sm.deleteBuildResources(ctx, buildID)
	if cleanupErr != nil {
		return fmt.Errorf(
			"%w; deleting the failed attempt's temporary resources also failed, "+
				"so the download is not retried: %w",
			errors.Join(downloadErr, ctx.Err()), cleanupErr,
		)
	}

	return waitForBackoff(ctx, "cancelled while waiting to retry the Talos image download", delay)
}

// withBuildCleanup deletes the failed attempt's temporary server and SSH key before a download
// failure ends the build, so a resource the uploader could not remove does not keep running.
// A failed deletion is reported together with buildErr.
func (sm *SnapshotManager) withBuildCleanup(
	ctx context.Context,
	buildID string,
	buildErr error,
) error {
	cleanupErr := sm.deleteBuildResources(ctx, buildID)
	if cleanupErr == nil {
		return buildErr
	}

	return fmt.Errorf(
		"%w; deleting the failed attempt's temporary resources also failed: %w",
		buildErr, cleanupErr,
	)
}

// imageMissing reports whether an Image Factory status means the image does not exist, as
// opposed to a failure that may pass. Only "not found", "gone" and "bad request" (an unknown
// version or schematic) say that. Other client errors, such as a method a proxy does not allow,
// a refused request, a timeout or a rate limit, say nothing about the image, so the download
// is retried.
func imageMissing(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusGone:
		return true
	default:
		return false
	}
}

// headImageURL asks the Image Factory for imageURL without downloading it and returns the
// status it answers with.
func headImageURL(ctx context.Context, imageURL *url.URL) (int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, snapshotImageProbeTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(probeCtx, http.MethodHead, imageURL.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("building the Image Factory request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("asking the Image Factory for the image: %w", err)
	}

	_ = response.Body.Close()

	return response.StatusCode, nil
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
		LabelSelector: selector,
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
				result, _, err := sm.hcloudClient.Server.DeleteWithResult(ctx, server)
				if hcloud.IsError(err, hcloud.ErrorCodeNotFound) {
					return true, nil
				}

				if err != nil {
					return false, fmt.Errorf("delete server: %w", err)
				}

				// An accepted request only queues the deletion, which can still fail.
				err = sm.hcloudClient.Action.WaitFor(ctx, result.Action)
				if err != nil {
					return false, fmt.Errorf("wait for server deletion: %w", err)
				}

				return true, nil
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
		LabelSelector: selector,
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
