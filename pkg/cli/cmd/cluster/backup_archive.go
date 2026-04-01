package cluster

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
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
	outFile, err := os.Create(targetPath) //nolint:gosec // path is user-controlled output
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	gzipWriter, err := gzip.NewWriterLevel(outFile, compressionLevel)
	if err != nil {
		_ = outFile.Close()
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
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		_ = outFile.Close()
		return fmt.Errorf("failed to walk source directory: %w", err)
	}

	err = tarWriter.Close()
	if err != nil {
		_ = gzipWriter.Close()
		_ = outFile.Close()
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		_ = outFile.Close()
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	err = outFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close output file: %w", err)
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
