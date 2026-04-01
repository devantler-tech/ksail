package cluster

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// defaultCompressionLevel uses gzip.DefaultCompression so the constant stays
// co-located with the gzip import and avoids a magic number in backup.go.
const defaultCompressionLevel = gzip.DefaultCompression

func writeMetadata(metadata *BackupMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	err = os.WriteFile(path, data, filePerm)
	if err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

func createTarball(
	sourceDir, targetPath string,
	compressionLevel int,
) error {
	// Use os.CreateTemp so the temp path is unique — avoids clobbering a
	// pre-existing .tmp file from a previous failed run and reduces races.
	tmpDir := filepath.Dir(targetPath)
	tmpPrefix := filepath.Base(targetPath) + ".tmp-"

	outFile, err := os.CreateTemp(tmpDir, tmpPrefix) //nolint:gosec // path is user-controlled output
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := outFile.Name()

	gzipWriter, err := gzip.NewWriterLevel(outFile, compressionLevel)
	if err != nil {
		_ = outFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}

	tarWriter := tar.NewWriter(gzipWriter)

	err = filepath.Walk(
		sourceDir,
		func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			return addFileToTar(tarWriter, sourceDir, path, info)
		},
	)
	if err != nil {
		// Surface any close errors alongside the walk error so callers see both.
		closeErr := errors.Join(tarWriter.Close(), gzipWriter.Close(), outFile.Close())
		_ = os.Remove(tmpPath)

		return errors.Join(fmt.Errorf("failed to walk source directory: %w", err), closeErr)
	}

	err = tarWriter.Close()
	if err != nil {
		_ = gzipWriter.Close()
		_ = outFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		_ = outFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	err = outFile.Close()
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close output file: %w", err)
	}

	// Remove targetPath first so os.Rename works on Windows when the target
	// already exists (on Unix, Rename atomically replaces the destination).
	_ = os.Remove(targetPath) //nolint:errcheck // best-effort; Rename will report any real error

	err = os.Rename(tmpPath, targetPath) //nolint:gosec // both paths are user-controlled output
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize archive: %w", err)
	}

	return nil
}

func addFileToTar(
	tarWriter *tar.Writer,
	sourceDir, path string,
	info os.FileInfo,
) error {
	// Skip symlinks and special files (devices, pipes, sockets, etc.).
	// restore explicitly rejects non-regular files, so including them would
	// produce backups that cannot be restored.
	if !info.IsDir() && info.Mode()&os.ModeType != 0 {
		return nil
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to create tar header: %w", err)
	}

	relPath, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	header.Name = filepath.ToSlash(relPath)

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if info.IsDir() {
		return nil
	}

	file, err := os.Open( //nolint:gosec // G304: path from archive walk
		path,
	)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func() { _ = file.Close() }()

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return fmt.Errorf("failed to write file to tar: %w", err)
	}

	return nil
}
