package image

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// blobSHA256Prefix is the path prefix for SHA256 blobs in OCI image layout tar archives.
const blobSHA256Prefix = "blobs/sha256/"

// sha256HexLength is the expected length of a SHA256 hex-encoded digest.
const sha256HexLength = 64

// ValidateExportedTar validates the integrity of SHA256 blobs in an OCI image tar archive.
// For each blob file at blobs/sha256/<hex>, it computes the SHA256 hash of the content
// and verifies it matches the expected digest from the filename.
//
// Returns nil if:
//   - The first header cannot be parsed (not a tar archive, skip validation)
//   - The tar contains no SHA256 blobs
//   - All blobs have correct digests
//
// Returns ErrBlobIntegrityFailed if:
//   - Any blob's content does not match its expected digest (truncated or corrupted export)
//   - The tar stream is corrupted mid-archive after at least one header was successfully read
func ValidateExportedTar(tarPath string) error {
	tarFile, err := os.Open(tarPath) //nolint:gosec // Path is from internal code
	if err != nil {
		return fmt.Errorf("failed to open exported tar for validation: %w", err)
	}

	defer func() { _ = tarFile.Close() }()

	tarReader := tar.NewReader(tarFile)
	entriesSeen := false

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			if !entriesSeen {
				// First header failed to parse — not a tar archive, skip validation.
				return nil
			}

			return fmt.Errorf(
				"%w: tar archive is truncated or corrupted: %w",
				ErrBlobIntegrityFailed,
				err,
			)
		}

		entriesSeen = true

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if !strings.HasPrefix(header.Name, blobSHA256Prefix) {
			continue
		}

		expectedDigest := strings.TrimPrefix(header.Name, blobSHA256Prefix)
		if len(expectedDigest) != sha256HexLength {
			continue
		}

		err = validateBlobDigest(header, tarReader)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateBlobDigest reads a single OCI blob from tarReader and verifies that its SHA256
// digest matches the expected value encoded in the blob's tar entry name.
func validateBlobDigest(header *tar.Header, tarReader *tar.Reader) error {
	expectedDigest := strings.TrimPrefix(header.Name, blobSHA256Prefix)
	hasher := sha256.New()

	bytesRead, copyErr := io.Copy(hasher, io.LimitReader(tarReader, header.Size))
	if copyErr != nil {
		return fmt.Errorf(
			"%w: failed to read blob %s: %w",
			ErrBlobIntegrityFailed,
			header.Name,
			copyErr,
		)
	}

	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != expectedDigest {
		return fmt.Errorf(
			"%w: blob %s: computed SHA256 %s (read %d of %d bytes)",
			ErrBlobIntegrityFailed,
			header.Name,
			actualDigest,
			bytesRead,
			header.Size,
		)
	}

	return nil
}
