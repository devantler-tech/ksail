package image

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

const (
	exportFileMode         = 0o600
	exportContentDirectory = "content"
	// stagedPartName is the staged archive of the node share being merged.
	stagedPartName = "part.tar"
)

var errExportArchiveKept = errors.New("complete image archive kept in staging")

// Stage all sources privately and publish only after complete archive coverage
// is verified. A failed later node cannot replace an existing destination.
func (e *Exporter) exportSources(
	ctx context.Context,
	plan *exportPlan,
	distribution v1alpha1.Distribution,
	opts ExportOptions,
) (resultErr error) {
	// Imported digest aliases can target nested indexes. Preserve their OCI
	// structure instead of asking ctr to synthesize a flat Docker manifest.
	ociExporter := *e
	ociExporter.ociOnly = true

	directory, err := os.MkdirTemp(filepath.Dir(opts.OutputPath), ".ksail-export-")
	if err != nil {
		return fmt.Errorf("create export staging directory: %w", err)
	}

	var root *os.Root

	defer func() { resultErr = cleanupExportSources(ctx, root, directory, resultErr) }()

	root, err = os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open export staging directory: %w", err)
	}

	merger, err := newImageArchiveMerger(filepath.Join(directory, exportContentDirectory))
	if err != nil {
		return err
	}

	staging := exportStaging{root: root, merger: merger, directory: directory}

	err = ociExporter.exportShares(ctx, staging, plan, distribution, opts)
	if err != nil {
		return err
	}

	return publishExportSources(ctx, root, merger, opts.OutputPath)
}

// exportStaging is the private staging area of one multi-node export.
type exportStaging struct {
	root      *os.Root
	merger    *imageArchiveMerger
	directory string
}

// exportShares exports every planned share into the staging area. A node whose
// copy is damaged or unreadable does not end the export while another node
// holds a complete copy: its share moves to those nodes.
func (e *Exporter) exportShares(
	ctx context.Context,
	staging exportStaging,
	plan *exportPlan,
	distribution v1alpha1.Distribution,
	opts ExportOptions,
) error {
	queue := slices.Clone(plan.sources)
	failedNodes := make(map[string]bool)

	for len(queue) > 0 {
		source := queue[0]
		queue = queue[1:]

		shareErr := e.exportShare(ctx, staging, source, distribution, opts)

		// Remove the staged node archive however the share ended, so a retry on
		// another node starts clean. Failing to remove it ends the export.
		removeErr := staging.root.Remove(stagedPartName)
		if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return errors.Join(shareErr, fmt.Errorf("remove staged node archive: %w", removeErr))
		}

		if shareErr == nil {
			continue
		}

		failedNodes[source.node] = true

		alternates, err := e.replanFailedShare(ctx, plan, source, failedNodes, shareErr)
		if err != nil {
			return err
		}

		queue = append(queue, alternates...)
	}

	return nil
}

// exportShare exports one node's share into the staging area and merges it
// into the combined archive.
func (e *Exporter) exportShare(
	ctx context.Context,
	staging exportStaging,
	source nodeExportSource,
	distribution v1alpha1.Distribution,
	opts ExportOptions,
) error {
	part := opts
	part.OutputPath = filepath.Join(staging.directory, stagedPartName)

	platform, err := e.exportSource(ctx, source, distribution, part)
	if err != nil {
		return err
	}

	return ingestExportPart(ctx, staging.root, staging.merger, source.images, platform)
}

// replanFailedShare assigns a failed share to the other nodes that hold its
// images. It keeps the original failure in the error when no other node can
// serve the share, or when the export was cancelled.
func (e *Exporter) replanFailedShare(
	ctx context.Context,
	plan *exportPlan,
	source nodeExportSource,
	failedNodes map[string]bool,
	shareErr error,
) ([]nodeExportSource, error) {
	if ctx.Err() != nil {
		return nil, shareErr
	}

	alternates, err := e.replan(ctx, plan, source.wanted, failedNodes)
	if err != nil {
		return nil, errors.Join(shareErr, err)
	}

	slog.WarnContext(ctx, "Exporting images from another node after a node export failed",
		"node", source.node,
		"images", strings.Join(source.wanted, ","),
		"error", shareErr,
	)

	return alternates, nil
}

// cleanupExportSources removes staging files after the export. If neither
// publication nor fallback rename worked, it preserves the completed tarball.
func cleanupExportSources(
	ctx context.Context,
	root *os.Root,
	directory string,
	resultErr error,
) error {
	keepArchive := errors.Is(resultErr, errExportArchiveKept)

	var cleanupErr error

	if root != nil {
		if keepArchive {
			cleanupErr = root.RemoveAll(exportContentDirectory)
		}

		cleanupErr = errors.Join(cleanupErr, root.Close())
	}

	if !keepArchive {
		cleanupErr = errors.Join(cleanupErr, os.RemoveAll(directory))
	}

	if cleanupErr == nil {
		return resultErr
	}

	if resultErr != nil {
		return errors.Join(resultErr, cleanupErr)
	}

	// A nil result means the complete archive was already atomically published.
	slog.WarnContext(ctx,
		"Image archive exported but staging cleanup failed", "error", cleanupErr)

	return nil
}

// publishExportSources writes and validates the combined archive before
// checking for cancellation and attempting the atomic rename.
func publishExportSources(
	ctx context.Context, root *os.Root, merger *imageArchiveMerger, output string,
) error {
	// Publish the merged archive even when one node covers the whole plan, so a
	// multi-node export always names its images by their normalized references
	// rather than by whatever short names that node happens to hold.
	err := writeCombinedExport(ctx, root, merger)
	if err != nil {
		return err
	}

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("image export cancelled: %w", err)
	}

	return publishArchive(filepath.Join(root.Name(), "combined.tar"), output, os.Rename)
}

// publishArchive moves the validated archive at staged onto output. The rename
// is atomic, so a failed export never changes the previous archive. When output
// cannot be renamed over (a bind-mounted file, another filesystem, a protected
// directory), the previous archive is left untouched and the completed archive
// is kept beside it under a name of its own. If that also fails, the archive
// stays in staging. In either case, the error reports the recoverable path.
func publishArchive(staged, output string, rename func(string, string) error) error {
	err := rename(staged, output)
	if err == nil {
		return nil
	}

	kept, keepErr := keepArchiveBeside(staged, output, rename)
	if keepErr != nil {
		return fmt.Errorf("%w at %s: %w", errExportArchiveKept, staged, errors.Join(err, keepErr))
	}

	return fmt.Errorf(
		"publish complete image archive: %s was not replaced, archive kept at %s: %w",
		output, kept, err,
	)
}

// keepArchiveBeside moves the staged archive to a new, unique name in output's
// directory, so a later failed export cannot replace an earlier kept archive.
func keepArchiveBeside(
	staged, output string, rename func(string, string) error,
) (string, error) {
	placeholder, err := os.CreateTemp(
		filepath.Dir(output), filepath.Base(output)+".*.ksail-export",
	)
	if err != nil {
		return "", fmt.Errorf("reserve a name for the kept archive: %w", err)
	}

	kept := placeholder.Name()

	err = placeholder.Close()
	if err == nil {
		err = rename(staged, kept)
	}

	if err != nil {
		_ = os.Remove(kept)

		return "", fmt.Errorf("keep the completed archive: %w", err)
	}

	return kept, nil
}

// ingestExportPart validates the node archive against the selected references
// and preserves any read, permission, or close failure.
func ingestExportPart(
	ctx context.Context, root *os.Root, merger *imageArchiveMerger, refs []string, platform string,
) error {
	file, err := root.Open(stagedPartName)
	if err != nil {
		return fmt.Errorf("open staged node archive: %w", err)
	}

	addErr := merger.add(ctx, file, refs, platform)
	modeErr := file.Chmod(exportFileMode)

	closeErr := file.Close()

	err = errors.Join(addErr, modeErr, closeErr)
	if err != nil {
		return fmt.Errorf("ingest complete node archive: %w", err)
	}

	return nil
}

// writeCombinedExport writes the merged archive into a new staging file and
// validates the tarball before it can replace the destination.
func writeCombinedExport(ctx context.Context, root *os.Root, merger *imageArchiveMerger) error {
	file, err := root.OpenFile("combined.tar", os.O_CREATE|os.O_EXCL|os.O_WRONLY, exportFileMode)
	if err != nil {
		return fmt.Errorf("create combined image archive: %w", err)
	}

	writeErr := merger.write(ctx, file)

	err = errors.Join(writeErr, file.Close())
	if err != nil {
		return fmt.Errorf("write combined image archive: %w", err)
	}

	err = ValidateExportedTar(filepath.Join(root.Name(), "combined.tar"))
	if err != nil {
		return err
	}

	return nil
}
