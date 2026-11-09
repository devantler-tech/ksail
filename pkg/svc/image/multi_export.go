package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

const exportFileMode = 0o600

// Stage all sources privately and publish only after complete archive coverage
// is verified. A failed later node cannot replace an existing destination.
func (e *Exporter) exportSources(
	ctx context.Context,
	sources []nodeExportSource,
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

	merger, err := newImageArchiveMerger(filepath.Join(directory, "content"))
	if err != nil {
		return err
	}

	for _, source := range sources {
		part := opts
		part.OutputPath = filepath.Join(directory, "part.tar")

		platform, exportErr := ociExporter.exportSource(ctx, source, distribution, part)
		if exportErr != nil {
			return exportErr
		}

		err := ingestExportPart(ctx, root, merger, source.images, platform)
		if err != nil {
			return err
		}

		err = root.Remove("part.tar")
		if err != nil {
			return fmt.Errorf("remove staged node archive: %w", err)
		}
	}

	return publishExportSources(ctx, root, merger, opts.OutputPath)
}

func cleanupExportSources(
	ctx context.Context,
	root *os.Root,
	directory string,
	resultErr error,
) error {
	var closeErr error
	if root != nil {
		closeErr = root.Close()
	}

	cleanupErr := errors.Join(closeErr, os.RemoveAll(directory))
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

// publishArchive moves the validated archive at staged onto output. A rename is
// atomic, so a failed export never touches the previous archive. When output
// cannot be renamed over (a bind-mounted file, or another filesystem), the
// archive is copied into it instead. If that copy fails, the archive is moved
// next to output and the error names it, so a completed export is not lost.
func publishArchive(staged, output string, rename func(string, string) error) error {
	err := rename(staged, output)
	if err == nil {
		return nil
	}

	if !errors.Is(err, syscall.EBUSY) && !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("publish complete image archive: %w", err)
	}

	copyErr := copyArchiveInto(staged, output)
	if copyErr == nil {
		return nil
	}

	kept := output + ".ksail-export"

	keepErr := rename(staged, kept)
	if keepErr != nil {
		return fmt.Errorf(
			"publish complete image archive: copy into %s: %w",
			output,
			errors.Join(copyErr, keepErr),
		)
	}

	return fmt.Errorf(
		"publish complete image archive: copy into %s failed, archive kept at %s: %w",
		output, kept, copyErr,
	)
}

// copyArchiveInto overwrites the existing file at output with the archive at
// staged and syncs it to disk.
func copyArchiveInto(staged, output string) error {
	source, err := os.Open(staged) //nolint:gosec // staged is the export's own staging file.
	if err != nil {
		return fmt.Errorf("open staged archive: %w", err)
	}

	defer func() { _ = source.Close() }()

	//nolint:gosec // output is the caller's canonicalized export path.
	target, err := os.OpenFile(output, os.O_WRONLY|os.O_TRUNC, exportFileMode)
	if err != nil {
		return fmt.Errorf("open export destination: %w", err)
	}

	_, copyErr := io.Copy(target, source)
	syncErr := target.Sync()
	closeErr := target.Close()

	return errors.Join(copyErr, syncErr, closeErr)
}

func ingestExportPart(
	ctx context.Context, root *os.Root, merger *imageArchiveMerger, refs []string, platform string,
) error {
	file, err := root.Open("part.tar")
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
