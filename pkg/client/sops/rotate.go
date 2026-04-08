package sops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/getsops/sops/v3"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/aes"
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

// Exported error variables for rotation operations.
var (
	ErrNoEncryptedFiles = errors.New("no SOPS-encrypted files found")
	ErrUnsupportedKeyType = errors.New("unsupported key type")
)

// FindEncryptedFiles discovers SOPS-encrypted YAML/JSON files in rootDir.
// When recursive is true, subdirectories are scanned; otherwise only
// direct children of rootDir are checked. Hidden directories (starting
// with ".") are always skipped.
func FindEncryptedFiles(rootDir string, recursive bool) ([]string, error) {
	if recursive {
		return findEncryptedFilesRecursive(rootDir)
	}

	return findEncryptedFilesFlat(rootDir)
}

func findEncryptedFilesRecursive(rootDir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			base := d.Name()
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}

			return nil
		}

		if !isSupportedExtension(path) {
			return nil
		}

		encrypted, checkErr := IsFileEncrypted(path)
		if checkErr != nil {
			return checkErr
		}

		if !encrypted {
			return nil
		}

		files = append(files, path)

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
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(rootDir, entry.Name())
		if !isSupportedExtension(path) {
			continue
		}

		encrypted, checkErr := IsFileEncrypted(path)
		if checkErr != nil {
			return nil, fmt.Errorf("checking file %q: %w", path, checkErr)
		}

		if !encrypted {
			continue
		}

		files = append(files, path)
	}

	return files, nil
}

func isSupportedExtension(path string) bool {
	ext := filepath.Ext(path)

	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// IsFileEncrypted checks whether a file contains SOPS metadata by
// loading it as a plain file and looking for the sops top-level key.
// I/O errors (read failures, permission errors) are returned as errors.
// Parse/format errors are treated as "not encrypted" (returns false, nil).
func IsFileEncrypted(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading file %q: %w", path, err)
	}

	store, _, storeErr := GetStores(path)
	if storeErr != nil {
		return false, nil
	}

	branches, loadErr := store.LoadPlainFile(data)
	if loadErr != nil {
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
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving path %q: %w", path, err)
	}

	inputStore, outputStore, err := GetStores(absPath)
	if err != nil {
		return fmt.Errorf("getting stores for %q: %w", absPath, err)
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

	// Add new master keys to key group 0
	if len(opts.AddKeys) > 0 {
		if len(tree.Metadata.KeyGroups) == 0 {
			tree.Metadata.KeyGroups = append(tree.Metadata.KeyGroups, sops.KeyGroup{})
		}

		tree.Metadata.KeyGroups[0] = append(tree.Metadata.KeyGroups[0], opts.AddKeys...)
	}

	// Remove specified master keys from all key groups
	for _, removeKey := range opts.RemoveKeys {
		tree.Metadata.KeyGroups = removeKeyFromGroups(tree.Metadata.KeyGroups, removeKey)
	}

	// Generate a new data key (core of rotate operation)
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		return fmt.Errorf("generating new data key for %q: %v", absPath, errs)
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

// removeKeyFromGroups removes keys matching the given string representation
// from all key groups.
func removeKeyFromGroups(keyGroups []sops.KeyGroup, keyToRemove string) []sops.KeyGroup {
	result := make([]sops.KeyGroup, len(keyGroups))

	for i, group := range keyGroups {
		newGroup := make(sops.KeyGroup, 0, len(group))

		for _, key := range group {
			if key.ToString() != keyToRemove {
				newGroup = append(newGroup, key)
			}
		}

		result[i] = newGroup
	}

	return result
}

// ParseKeyType auto-detects the key type from a public key string and
// returns the corresponding SOPS MasterKey. Currently supports Age keys
// (age1...); additional key types (PGP, KMS, etc.) can be added here.
func ParseKeyType(publicKey string) (keys.MasterKey, error) {
	if strings.HasPrefix(publicKey, "age1") {
		return sopsage.MasterKeyFromRecipient(publicKey)
	}

	return nil, fmt.Errorf("%w: %q (supported: age1...)", ErrUnsupportedKeyType, publicKey)
}
