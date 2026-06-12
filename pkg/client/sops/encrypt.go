package sops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/version"
)

// Encrypt performs the core encryption logic for a file.
// It loads the file, validates that it's not already encrypted, generates
// encryption keys using the configured key services, encrypts the data,
// and returns the encrypted file content.
func Encrypt(opts EncryptOpts) ([]byte, error) {
	fileBytes, err := loadFile(opts)
	if err != nil {
		return nil, err
	}

	branches, err := opts.InputStore.LoadPlainFile(fileBytes)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("error unmarshalling file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	if len(branches) < 1 {
		return nil, common.NewExitError(
			"file cannot be completely empty, it must contain at least one document",
			codes.NeedAtLeastOneDocument,
		)
	}

	err = ensureNoMetadata(opts, branches[0])
	if err != nil {
		return nil, common.NewExitError(err, codes.FileAlreadyEncrypted)
	}

	tree, err := createSOPSTree(branches, opts.EncryptConfig, opts.InputPath)
	if err != nil {
		return nil, err
	}

	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrCouldNotGenerateDataKey, errs)
	}

	return encryptTreeAndEmit(tree, dataKey, opts.Cipher, opts.OutputStore)
}

// loadFile reads file content either from stdin or from a file path.
// The source is determined by the ReadFromStdin option.
func loadFile(opts EncryptOpts) ([]byte, error) {
	var fileBytes []byte

	var err error

	if opts.ReadFromStdin {
		fileBytes, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, common.NewExitError(
				fmt.Sprintf("error reading from stdin: %s", err),
				codes.CouldNotReadInputFile,
			)
		}
	} else {
		fileBytes, err = os.ReadFile(opts.InputPath)
		if err != nil {
			return nil, common.NewExitError(
				fmt.Sprintf("error reading file: %s", err),
				codes.CouldNotReadInputFile,
			)
		}
	}

	return fileBytes, nil
}

// ensureNoMetadata checks whether a file already contains SOPS metadata.
// This prevents re-encryption of already encrypted files, which would corrupt them.
func ensureNoMetadata(opts EncryptOpts, branch sops.TreeBranch) error {
	if opts.OutputStore.HasSopsTopLevelKey(branch) {
		return &FileAlreadyEncryptedError{}
	}

	return nil
}

// metadataFromEncryptionConfig creates SOPS metadata from the encryption configuration.
// It converts the EncryptConfig fields into a sops.Metadata structure that will be
// stored in the encrypted file.
func metadataFromEncryptionConfig(config EncryptConfig) sops.Metadata {
	return sops.Metadata{
		KeyGroups:               config.KeyGroups,
		UnencryptedSuffix:       config.UnencryptedSuffix,
		EncryptedSuffix:         config.EncryptedSuffix,
		UnencryptedRegex:        config.UnencryptedRegex,
		EncryptedRegex:          config.EncryptedRegex,
		UnencryptedCommentRegex: config.UnencryptedCommentRegex,
		EncryptedCommentRegex:   config.EncryptedCommentRegex,
		MACOnlyEncrypted:        config.MACOnlyEncrypted,
		Version:                 version.Version,
		ShamirThreshold:         config.GroupThreshold,
	}
}

// createSOPSTree creates a SOPS tree with the given branches, metadata config, and input path.
func createSOPSTree(
	branches sops.TreeBranches,
	config EncryptConfig,
	inputPath string,
) (*sops.Tree, error) {
	path, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	tree := &sops.Tree{
		Branches: branches,
		Metadata: metadataFromEncryptionConfig(config),
		FilePath: path,
	}

	return tree, nil
}

// encryptTreeAndEmit encrypts a tree and emits the encrypted file content.
// This is a common helper shared by encrypt and edit operations.
func encryptTreeAndEmit(
	tree *sops.Tree,
	dataKey []byte,
	cipher sops.Cipher,
	outputStore sops.Store,
) ([]byte, error) {
	err := common.EncryptTree(common.EncryptTreeOpts{
		DataKey: dataKey,
		Tree:    tree,
		Cipher:  cipher,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt tree: %w", err)
	}

	encryptedFile, err := outputStore.EmitEncryptedFile(*tree)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("could not marshal tree: %s", err),
			codes.ErrorDumpingTree,
		)
	}

	return encryptedFile, nil
}
