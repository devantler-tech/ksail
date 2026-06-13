package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func (e *Exporter) tryExportImagesWithRepair(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
	repairImages []string,
	resolveImages []string,
) ([]string, error) {
	exportErr := e.tryExportImages(ctx, nodeName, tmpPath, platform, images)
	if exportErr == nil || len(repairImages) == 0 || !isMissingContentError(exportErr) {
		return images, exportErr
	}

	successfulRepairRefs, refreshErr := e.refreshImageContent(ctx, nodeName, platform, repairImages)
	if refreshErr != nil {
		return images, exportErr
	}

	refreshedImages := images
	if len(resolveImages) > 0 {
		refreshedImages = e.resolveSpecifiedImages(ctx, nodeName, resolveImages)
	}

	refreshedImages = preferSuccessfulRepairRefs(refreshedImages, successfulRepairRefs)

	return refreshedImages, e.tryExportImages(
		ctx,
		nodeName,
		tmpPath,
		platform,
		refreshedImages,
	)
}

func (e *Exporter) fallbackExportImages(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
	bulkErr error,
) error {
	successfulImages, failedImages := e.exportImagesOneByOne(
		ctx,
		nodeName,
		tmpPath,
		platform,
		images,
	)
	if len(successfulImages) == 0 {
		return fmt.Errorf(
			"ctr export failed for all images during individual export attempts (initial bulk export error: %w)",
			bulkErr,
		)
	}

	logFailedImageExports(failedImages)

	err := e.tryExportImages(ctx, nodeName, tmpPath, platform, successfulImages)
	if err != nil {
		return fmt.Errorf("ctr export failed: %w", err)
	}

	return nil
}

func logFailedImageExports(failedImages []string) {
	if len(failedImages) == 0 {
		return
	}

	fmt.Fprintf(
		os.Stderr,
		"warning: failed to export %d image(s): %s\n",
		len(failedImages),
		strings.Join(failedImages, ", "),
	)
}

func (e *Exporter) refreshImageContent(
	ctx context.Context,
	nodeName string,
	platform string,
	imageRefs []string,
) (map[string]string, error) {
	successfulRefs := make(map[string]string, len(imageRefs))

	var errs []error

	for _, imageRef := range imageRefs {
		successfulRef, err := e.refreshSingleImageContent(ctx, nodeName, platform, imageRef)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		successfulRefs[imageRef] = successfulRef
	}

	if len(errs) > 0 {
		return successfulRefs, errors.Join(errs...)
	}

	return successfulRefs, nil
}

func (e *Exporter) refreshSingleImageContent(
	ctx context.Context,
	nodeName string,
	platform string,
	imageRef string,
) (string, error) {
	successfulRef := ""

	var errs []error

	for _, candidate := range repairPullCandidates(imageRef) {
		// Remove the image reference before pulling. When containerd is configured
		// with discard_unpacked_layers=true (Kind's default), a re-pull of an image
		// whose snapshot already exists is a no-op — containerd skips re-downloading
		// the content blobs because the snapshot is still live. Removing the image ref
		// first forces a genuinely fresh pull that downloads all content blobs.
		// Running containers are unaffected: their snapshot leases keep the overlayfs
		// mounts alive regardless of whether the image ref exists.
		_, _ = e.executor.ExecInContainer(
			ctx,
			nodeName,
			buildCtrImagesRmCommand(candidate),
		)

		_, err := e.executor.ExecInContainer(
			ctx,
			nodeName,
			buildCtrPullCommand(platform, candidate),
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", candidate, err))

			continue
		}

		// Content fetch re-downloads blobs into the content store without unpacking,
		// keeping them present for the subsequent ctr export call even when
		// discard_unpacked_layers=true would otherwise discard them after unpacking.
		_, _ = e.executor.ExecInContainer(
			ctx,
			nodeName,
			buildCtrContentFetchCommand(platform, candidate),
		)

		if successfulRef == "" || preferBaseRef(successfulRef, candidate) {
			successfulRef = candidate
		}
	}

	if successfulRef != "" {
		return successfulRef, nil
	}

	return "", errors.Join(errs...)
}

func buildCtrImagesRmCommand(imageRef string) []string {
	return []string{
		ctrCommand,
		ctrNamespaceArg,
		ctrImages,
		"rm",
		imageRef,
	}
}

func buildCtrPullCommand(platform string, imageRef string) []string {
	return []string{
		ctrCommand,
		ctrNamespaceArg,
		ctrImages,
		"pull",
		"--platform",
		platform,
		imageRef,
	}
}

func buildCtrContentFetchCommand(platform string, imageRef string) []string {
	return []string{
		ctrCommand,
		ctrNamespaceArg,
		"content",
		"fetch",
		"--platform",
		platform,
		imageRef,
	}
}

func isMissingContentError(err error) bool {
	if err == nil {
		return false
	}

	errText := err.Error()

	return strings.Contains(errText, "failed to get reader: content digest") &&
		strings.Contains(errText, "not found")
}

// tryExportImages attempts to export a set of images using ctr.
func (e *Exporter) tryExportImages(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
) error {
	// Build ctr export command with platform flag
	// The --platform flag is critical for multi-arch images: containerd may have
	// image manifests listing multiple platforms (e.g., linux/amd64, linux/arm64),
	// but only the layers for the running platform are actually downloaded.
	// Without --platform, ctr tries to export all platforms and fails with
	// "content digest not found" for missing platform layers.
	cmd := make([]string, 0, 7+len(images)) //nolint:mnd // fixed args + images
	cmd = append(
		cmd,
		ctrCommand,
		ctrNamespaceArg,
		ctrImages,
		"export",
		"--platform",
		platform,
		tmpPath,
	)
	cmd = append(cmd, images...)

	_, err := e.executor.ExecInContainer(ctx, nodeName, cmd)

	return err
}

func (e *Exporter) tryExportSingleImageWithRepair(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	image string,
) (string, error) {
	err := e.tryExportImages(ctx, nodeName, tmpPath, platform, []string{image})
	if err == nil || !isMissingContentError(err) {
		return image, err
	}

	successfulRef, refreshErr := e.refreshSingleImageContent(ctx, nodeName, platform, image)
	if refreshErr != nil {
		return image, err
	}

	return successfulRef, e.tryExportImages(
		ctx,
		nodeName,
		tmpPath,
		platform,
		[]string{successfulRef},
	)
}

func preferSuccessfulRepairRefs(
	images []string,
	successfulRepairRefs map[string]string,
) []string {
	if len(successfulRepairRefs) == 0 {
		return images
	}

	preferred := make([]string, 0, len(images))
	for _, image := range images {
		if successfulRef, exists := successfulRepairRefs[image]; exists {
			preferred = append(preferred, successfulRef)

			continue
		}

		preferred = append(preferred, image)
	}

	return preferred
}

// exportImagesOneByOne tests each image individually and returns the list of
// images that can be successfully exported, along with the list of failed images.
func (e *Exporter) exportImagesOneByOne(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
) ([]string, []string) {
	successful := make([]string, 0, len(images))
	failed := make([]string, 0, len(images))

	for _, image := range images {
		successfulRef, err := e.tryExportSingleImageWithRepair(
			ctx,
			nodeName,
			tmpPath,
			platform,
			image,
		)
		if err == nil {
			successful = append(successful, successfulRef)
		} else {
			failed = append(failed, image)
		}
	}

	// Clean up test export file
	_, _ = e.executor.ExecInContainer(ctx, nodeName, []string{"rm", "-f", tmpPath})

	return successful, failed
}
