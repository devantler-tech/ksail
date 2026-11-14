package kubeconform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var errInvalidCacheEntry = errors.New("invalid refreshed schema cache entry")

// SchemaRefresh owns a temporary schema cache until a successful validation
// promotes its downloaded entries. Failed refreshes never erase existing schemas.
type SchemaRefresh struct {
	directory string
	target    string
	prepareMu sync.Mutex
}

// DefaultSchemaCacheDir returns the stable cache shared by normal validation
// and typed substitution, including the existing no-home fallback.
func DefaultSchemaCacheDir() string {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ksail", "kubeconform")
	}

	return filepath.Join(userCacheDir, "ksail", "kubeconform")
}

// NewSchemaRefresh creates an owned private cache without modifying persistent
// entries. The caller must close it on every path and commit only after validation.
func NewSchemaRefresh() (*SchemaRefresh, error) {
	directory, err := os.MkdirTemp("", "ksail-schema-refresh-")
	if err != nil {
		return nil, fmt.Errorf("create schema refresh cache: %w", err)
	}

	return &SchemaRefresh{directory: directory, target: DefaultSchemaCacheDir()}, nil
}

// CacheDirectory exposes the private cache location without making its cleanup
// target mutable by callers.
func (refresh *SchemaRefresh) CacheDirectory() string {
	return refresh.directory
}

// ValidationOptions binds a copy of the caller's options to this private cache.
// Parallel warmers share one lock so native cache files are fully written before
// another validation reads them; final resource validation remains independent.
func (refresh *SchemaRefresh) ValidationOptions(opts *ValidationOptions) *ValidationOptions {
	bound := ValidationOptions{}
	if opts != nil {
		bound = *opts
	}

	bound.CacheDirectory = refresh.directory
	bound.schemaRefresh = refresh

	return &bound
}

// Commit replaces only downloaded schema entries, atomically per file, after
// checking every entry is a regular native cache file. Unrelated cache entries stay.
func (refresh *SchemaRefresh) Commit() error {
	entries, err := os.ReadDir(refresh.directory)
	if err != nil {
		return fmt.Errorf("read refreshed schemas: %w", err)
	}

	err = validateRefreshEntries(entries)
	if err != nil {
		return err
	}

	err = os.MkdirAll(refresh.target, schemaCacheDirPerm)
	if err != nil {
		return fmt.Errorf("create persistent schema cache: %w", err)
	}

	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(refresh.directory, entry.Name()))
		if readErr != nil {
			return fmt.Errorf("read refreshed schema: %w", readErr)
		}

		err = replaceCachedSchema(refresh.target, entry.Name(), data)
		if err != nil {
			return err
		}
	}

	return nil
}

// Close removes only this refresh's owned temporary directory. It is idempotent.
func (refresh *SchemaRefresh) Close() error {
	err := os.RemoveAll(refresh.directory)
	if err != nil {
		return fmt.Errorf("remove schema refresh cache: %w", err)
	}

	return nil
}

// replaceCachedSchema stages in the destination directory before replacing an
// entry, preserving the previous bytes if staging or writing fails.
func replaceCachedSchema(directory, name string, data []byte) error {
	file, err := os.CreateTemp(directory, ".schema-refresh-")
	if err != nil {
		return fmt.Errorf("stage refreshed schema: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()

	_, err = file.Write(data)
	if err != nil {
		_ = file.Close()

		return fmt.Errorf("write refreshed schema: %w", err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("close refreshed schema: %w", err)
	}

	err = os.Rename(file.Name(), filepath.Join(directory, name))
	if err != nil {
		return fmt.Errorf("replace refreshed schema: %w", err)
	}

	return nil
}

// validateRefreshEntries rejects all unexpected inputs before persistent writes begin.
func validateRefreshEntries(entries []os.DirEntry) error {
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != hex.EncodedLen(sha256.Size) ||
			strings.Trim(name, "0123456789abcdef") != "" {
			return fmt.Errorf("%w %q: unexpected name", errInvalidCacheEntry, name)
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("inspect refreshed schema: %w", infoErr)
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w %q: not a regular file", errInvalidCacheEntry, name)
		}
	}

	return nil
}
