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

	outFile, err := os.CreateTemp(tmpDir, tmpPrefix)
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

	return commitTarball(tarWriter, gzipWriter, outFile, tmpPath, targetPath)
}

// commitTarball flushes and closes the writers, then atomically renames the
// temp file to targetPath. It is extracted from createTarball to keep that
// function within the project's line-length limit.
func commitTarball(
	tarWriter *tar.Writer,
	gzipWriter *gzip.Writer,
	outFile *os.File,
	tmpPath, targetPath string,
) error {
	err := tarWriter.Close()
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

	// Try an atomic rename first; on Unix this replaces the destination in one
	// operation, so the previous archive survives if Rename fails.
	// On Windows, Rename can fail with a permission/access error when the
	// destination already exists. Fall back to remove-and-retry only when the
	// target actually exists (os.Stat succeeds) so unrelated failures never
	// destroy a valid backup.
	err = os.Rename(tmpPath, targetPath)
	if err != nil {
		_, statErr := os.Stat(targetPath)
		if statErr == nil {
			_ = os.Remove(targetPath)

			err = os.Rename(tmpPath, targetPath)
		}
	}

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
