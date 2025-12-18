package cipher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores"
	"github.com/getsops/sops/v3/stores/json"
	"github.com/getsops/sops/v3/stores/yaml"
	"github.com/getsops/sops/v3/version"
	"github.com/mitchellh/go-wordwrap"
	"github.com/spf13/cobra"
)

// encryptConfig holds configuration options for SOPS encryption.
// It defines patterns for which values should be encrypted/unencrypted,
// key groups for encryption, and Shamir secret sharing threshold.
type encryptConfig struct {
	UnencryptedSuffix       string
	EncryptedSuffix         string
	UnencryptedRegex        string
	EncryptedRegex          string
	UnencryptedCommentRegex string
	EncryptedCommentRegex   string
	MACOnlyEncrypted        bool
	KeyGroups               []sops.KeyGroup
	GroupThreshold          int
}

// encryptOpts contains all options needed for the encryption operation.
// It combines encryption configuration with runtime parameters like cipher,
// stores, and key services.
type encryptOpts struct {
	encryptConfig

	Cipher        sops.Cipher
	InputStore    sops.Store
	OutputStore   sops.Store
	InputPath     string
	ReadFromStdin bool
	KeyServices   []keyservice.KeyServiceClient
}

// fileAlreadyEncryptedError indicates that a file already contains SOPS metadata
// and cannot be re-encrypted without first decrypting it.
type fileAlreadyEncryptedError struct{}

func (err *fileAlreadyEncryptedError) Error() string {
	return "file already encrypted"
}

const wrapWidth = 75

func (err *fileAlreadyEncryptedError) UserError() string {
	message := "The file you have provided contains a top-level entry called " +
		"'" + stores.SopsMetadataKey + "', or for flat file formats top-level entries starting with " +
		"'" + stores.SopsMetadataKey + "_'. This is generally due to the file already being encrypted. " +
		"SOPS uses a top-level entry called '" + stores.SopsMetadataKey + "' to store the metadata " +
		"required to decrypt the file. For this reason, SOPS cannot " +
		"encrypt files that already contain such an entry.\n\n" +
		"If this is an unencrypted file, rename the '" + stores.SopsMetadataKey + "' entry.\n\n" +
		"If this is an encrypted file and you want to edit it, use the " +
		"editor mode, for example: `sops my_file.yaml`"

	return wordwrap.WrapString(message, wrapWidth)
}

// ensureNoMetadata checks whether a file already contains SOPS metadata.
// This prevents re-encryption of already encrypted files, which would corrupt them.
func ensureNoMetadata(opts encryptOpts, branch sops.TreeBranch) error {
	if opts.OutputStore.HasSopsTopLevelKey(branch) {
		return &fileAlreadyEncryptedError{}
	}

	return nil
}

// metadataFromEncryptionConfig creates SOPS metadata from the encryption configuration.
// It converts the encryptConfig fields into a sops.Metadata structure that will be
// stored in the encrypted file.
func metadataFromEncryptionConfig(config encryptConfig) sops.Metadata {
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
	config encryptConfig,
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

var errCouldNotGenerateDataKey = errors.New("could not generate data key")

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

// encrypt performs the core encryption logic for a file.
// It loads the file, validates that it's not already encrypted, generates
// encryption keys using the configured key services, encrypts the data,
// and returns the encrypted file content.
func encrypt(opts encryptOpts) ([]byte, error) {
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

	tree, err := createSOPSTree(branches, opts.encryptConfig, opts.InputPath)
	if err != nil {
		return nil, err
	}

	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: %s", errCouldNotGenerateDataKey, errs)
	}

	return encryptTreeAndEmit(tree, dataKey, opts.Cipher, opts.OutputStore)
}

// loadFile reads file content either from stdin or from a file path.
// The source is determined by the ReadFromStdin option.
func loadFile(opts encryptOpts) ([]byte, error) {
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

// NewEncryptCmd creates and returns the encrypt command.
func NewEncryptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encrypt <file>",
		Short: "Encrypt a file with SOPS",
		Long: `Encrypt a file using SOPS (Secrets OPerationS).

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault

Example:
  ksail cipher encrypt secrets.yaml`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE:         handleEncryptRunE,
	}

	return cmd
}

const encryptedFilePermissions = 0o600

var errUnsupportedFileFormat = errors.New("unsupported file format")

// handleEncryptRunE is the main handler for the encrypt command.
// It orchestrates the encryption workflow: determining file stores,
// setting up encryption options, encrypting the file, and writing
// the encrypted content back to disk.
func handleEncryptRunE(cmd *cobra.Command, args []string) error {
	inputPath := args[0]

	inputStore, outputStore, err := getStores(inputPath)
	if err != nil {
		return err
	}

	opts := encryptOpts{
		encryptConfig: encryptConfig{
			KeyGroups:      []sops.KeyGroup{},
			GroupThreshold: 0,
		},
		Cipher:        aes.NewCipher(),
		InputStore:    inputStore,
		OutputStore:   outputStore,
		InputPath:     inputPath,
		ReadFromStdin: false,
		KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	}

	encryptedData, err := encrypt(opts)
	if err != nil {
		return fmt.Errorf("encryption failed: %w", err)
	}

	err = os.WriteFile(inputPath, encryptedData, encryptedFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to write encrypted file: %w", err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Successfully encrypted %s\n", inputPath)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}

// getStores returns the appropriate SOPS stores (input and output) based on file extension.
// It supports YAML (.yaml, .yml) and JSON (.json) file formats.
func getStores(inputPath string) (sops.Store, sops.Store, error) {
	ext := filepath.Ext(inputPath)

	switch ext {
	case ".yaml", ".yml":
		return &yaml.Store{}, &yaml.Store{}, nil
	case ".json":
		return &json.Store{}, &json.Store{}, nil
	default:
		return nil, nil, fmt.Errorf(
			"%w: %s (supported: .yaml, .yml, .json)",
			errUnsupportedFileFormat,
			ext,
		)
	}
}
