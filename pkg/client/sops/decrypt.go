package sops

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/stores/json"
)

// DecryptTree loads and decrypts a SOPS tree from the input file.
// It handles loading the encrypted file and decrypting its contents.
func DecryptTree(opts DecryptOpts) (*sops.Tree, error) {
	tree, _, err := DecryptTreeWithKey(opts)

	return tree, err
}

// DecryptTreeWithKey loads and decrypts a SOPS tree, returning both tree and data key.
// This is useful when the caller needs the data key for re-encryption.
func DecryptTreeWithKey(opts DecryptOpts) (*sops.Tree, []byte, error) {
	tree, err := common.LoadEncryptedFileWithBugFixes(common.GenericDecryptOpts{
		Cipher:        opts.Cipher,
		InputStore:    opts.InputStore,
		InputPath:     opts.InputPath,
		ReadFromStdin: opts.ReadFromStdin,
		IgnoreMAC:     opts.IgnoreMAC,
		KeyServices:   opts.KeyServices,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load encrypted file: %w", err)
	}

	dataKey, err := common.DecryptTree(common.DecryptTreeOpts{
		Cipher:          opts.Cipher,
		IgnoreMac:       opts.IgnoreMAC,
		Tree:            tree,
		KeyServices:     opts.KeyServices,
		DecryptionOrder: opts.DecryptionOrder,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt tree: %w", err)
	}

	return tree, dataKey, nil
}

// Decrypt performs the core decryption logic for a file.
// It loads the encrypted file, decrypts it, and handles extraction if specified.
func Decrypt(opts DecryptOpts) ([]byte, error) {
	tree, err := DecryptTree(opts)
	if err != nil {
		return nil, err
	}

	if len(opts.Extract) > 0 {
		return Extract(tree, opts.Extract, opts.OutputStore)
	}

	decryptedFile, err := opts.OutputStore.EmitPlainFile(tree.Branches)

	return HandleEmitError(err, decryptedFile)
}

// HandleEmitError processes errors from EmitPlainFile operations.
func HandleEmitError(err error, data []byte) ([]byte, error) {
	if errors.Is(err, json.BinaryStoreEmitPlainError) {
		return nil, fmt.Errorf("%w: %s", err, NotBinaryHint)
	}

	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("%s: %s", ErrDumpingTree.Error(), err),
			codes.ErrorDumpingTree,
		)
	}

	return data, nil
}

// Extract retrieves a specific value or subtree from the decrypted tree.
// It supports extracting nested keys using a path array.
func Extract(tree *sops.Tree, path []any, outputStore sops.Store) ([]byte, error) {
	value, err := tree.Branches[0].Truncate(path)
	if err != nil {
		return nil, fmt.Errorf("failed to truncate tree: %w", err)
	}

	if newBranch, ok := value.(sops.TreeBranch); ok {
		tree.Branches[0] = newBranch

		decrypted, err := outputStore.EmitPlainFile(tree.Branches)

		return HandleEmitError(err, decrypted)
	}

	if str, ok := value.(string); ok {
		return []byte(str), nil
	}

	bytes, err := outputStore.EmitValue(value)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("error dumping tree: %s", err),
			codes.ErrorDumpingTree,
		)
	}

	return bytes, nil
}

// ParseExtractPath converts a JSONPath-like extract string into a path array.
// Example: '["data"]["password"]' -> []any{"data", "password"}.
func ParseExtractPath(extract string) ([]any, error) {
	// Remove outer quotes if present
	extract = strings.Trim(extract, "'\"")

	// Parse the JSONPath format: ["key1"]["key2"]
	var path []any

	parts := strings.SplitSeq(extract, "][")

	for part := range parts {
		// Clean up the part
		part = strings.TrimPrefix(part, "[")
		part = strings.TrimSuffix(part, "]")
		part = strings.Trim(part, "\"'")

		if part != "" {
			path = append(path, part)
		}
	}

	if len(path) == 0 {
		return nil, ErrInvalidExtractPath
	}

	return path, nil
}
