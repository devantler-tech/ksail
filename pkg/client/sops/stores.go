package sops

import (
	"fmt"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/stores/json"
	"github.com/getsops/sops/v3/stores/yaml"
)

// GetStores returns the appropriate SOPS stores (input and output) based on file extension.
// It supports YAML (.yaml, .yml) and JSON (.json) file formats.
func GetStores(inputPath string) (sops.Store, sops.Store, error) {
	ext := filepath.Ext(inputPath)

	switch ext {
	case ".yaml", ".yml":
		return &yaml.Store{}, &yaml.Store{}, nil
	case ".json":
		return &json.Store{}, &json.Store{}, nil
	default:
		return nil, nil, fmt.Errorf(
			"%w: %s (supported: .yaml, .yml, .json)",
			ErrUnsupportedFileFormat,
			ext,
		)
	}
}

// GetDecryptStores returns the appropriate SOPS stores for decryption.
// When reading from stdin, it defaults to YAML format.
// For JSON format from stdin, users can pipe to a file first.
func GetDecryptStores(inputPath string, readFromStdin bool) (sops.Store, sops.Store, error) {
	if readFromStdin {
		// Default to YAML for stdin - most common format
		return GetStores("stdin.yaml")
	}

	return GetStores(inputPath)
}

// CanonicalizeAndGetStores resolves the input path to a canonical form and returns the
// appropriate SOPS stores.
func CanonicalizeAndGetStores(inputPath string) (string, sops.Store, sops.Store, error) {
	canonPath, err := fsutil.EvalCanonicalPath(inputPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve input path %q: %w", inputPath, err)
	}

	inputStore, outputStore, err := GetStores(canonPath)
	if err != nil {
		return "", nil, nil, err
	}

	return canonPath, inputStore, outputStore, nil
}
