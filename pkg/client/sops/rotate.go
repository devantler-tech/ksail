package sops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/keys"
	"github.com/getsops/sops/v3/keyservice"
)

// RotateOpts contains all options needed for the rotation operation.
type RotateOpts struct {
	// AddKeys are master keys to add as recipients during rotation.
	AddKeys []keys.MasterKey
	// RemoveKeys are public key strings to remove from recipients during rotation.
	// Keys are matched by their ToString() representation.
	RemoveKeys []string
	// KeyServices are the key services to use for decryption/encryption.
	KeyServices []keyservice.KeyServiceClient
	// DecryptionOrder controls the order in which key types are tried for decryption.
	DecryptionOrder []string
	// IgnoreMAC ignores the Message Authentication Code when decrypting.
	IgnoreMAC bool
}

// ErrUnsupportedKeyType is returned when a key string cannot be auto-detected.
var ErrUnsupportedKeyType = errors.New("unsupported key type")

// FindEncryptedFiles discovers SOPS-encrypted YAML/JSON files in rootDir.
// When recursive is true, subdirectories are scanned; otherwise only
// direct children of rootDir are checked. In recursive mode, hidden
// subdirectories (starting with ".") are skipped, but rootDir itself is
// still scanned when explicitly provided, even if it is hidden. Symlinks
// are always skipped.
func FindEncryptedFiles(rootDir string, recursive bool) ([]string, error) {
	canonicalRootDir, err := fsutil.EvalCanonicalPath(rootDir)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing directory %q: %w", rootDir, err)
	}

	if recursive {
		return findEncryptedFilesRecursive(canonicalRootDir)
	}

	return findEncryptedFilesFlat(canonicalRootDir)
}

// isHiddenDir reports whether a directory entry is hidden (name starts with ".").
func isHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func findEncryptedFilesRecursive(rootDir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(rootDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != rootDir && isHiddenDir(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		encrypted, checkErr := checkFileForEncryption(path)
		if checkErr != nil {
			return checkErr
		}

		if encrypted {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory %q: %w", rootDir, err)
	}

	return files, nil
}

func findEncryptedFilesFlat(rootDir string) ([]string, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", rootDir, err)
	}

	var files []string

	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}

		path := filepath.Join(rootDir, entry.Name())

		encrypted, checkErr := checkFileForEncryption(path)
		if checkErr != nil {
			return nil, fmt.Errorf("checking file %q: %w", path, checkErr)
		}

		if encrypted {
			files = append(files, path)
		}
	}

	return files, nil
}

func isSupportedExtension(path string) bool {
	ext := filepath.Ext(path)

	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// checkFileForEncryption returns true if the file has a supported extension
// and contains SOPS metadata.
func checkFileForEncryption(path string) (bool, error) {
	if !isSupportedExtension(path) {
		return false, nil
	}

	return IsFileEncrypted(path)
}

// IsFileEncrypted checks whether a file contains SOPS metadata by
// loading it as a plain file and looking for the sops top-level key.
// I/O errors (read failures, permission errors) are returned as errors.
// Parse/format errors are treated as "not encrypted" (returns false, nil).
func IsFileEncrypted(path string) (bool, error) {
	canonPath, pathErr := fsutil.EvalCanonicalPath(path)
	if pathErr != nil {
		return false, fmt.Errorf("canonicalizing path %q: %w", path, pathErr)
	}

	data, err := os.ReadFile(canonPath) //nolint:gosec // canonicalized via EvalCanonicalPath
	if err != nil {
		return false, fmt.Errorf("reading file %q: %w", canonPath, err)
	}

	store, _, storeErr := GetStores(canonPath)
	if storeErr != nil {
		//nolint:nilerr // unsupported format is not an I/O error; treat as "not encrypted"
		return false, nil
	}

	branches, loadErr := store.LoadPlainFile(data)
	if loadErr != nil {
		//nolint:nilerr // unparseable content is not an I/O error; treat as "not encrypted"
		return false, nil
	}

	if len(branches) == 0 {
		return false, nil
	}

	return store.HasSopsTopLevelKey(branches[0]), nil
}

// RotateFile generates a new data key, optionally adds/removes master keys,
// and re-encrypts all values in the file. This follows the SOPS native
// rotate behavior: decrypt → modify key groups → new data key → re-encrypt.
func RotateFile(path string, opts RotateOpts) error {
	absPath, inputStore, outputStore, err := CanonicalizeAndGetStores(path)
	if err != nil {
		return err
	}

	tree, err := common.LoadEncryptedFileWithBugFixes(common.GenericDecryptOpts{
		Cipher:      aes.NewCipher(),
		InputStore:  inputStore,
		InputPath:   absPath,
		IgnoreMAC:   opts.IgnoreMAC,
		KeyServices: opts.KeyServices,
	})
	if err != nil {
		return fmt.Errorf("loading encrypted file %q: %w", absPath, err)
	}

	_, err = common.DecryptTree(common.DecryptTreeOpts{
		Cipher:          aes.NewCipher(),
		IgnoreMac:       opts.IgnoreMAC,
		Tree:            tree,
		KeyServices:     opts.KeyServices,
		DecryptionOrder: opts.DecryptionOrder,
	})
	if err != nil {
		return fmt.Errorf("decrypting %q: %w", absPath, err)
	}

	// Modify key groups (add/remove master keys)
	modifyKeyGroups(&tree.Metadata, opts)

	// Generate a new data key (core of rotate operation)
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		return fmt.Errorf("%w for %q: %v", ErrCouldNotGenerateDataKey, absPath, errs)
	}

	output, err := EncryptTreeAndEmit(tree, dataKey, aes.NewCipher(), outputStore)
	if err != nil {
		return fmt.Errorf("re-encrypting %q: %w", absPath, err)
	}

	err = os.WriteFile(absPath, output, EncryptedFilePermissions)
	if err != nil {
		return fmt.Errorf("writing rotated file %q: %w", absPath, err)
	}

	return nil
}

// modifyKeyGroups adds and removes master keys from the tree metadata.
func modifyKeyGroups(metadata *sops.Metadata, opts RotateOpts) {
	if len(opts.AddKeys) > 0 {
		if len(metadata.KeyGroups) == 0 {
			metadata.KeyGroups = append(metadata.KeyGroups, sops.KeyGroup{})
		}

		metadata.KeyGroups[0] = append(metadata.KeyGroups[0], opts.AddKeys...)
	}

	for _, removeKey := range opts.RemoveKeys {
		metadata.KeyGroups = removeKeyFromGroups(metadata.KeyGroups, removeKey)
	}
}

// removeKeyFromGroups removes keys matching the given string representation
// from all key groups, filtering out any resulting empty groups.
func removeKeyFromGroups(keyGroups []sops.KeyGroup, keyToRemove string) []sops.KeyGroup {
	result := make([]sops.KeyGroup, 0, len(keyGroups))

	for _, group := range keyGroups {
		newGroup := make(sops.KeyGroup, 0, len(group))

		for _, key := range group {
			if key.ToString() != keyToRemove {
				newGroup = append(newGroup, key)
			}
		}

		if len(newGroup) > 0 {
			result = append(result, newGroup)
		}
	}

	return result
}

// ParseKeyType auto-detects the key type from a public key string and
// returns the corresponding SOPS MasterKey. Currently supports Age keys
// (age1...); additional key types (PGP, KMS, etc.) can be added here.
func ParseKeyType(publicKey string) (keys.MasterKey, error) {
	if strings.HasPrefix(publicKey, "age1") {
		key, err := sopsage.MasterKeyFromRecipient(publicKey)
		if err != nil {
			return nil, fmt.Errorf("parsing age key: %w", err)
		}

		return key, nil
	}

	return nil, fmt.Errorf("%w: %q (supported: age1...)", ErrUnsupportedKeyType, publicKey)
}
