package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(directory)) }()

	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open export staging directory: %w", err)
	}

	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()

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

		if len(sources) > 1 {
			err = root.Remove("part.tar")
			if err != nil {
				return fmt.Errorf("remove staged node archive: %w", err)
			}
		}
	}

	return publishExportSources(ctx, root, merger, len(sources), opts.OutputPath)
}

func publishExportSources(
	ctx context.Context, root *os.Root, merger *imageArchiveMerger, count int, output string,
) error {
	staged := "part.tar"
	if count > 1 {
		staged = "combined.tar"

		err := writeCombinedExport(ctx, root, merger)
		if err != nil {
			return err
		}
	}

	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("image export cancelled: %w", err)
	}

	err = os.Rename(filepath.Join(root.Name(), staged), output)
	if err != nil {
		return fmt.Errorf("publish complete image archive: %w", err)
	}

	return nil
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
