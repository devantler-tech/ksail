package cipher

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/json"
	"github.com/spf13/cobra"
)

const notBinaryHint = "This is likely not an encrypted binary file."

var errDumpingTree = errors.New("error dumping file")

// decryptOpts contains all options needed for the decryption operation.
type decryptOpts struct {
	Cipher          sops.Cipher
	InputStore      sops.Store
	OutputStore     sops.Store
	InputPath       string
	ReadFromStdin   bool
	IgnoreMAC       bool
	Extract         []any
	KeyServices     []keyservice.KeyServiceClient
	DecryptionOrder []string
}

// decryptTree loads and decrypts a SOPS tree from the input file.
// It handles loading the encrypted file and decrypting its contents.
func decryptTree(opts decryptOpts) (*sops.Tree, error) {
	tree, _, err := decryptTreeWithKey(opts)

	return tree, err
}

// decryptTreeWithKey loads and decrypts a SOPS tree, returning both tree and data key.
// This is useful when the caller needs the data key for re-encryption.
func decryptTreeWithKey(opts decryptOpts) (*sops.Tree, []byte, error) {
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

// decrypt performs the core decryption logic for a file.
// It loads the encrypted file, decrypts it, and handles extraction if specified.
func decrypt(opts decryptOpts) ([]byte, error) {
	tree, err := decryptTree(opts)
	if err != nil {
		return nil, err
	}

	if len(opts.Extract) > 0 {
		return extract(tree, opts.Extract, opts.OutputStore)
	}

	decryptedFile, err := opts.OutputStore.EmitPlainFile(tree.Branches)

	return handleEmitError(err, decryptedFile)
}

// handleEmitError processes errors from EmitPlainFile operations.
func handleEmitError(err error, data []byte) ([]byte, error) {
	if errors.Is(err, json.BinaryStoreEmitPlainError) {
		return nil, fmt.Errorf("%w: %s", err, notBinaryHint)
	}

	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("%s: %s", errDumpingTree.Error(), err),
			codes.ErrorDumpingTree,
		)
	}

	return data, nil
}

// extract retrieves a specific value or subtree from the decrypted tree.
// It supports extracting nested keys using a path array.
func extract(tree *sops.Tree, path []any, outputStore sops.Store) ([]byte, error) {
	value, err := tree.Branches[0].Truncate(path)
	if err != nil {
		return nil, fmt.Errorf("failed to truncate tree: %w", err)
	}

	if newBranch, ok := value.(sops.TreeBranch); ok {
		tree.Branches[0] = newBranch

		decrypted, err := outputStore.EmitPlainFile(tree.Branches)

		return handleEmitError(err, decrypted)
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

// NewDecryptCmd creates and returns the decrypt command.
func NewDecryptCmd() *cobra.Command {
	var (
		extract   string
		ignoreMac bool
		output    string
	)

	cmd := &cobra.Command{
		Use:   "decrypt <file>",
		Short: "Decrypt a file with SOPS",
		Long: `Decrypt a file using SOPS (Secrets OPerationS).

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault

Example:
  ksail cipher decrypt secrets.yaml
  ksail cipher decrypt secrets.yaml --extract '["data"]["password"]'
  ksail cipher decrypt secrets.yaml --output plaintext.yaml
  ksail cipher decrypt secrets.yaml --ignore-mac`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleDecryptRunE(cmd, args, extract, ignoreMac, output)
		},
	}

	cmd.Flags().StringVarP(
		&extract,
		"extract",
		"e",
		"",
		"extract a specific key from the decrypted file (JSONPath format)",
	)
	cmd.Flags().BoolVar(
		&ignoreMac,
		"ignore-mac",
		false,
		"ignore Message Authentication Code (MAC) check",
	)
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path (default: stdout)")

	return cmd
}

const decryptedFilePermissions = 0o600

// handleDecryptRunE is the main handler for the decrypt command.
// It orchestrates the decryption workflow: determining file stores,
// setting up decryption options, decrypting the file, and writing
// the decrypted content to stdout or a file.
func handleDecryptRunE(
	cmd *cobra.Command,
	args []string,
	extract string,
	ignoreMac bool,
	output string,
) error {
	var inputPath string

	readFromStdin := len(args) == 0

	if !readFromStdin {
		inputPath = args[0]
	}

	inputStore, outputStore, err := getDecryptStores(inputPath, readFromStdin)
	if err != nil {
		return err
	}

	var extractPath []any
	if extract != "" {
		extractPath, err = parseExtractPath(extract)
		if err != nil {
			return fmt.Errorf("failed to parse extract path: %w", err)
		}
	}

	opts := decryptOpts{
		Cipher:          aes.NewCipher(),
		InputStore:      inputStore,
		OutputStore:     outputStore,
		InputPath:       inputPath,
		ReadFromStdin:   readFromStdin,
		IgnoreMAC:       ignoreMac,
		Extract:         extractPath,
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	decryptedData, err := decrypt(opts)
	if err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}

	return writeDecryptedOutput(cmd, decryptedData, output)
}

// writeDecryptedOutput writes decrypted data to either a file or stdout.
func writeDecryptedOutput(cmd *cobra.Command, data []byte, outputPath string) error {
	if outputPath != "" {
		err := os.WriteFile(outputPath, data, decryptedFilePermissions)
		if err != nil {
			return fmt.Errorf("failed to write decrypted file: %w", err)
		}

		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Successfully decrypted to %s\n", outputPath)
		if err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}

		return nil
	}

	_, err := cmd.OutOrStdout().Write(data)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}

// getDecryptStores returns the appropriate SOPS stores for decryption.
// When reading from stdin, it defaults to YAML format.
// For JSON format from stdin, users can pipe to a file first.
//
//nolint:ireturn // Returning sops.Store interface is the correct pattern here
func getDecryptStores(inputPath string, readFromStdin bool) (sops.Store, sops.Store, error) {
	if readFromStdin {
		// Default to YAML for stdin - most common format
		return getStores("stdin.yaml")
	}

	return getStores(inputPath)
}

var errInvalidExtractPath = errors.New("invalid extract path format")

// parseExtractPath converts a JSONPath-like extract string into a path array.
// Example: '["data"]["password"]' -> []any{"data", "password"}.
func parseExtractPath(extract string) ([]any, error) {
	// Remove outer quotes if present
	extract = strings.Trim(extract, "'\"")

	// Parse the JSONPath format: ["key1"]["key2"]
	var path []any
	//nolint:modernize // Using Split is clearer than SplitN for this use case
	parts := strings.Split(extract, "][")

	for _, part := range parts {
		// Clean up the part
		part = strings.TrimPrefix(part, "[")
		part = strings.TrimSuffix(part, "]")
		part = strings.Trim(part, "\"'")

		if part != "" {
			path = append(path, part)
		}
	}

	if len(path) == 0 {
		return nil, errInvalidExtractPath
	}

	return path, nil
}
