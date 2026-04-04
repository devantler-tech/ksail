package cipher

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/json"
	"github.com/getsops/sops/v3/stores/yaml"
	"github.com/getsops/sops/v3/version"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	mock "github.com/stretchr/testify/mock"
)

// NewCipherCmd creates the cipher command that integrates with SOPS.
func NewCipherCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cipher",
		Short: "Manage encrypted files with SOPS",
		Long: `Cipher command provides access to SOPS (Secrets OPerationS) functionality
for encrypting and decrypting files.

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault`,
		SilenceUsage: true,
		Annotations: map[string]string{
			// Consolidate cipher subcommands (encrypt, decrypt, edit, import)
			// into a single cipher_write tool (all operations modify state or reveal secrets).
			// The "cipher_operation" parameter will select which operation to perform.
			annotations.AnnotationConsolidate: "cipher_operation",
		},
	}

	// Add subcommands
	cmd.AddCommand(NewEncryptCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewDecryptCmd())
	cmd.AddCommand(NewImportCmd())

	return cmd
}

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
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
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

		// Canonicalize user-supplied input path (resolve symlinks + absolute)
		// so that the actual file being decrypted is predictable and
		// symlink-escape attacks are prevented in CI pipelines.
		canonPath, err := fsutil.EvalCanonicalPath(inputPath)
		if err != nil {
			return fmt.Errorf("resolve input path %q: %w", inputPath, err)
		}

		inputPath = canonPath
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
		// Canonicalize user-supplied output path (resolve symlinks + absolute)
		// so that the actual write destination is predictable.
		canonOutput, err := fsutil.EvalCanonicalPath(outputPath)
		if err != nil {
			return fmt.Errorf("resolve output path %q: %w", outputPath, err)
		}

		err = os.WriteFile(canonOutput, data, decryptedFilePermissions)
		if err != nil {
			return fmt.Errorf("failed to write decrypted file: %w", err)
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "decrypted to %s",
			Args:    []any{outputPath},
			Writer:  cmd.OutOrStdout(),
		})

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
		return nil, errInvalidExtractPath
	}

	return path, nil
}

// storeWithExample is an interface for stores that can emit example files.
type storeWithExample interface {
	sops.Store
	EmitExample() []byte
}

// editOpts contains all options needed for the edit operation.
type editOpts struct {
	Cipher          sops.Cipher
	InputStore      sops.Store
	OutputStore     sops.Store
	InputPath       string
	IgnoreMAC       bool
	KeyServices     []keyservice.KeyServiceClient
	DecryptionOrder []string
	ShowMasterKeys  bool
}

// editExampleOpts combines editOpts with encryption configuration
// for creating and editing example files.
type editExampleOpts struct {
	editOpts

	encryptConfig

	InputStoreWithExample storeWithExample
}

// runEditorUntilOkOpts contains options for the editor loop.
type runEditorUntilOkOpts struct {
	TmpFileName    string
	OriginalHash   []byte
	InputStore     sops.Store
	ShowMasterKeys bool
	Tree           *sops.Tree
	Logger         *logrus.Logger
}

const tmpFilePermissions = os.FileMode(0o600)

var (
	errInvalidEditor            = errors.New("invalid editor configuration")
	errNoEditorAvailable        = errors.New("no editor available")
	errStoreNoExampleGeneration = errors.New("store does not support example file generation")
)

// editExample creates and edits an example file when the target file doesn't exist.
func editExample(opts editExampleOpts) ([]byte, error) {
	fileBytes := opts.InputStoreWithExample.EmitExample()

	branches, err := opts.InputStoreWithExample.LoadPlainFile(fileBytes)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("Error unmarshalling file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	tree, err := createSOPSTree(branches, opts.encryptConfig, opts.InputPath)
	if err != nil {
		return nil, err
	}

	// Generate a data key
	dataKey, errs := tree.GenerateDataKeyWithKeyServices(opts.KeyServices)
	if len(errs) > 0 {
		return nil, common.NewExitError(
			fmt.Sprintf("Error encrypting the data key with one or more master keys: %s", errs),
			codes.CouldNotRetrieveKey,
		)
	}

	return editTree(opts.editOpts, tree, dataKey)
}

// edit loads, decrypts, and allows editing of an existing encrypted file.
func edit(opts editOpts) ([]byte, error) {
	// Convert editOpts to decryptOpts for decryption
	decOpts := decryptOpts{
		Cipher:          opts.Cipher,
		InputStore:      opts.InputStore,
		OutputStore:     opts.OutputStore,
		InputPath:       opts.InputPath,
		ReadFromStdin:   false,
		IgnoreMAC:       opts.IgnoreMAC,
		KeyServices:     opts.KeyServices,
		DecryptionOrder: opts.DecryptionOrder,
	}

	tree, dataKey, err := decryptTreeWithKey(decOpts)
	if err != nil {
		return nil, err
	}

	return editTree(opts, tree, dataKey)
}

// editTree handles the core edit workflow: write to temp file, launch editor, re-encrypt.
func editTree(opts editOpts, tree *sops.Tree, dataKey []byte) ([]byte, error) {
	tmpfileName, cleanupFn, err := createTempFileWithContent(opts, tree)
	if err != nil {
		return nil, err
	}
	defer cleanupFn()

	origHash, err := hashFile(tmpfileName)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("Could not hash file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	logger := logrus.New()

	err = runEditorUntilOk(runEditorUntilOkOpts{
		InputStore:     opts.InputStore,
		OriginalHash:   origHash,
		TmpFileName:    tmpfileName,
		ShowMasterKeys: opts.ShowMasterKeys,
		Tree:           tree,
		Logger:         logger,
	})
	if err != nil {
		return nil, err
	}

	return encryptAndEmit(opts, tree, dataKey)
}

// createTempFileWithContent creates a temporary file with the tree content.
func createTempFileWithContent(opts editOpts, tree *sops.Tree) (string, func(), error) {
	tmpdir, cleanup, err := createTempDir()
	if err != nil {
		return "", nil, err
	}

	tmpfileName, err := writeTempFile(tmpdir, opts, tree, cleanup)
	if err != nil {
		return "", nil, err
	}

	return tmpfileName, cleanup, nil
}

// createTempDir creates a temporary directory for editing.
func createTempDir() (string, func(), error) {
	tmpdir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", nil, common.NewExitError(
			fmt.Sprintf("Could not create temporary directory: %s", err),
			codes.CouldNotWriteOutputFile,
		)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpdir)
	}

	return tmpdir, cleanup, nil
}

// writeTempFile writes the tree content to a temporary file.
func writeTempFile(tmpdir string, opts editOpts, tree *sops.Tree, cleanup func()) (string, error) {
	tmpfile, err := os.Create(filepath.Join(tmpdir, filepath.Base(opts.InputPath))) // #nosec G304
	if err != nil {
		cleanup()

		return "", common.NewExitError(
			fmt.Sprintf("Could not create temporary file: %s", err),
			codes.CouldNotWriteOutputFile,
		)
	}

	defer func() {
		_ = tmpfile.Close()
	}()

	chmodErr := tmpfile.Chmod(tmpFilePermissions)
	if chmodErr != nil {
		cleanup()

		return "", common.NewExitError(
			fmt.Sprintf(
				"Could not change permissions of temporary file to read-write for owner only: %s",
				chmodErr,
			),
			codes.CouldNotWriteOutputFile,
		)
	}

	out, err := emitTreeContent(opts, tree)
	if err != nil {
		cleanup()

		return "", err
	}

	_, err = tmpfile.Write(out)
	if err != nil {
		cleanup()

		return "", common.NewExitError(
			fmt.Sprintf("Could not write output file: %s", err),
			codes.CouldNotWriteOutputFile,
		)
	}

	return tmpfile.Name(), nil
}

// emitTreeContent emits the tree content for editing.
func emitTreeContent(opts editOpts, tree *sops.Tree) ([]byte, error) {
	var out []byte

	var err error

	if opts.ShowMasterKeys {
		out, err = opts.OutputStore.EmitEncryptedFile(*tree)
	} else {
		out, err = opts.OutputStore.EmitPlainFile(tree.Branches)
	}

	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("Could not marshal tree: %s", err),
			codes.ErrorDumpingTree,
		)
	}

	return out, nil
}

// encryptAndEmit encrypts the tree and emits the encrypted file.
func encryptAndEmit(opts editOpts, tree *sops.Tree, dataKey []byte) ([]byte, error) {
	return encryptTreeAndEmit(tree, dataKey, opts.Cipher, opts.OutputStore)
}

// runEditorUntilOk runs the editor in a loop until the file is valid or user cancels.
func runEditorUntilOk(opts runEditorUntilOkOpts) error {
	for {
		err := runEditor(opts.TmpFileName)
		if err != nil {
			return common.NewExitError(
				fmt.Sprintf("Could not run editor: %s", err),
				codes.NoEditorFound,
			)
		}

		valid, err := validateEditedFile(opts)
		if err != nil {
			return err
		}

		if valid {
			break
		}
	}

	return nil
}

// validateEditedFile validates the edited file and updates the tree.
func validateEditedFile(opts runEditorUntilOkOpts) (bool, error) {
	newHash, err := hashFile(opts.TmpFileName)
	if err != nil {
		return false, common.NewExitError(
			fmt.Sprintf("Could not hash file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	if bytes.Equal(newHash, opts.OriginalHash) {
		return false, common.NewExitError(
			"File has not changed, exiting.",
			codes.FileHasNotBeenModified,
		)
	}

	edited, err := os.ReadFile(opts.TmpFileName)
	if err != nil {
		return false, common.NewExitError(
			fmt.Sprintf("Could not read edited file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	return processEditedContent(opts, edited)
}

// processEditedContent processes the edited content and updates the tree.
//
//nolint:nilerr // Returns (false, nil) intentionally to continue editor loop on validation errors
func processEditedContent(opts runEditorUntilOkOpts, edited []byte) (bool, error) {
	newBranches, err := opts.InputStore.LoadPlainFile(edited)
	if err != nil {
		opts.Logger.WithField("error", err).Errorf(
			"Could not load tree, probably due to invalid syntax. " +
				"Press a key to return to the editor, or Ctrl+C to exit.",
		)

		_, _ = bufio.NewReader(os.Stdin).ReadByte()

		return false, nil
	}

	if opts.ShowMasterKeys {
		err := handleMasterKeysMode(opts, edited)
		if err != nil {
			return false, nil
		}
	}

	opts.Tree.Branches = newBranches

	return validateTreeMetadata(opts)
}

// handleMasterKeysMode handles the show master keys mode validation.
func handleMasterKeysMode(opts runEditorUntilOkOpts, edited []byte) error {
	loadedTree, err := opts.InputStore.LoadEncryptedFile(edited)
	if err != nil {
		opts.Logger.WithField("error", err).Errorf(
			"SOPS metadata is invalid. Press a key to return to the editor, or Ctrl+C to exit.",
		)

		_, _ = bufio.NewReader(os.Stdin).ReadByte()

		return fmt.Errorf("failed to load encrypted file: %w", err)
	}

	*opts.Tree = loadedTree

	return nil
}

// validateTreeMetadata validates the tree metadata and updates version if needed.
func validateTreeMetadata(opts runEditorUntilOkOpts) (bool, error) {
	needVersionUpdated, err := version.AIsNewerThanB(version.Version, opts.Tree.Metadata.Version)
	if err != nil {
		return false, common.NewExitError(
			fmt.Sprintf("Failed to compare document version %q with program version %q: %v",
				opts.Tree.Metadata.Version, version.Version, err),
			codes.FailedToCompareVersions,
		)
	}

	if needVersionUpdated {
		opts.Tree.Metadata.Version = version.Version
	}

	if opts.Tree.Metadata.MasterKeyCount() == 0 {
		opts.Logger.Error(
			"No master keys were provided, so sops can't encrypt the file. " +
				"Press a key to return to the editor, or Ctrl+C to exit.",
		)

		_, _ = bufio.NewReader(os.Stdin).ReadByte()

		return false, nil
	}

	return true, nil
}

// hashFile computes the SHA256 hash of a file.
func hashFile(filePath string) ([]byte, error) {
	var result []byte

	file, err := os.Open(filePath) // #nosec G304
	if err != nil {
		return result, fmt.Errorf("failed to open file: %w", err)
	}

	defer func() {
		_ = file.Close()
	}()

	hash := sha256.New()

	_, copyErr := io.Copy(hash, file)
	if copyErr != nil {
		return result, fmt.Errorf("failed to hash file: %w", copyErr)
	}

	return hash.Sum(result), nil
}

// runEditor launches the editor specified by SOPS_EDITOR or EDITOR environment variables.
// Falls back to vim, nano, or vi if no editor is configured.
func runEditor(path string) error {
	cmd, err := createEditorCommand(path)
	if err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	if runErr != nil {
		return fmt.Errorf("editor execution failed: %w", runErr)
	}

	return nil
}

// createEditorCommand creates the exec.Cmd for the editor.
func createEditorCommand(path string) (*exec.Cmd, error) {
	envVar := "SOPS_EDITOR"
	editor := os.Getenv(envVar)

	if editor == "" {
		envVar = "EDITOR"
		editor = os.Getenv(envVar)
	}

	if editor == "" {
		editorPath, err := lookupAnyEditor("vim", "nano", "vi")
		if err != nil {
			return nil, err
		}

		//nolint:noctx // Interactive editor session doesn't benefit from context
		return exec.Command(editorPath, path), nil // #nosec G204
	}

	parts, err := parseEditorCommand(editor, envVar)
	if err != nil {
		return nil, err
	}

	parts = append(parts, path)

	//nolint:noctx,gosec // G204: editor command comes from user-configured environment; required for interactive editing
	return exec.Command(
		parts[0],
		parts[1:]...), nil
}

// parseEditorCommand parses the editor command string.
func parseEditorCommand(editor, envVar string) ([]string, error) {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: $%s is empty", errInvalidEditor, envVar)
	}

	return parts, nil
}

// lookupAnyEditor searches for any of the specified editors in PATH.
func lookupAnyEditor(editorNames ...string) (string, error) {
	for _, editorName := range editorNames {
		editorPath, err := exec.LookPath(editorName)
		if err == nil {
			return editorPath, nil
		}
	}

	return "", fmt.Errorf(
		"%w: sops attempts to use the editor defined in the SOPS_EDITOR "+
			"or EDITOR environment variables, and if that's not set defaults to any of %s, "+
			"but none of them could be found",
		errNoEditorAvailable,
		strings.Join(editorNames, ", "),
	)
}

// NewEditCmd creates and returns the edit command.
func NewEditCmd() *cobra.Command {
	var ignoreMac bool

	var showMasterKeys bool

	var editor string

	cmd := &cobra.Command{
		Use:          "edit <file>",
		Short:        "Edit an encrypted file with SOPS",
		Long:         editCommandLongDescription(),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleEditRunE(cmd, args, ignoreMac, showMasterKeys, editor)
		},
	}

	configureEditFlags(cmd, &ignoreMac, &showMasterKeys, &editor)

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// editCommandLongDescription returns the long description for the edit command.
func editCommandLongDescription() string {
	return `Edit an encrypted file using SOPS (Secrets OPerationS).

If the file exists and is encrypted, it will be decrypted for editing.
If the file does not exist, an example file will be created.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. SOPS_EDITOR or EDITOR environment variables
  4. Fallback to vim, nano, or vi

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault

Example:
  ksail cipher edit secrets.yaml
  ksail cipher edit --editor "code --wait" secrets.yaml
  SOPS_EDITOR="code --wait" ksail cipher edit secrets.yaml`
}

// configureEditFlags configures flags for the edit command.
func configureEditFlags(cmd *cobra.Command, ignoreMac, showMasterKeys *bool, editor *string) {
	cmd.Flags().BoolVar(
		ignoreMac,
		"ignore-mac",
		false,
		"ignore Message Authentication Code during decryption",
	)
	cmd.Flags().BoolVar(
		showMasterKeys,
		"show-master-keys",
		false,
		"show master keys in the editor",
	)
	cmd.Flags().StringVar(
		editor,
		"editor",
		"",
		"editor command to use (e.g., 'code --wait', 'vim', 'nano')",
	)
}

// editNewFile handles editing a new file that doesn't exist yet.
func editNewFile(opts editOpts, inputStore sops.Store) ([]byte, error) {
	storeWithEx, ok := inputStore.(storeWithExample)
	if !ok {
		return nil, fmt.Errorf("%w", errStoreNoExampleGeneration)
	}

	encConfig := encryptConfig{
		KeyGroups:      []sops.KeyGroup{},
		GroupThreshold: 0,
	}

	return editExample(editExampleOpts{
		editOpts:              opts,
		encryptConfig:         encConfig,
		InputStoreWithExample: storeWithEx,
	})
}

// handleEditRunE is the main handler for the edit command.
func handleEditRunE(
	cmd *cobra.Command,
	args []string,
	ignoreMac, showMasterKeys bool,
	editorFlag string,
) error {
	inputPath, inputStore, outputStore, err := canonicalizeAndGetStores(args[0])
	if err != nil {
		return err
	}

	opts := editOpts{
		Cipher:          aes.NewCipher(),
		InputStore:      inputStore,
		OutputStore:     outputStore,
		InputPath:       inputPath,
		IgnoreMAC:       ignoreMac,
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
		ShowMasterKeys:  showMasterKeys,
	}

	// Set up editor environment variables before edit
	cleanup := editor.SetupEditorEnv(cmd, editorFlag, "cipher")
	defer cleanup()

	var output []byte

	// Check if file exists
	_, err = os.Stat(inputPath)
	fileExists := !os.IsNotExist(err)

	if fileExists {
		output, err = edit(opts)
	} else {
		output, err = editNewFile(opts, inputStore)
	}

	if err != nil {
		return fmt.Errorf("edit failed: %w", err)
	}

	// Write the encrypted file
	err = os.WriteFile(inputPath, output, encryptedFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to write encrypted file: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "edited %s",
		Args:    []any{inputPath},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

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
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
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
	inputPath, inputStore, outputStore, err := canonicalizeAndGetStores(args[0])
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

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "encrypted %s",
		Args:    []any{inputPath},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// canonicalizeAndGetStores canonicalizes the input path via EvalCanonicalPath
// and returns the resolved path along with appropriate SOPS stores.
// This prevents symlink-escape attacks by ensuring the actual file path is predictable.
func canonicalizeAndGetStores(inputPath string) (string, sops.Store, sops.Store, error) {
	canonPath, err := fsutil.EvalCanonicalPath(inputPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve input path %q: %w", inputPath, err)
	}

	inputStore, outputStore, err := getStores(canonPath)
	if err != nil {
		return "", nil, nil, err
	}

	return canonPath, inputStore, outputStore, nil
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

var (
	errInvalidAgeKey        = errors.New("invalid age key format")
	errFailedToCreateDir    = errors.New("failed to create directory")
	errFailedToWriteKey     = errors.New("failed to write key")
	errFailedToDetermineAge = errors.New("failed to determine age key path")
)

const (
	ageKeyFilePermissions = 0o600
	ageKeyDirPermissions  = 0o700
	ageKeyPrefix          = "AGE-SECRET-KEY-"
	minAgeKeyLength       = 60
)

// getAgeKeyPath returns the platform-specific path for the age keys file.
// It follows the SOPS convention:
//   - First checks SOPS_AGE_KEY_FILE environment variable
//   - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
//   - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
//   - Windows: %AppData%\sops\age\keys.txt
func getAgeKeyPath() (string, error) {
	p, err := fsutil.SOPSAgeKeyPath()
	if err != nil {
		return "", fmt.Errorf("%w: %w", errFailedToDetermineAge, err)
	}

	return p, nil
}

// validateAgeKey performs basic validation on an age private key string.
// The input must start with "AGE-SECRET-KEY-" and meet minimum length requirements.
func validateAgeKey(privateKey string) error {
	privateKey = strings.TrimSpace(privateKey)

	if privateKey == "" {
		return fmt.Errorf("%w: key is empty", errInvalidAgeKey)
	}

	if !strings.HasPrefix(privateKey, ageKeyPrefix) {
		return fmt.Errorf("%w: key must start with %s", errInvalidAgeKey, ageKeyPrefix)
	}

	if len(privateKey) < minAgeKeyLength {
		return fmt.Errorf(
			"%w: key is too short (minimum %d characters)",
			errInvalidAgeKey,
			minAgeKeyLength,
		)
	}

	return nil
}

// derivePublicKey derives the public key from an age private key.
func derivePublicKey(privateKey string) (string, error) {
	// Parse the private key to get the identity
	identity, err := age.ParseX25519Identity(strings.TrimSpace(privateKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Get the recipient (public key) from the identity
	recipient := identity.Recipient()

	return recipient.String(), nil
}

// formatAgeKeyWithMetadata formats an age private key with metadata comments.
func formatAgeKeyWithMetadata(privateKey, publicKey string) string {
	var builder strings.Builder

	// Add creation timestamp
	builder.WriteString("# created: ")
	builder.WriteString(time.Now().UTC().Format(time.RFC3339))
	builder.WriteString("\n")

	// Add public key if provided
	if publicKey != "" {
		builder.WriteString("# public key: ")
		builder.WriteString(publicKey)
		builder.WriteString("\n")
	}

	// Add the private key
	builder.WriteString(privateKey)

	// Ensure trailing newline
	if !strings.HasSuffix(privateKey, "\n") {
		builder.WriteString("\n")
	}

	return builder.String()
}

// writeKeyToFile writes the formatted key to the target path, either creating a new file or appending to existing one.
func writeKeyToFile(targetPath, formattedKey string) error {
	_, statErr := os.Stat(targetPath)
	if statErr != nil {
		return handleNewFile(targetPath, formattedKey, statErr)
	}

	return appendToExistingFile(targetPath, formattedKey)
}

// handleNewFile creates a new file with the formatted key or returns an error if stat failed for other reasons.
func handleNewFile(targetPath, formattedKey string, statErr error) error {
	if errors.Is(statErr, os.ErrNotExist) {
		// File does not exist yet; create it
		err := os.WriteFile(targetPath, []byte(formattedKey), ageKeyFilePermissions)
		if err != nil {
			return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
		}

		return nil
	}

	// Some other error accessing the file
	return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, statErr)
}

// appendToExistingFile appends the formatted key to an existing file.
func appendToExistingFile(targetPath, formattedKey string) error {
	//#nosec G304 -- targetPath comes from getAgeKeyPath
	file, openErr := os.OpenFile(
		targetPath,
		os.O_APPEND|os.O_WRONLY,
		ageKeyFilePermissions,
	)
	if openErr != nil {
		return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, openErr)
	}

	var err error

	defer func() {
		cerr := file.Close()
		if cerr != nil && err == nil {
			err = fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, cerr)
		}
	}()

	_, err = file.WriteString("\n" + formattedKey)
	if err != nil {
		return fmt.Errorf("%w to %s: %w", errFailedToWriteKey, targetPath, err)
	}

	return nil
}

// importKey imports an age private key and automatically derives the public key.
func importKey(privateKey string) error {
	// Validate the private key
	err := validateAgeKey(privateKey)
	if err != nil {
		return err
	}

	// Derive the public key from the private key
	publicKey, err := derivePublicKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to derive public key: %w", err)
	}

	// Get target path
	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToDetermineAge, err)
	}

	// Create directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)

	err = os.MkdirAll(targetDir, ageKeyDirPermissions)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errFailedToCreateDir, targetDir, err)
	}

	// Format key with metadata
	formattedKey := formatAgeKeyWithMetadata(privateKey, publicKey)

	// Write or append key to file
	return writeKeyToFile(targetPath, formattedKey)
}

// NewImportCmd creates and returns the import command.
func NewImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import PRIVATE_KEY",
		Short: "Import an age key to the system's SOPS key location",
		Long: `Import an age private key to the system's default SOPS age key location.

The private key must be provided as a command argument and must include the full
key with the AGE-SECRET-KEY- prefix.

The public key will be automatically derived from the private key.

The command will automatically add metadata including:
  - Creation timestamp
  - Public key (derived from private key)

Key file location (checked in order):
  1. SOPS_AGE_KEY_FILE environment variable
  2. $XDG_CONFIG_HOME/sops/age/keys.txt (if XDG_CONFIG_HOME is set)
  3. Platform-specific defaults:
     Linux:   $HOME/.config/sops/age/keys.txt
     macOS:   $HOME/Library/Application Support/sops/age/keys.txt
     Windows: %AppData%\sops\age\keys.txt

The private key must be in age format (starting with "AGE-SECRET-KEY-").

Examples:
  # Import a private key (public key will be derived automatically)
  ksail cipher import AGE-SECRET-KEY-1ABCDEF...`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleImportRunE(cmd, args[0])
		},
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	return cmd
}

// handleImportRunE is the main handler for the import command.
func handleImportRunE(cmd *cobra.Command, privateKey string) error {
	err := importKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to import age key: %w", err)
	}

	targetPath, err := getAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToDetermineAge, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "imported age key to %s",
		Args:    []any{targetPath},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// newMockstoreWithExample creates a new instance of mockstoreWithExample. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func newMockstoreWithExample(t interface {
	mock.TestingT
	Cleanup(func())
}) *mockstoreWithExample {
	mock := &mockstoreWithExample{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

// mockstoreWithExample is an autogenerated mock type for the storeWithExample type
type mockstoreWithExample struct {
	mock.Mock
}

type mockstoreWithExample_Expecter struct {
	mock *mock.Mock
}

func (_m *mockstoreWithExample) EXPECT() *mockstoreWithExample_Expecter {
	return &mockstoreWithExample_Expecter{mock: &_m.Mock}
}

// EmitEncryptedFile provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) EmitEncryptedFile(tree sops.Tree) ([]byte, error) {
	ret := _mock.Called(tree)

	if len(ret) == 0 {
		panic("no return value specified for EmitEncryptedFile")
	}

	var r0 []byte
	var r1 error
	if returnFunc, ok := ret.Get(0).(func(sops.Tree) ([]byte, error)); ok {
		return returnFunc(tree)
	}
	if returnFunc, ok := ret.Get(0).(func(sops.Tree) []byte); ok {
		r0 = returnFunc(tree)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}
	if returnFunc, ok := ret.Get(1).(func(sops.Tree) error); ok {
		r1 = returnFunc(tree)
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

// mockstoreWithExample_EmitEncryptedFile_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'EmitEncryptedFile'
type mockstoreWithExample_EmitEncryptedFile_Call struct {
	*mock.Call
}

// EmitEncryptedFile is a helper method to define mock.On call
//   - tree sops.Tree
func (_e *mockstoreWithExample_Expecter) EmitEncryptedFile(tree interface{}) *mockstoreWithExample_EmitEncryptedFile_Call {
	return &mockstoreWithExample_EmitEncryptedFile_Call{Call: _e.mock.On("EmitEncryptedFile", tree)}
}

func (_c *mockstoreWithExample_EmitEncryptedFile_Call) Run(run func(tree sops.Tree)) *mockstoreWithExample_EmitEncryptedFile_Call {
	_c.Call.Run(func(args mock.Arguments) {
		var arg0 sops.Tree
		if args[0] != nil {
			arg0 = args[0].(sops.Tree)
		}
		run(
			arg0,
		)
	})
	return _c
}

func (_c *mockstoreWithExample_EmitEncryptedFile_Call) Return(bytes []byte, err error) *mockstoreWithExample_EmitEncryptedFile_Call {
	_c.Call.Return(bytes, err)
	return _c
}

func (_c *mockstoreWithExample_EmitEncryptedFile_Call) RunAndReturn(run func(tree sops.Tree) ([]byte, error)) *mockstoreWithExample_EmitEncryptedFile_Call {
	_c.Call.Return(run)
	return _c
}

// EmitExample provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) EmitExample() []byte {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for EmitExample")
	}

	var r0 []byte
	if returnFunc, ok := ret.Get(0).(func() []byte); ok {
		r0 = returnFunc()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}
	return r0
}

// mockstoreWithExample_EmitExample_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'EmitExample'
type mockstoreWithExample_EmitExample_Call struct {
	*mock.Call
}

// EmitExample is a helper method to define mock.On call
func (_e *mockstoreWithExample_Expecter) EmitExample() *mockstoreWithExample_EmitExample_Call {
	return &mockstoreWithExample_EmitExample_Call{Call: _e.mock.On("EmitExample")}
}

func (_c *mockstoreWithExample_EmitExample_Call) Run(run func()) *mockstoreWithExample_EmitExample_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *mockstoreWithExample_EmitExample_Call) Return(bytes []byte) *mockstoreWithExample_EmitExample_Call {
	_c.Call.Return(bytes)
	return _c
}

func (_c *mockstoreWithExample_EmitExample_Call) RunAndReturn(run func() []byte) *mockstoreWithExample_EmitExample_Call {
	_c.Call.Return(run)
	return _c
}

// EmitPlainFile provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) EmitPlainFile(treeBranches sops.TreeBranches) ([]byte, error) {
	ret := _mock.Called(treeBranches)

	if len(ret) == 0 {
		panic("no return value specified for EmitPlainFile")
	}

	var r0 []byte
	var r1 error
	if returnFunc, ok := ret.Get(0).(func(sops.TreeBranches) ([]byte, error)); ok {
		return returnFunc(treeBranches)
	}
	if returnFunc, ok := ret.Get(0).(func(sops.TreeBranches) []byte); ok {
		r0 = returnFunc(treeBranches)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}
	if returnFunc, ok := ret.Get(1).(func(sops.TreeBranches) error); ok {
		r1 = returnFunc(treeBranches)
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

// mockstoreWithExample_EmitPlainFile_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'EmitPlainFile'
type mockstoreWithExample_EmitPlainFile_Call struct {
	*mock.Call
}

// EmitPlainFile is a helper method to define mock.On call
//   - treeBranches sops.TreeBranches
func (_e *mockstoreWithExample_Expecter) EmitPlainFile(treeBranches interface{}) *mockstoreWithExample_EmitPlainFile_Call {
	return &mockstoreWithExample_EmitPlainFile_Call{Call: _e.mock.On("EmitPlainFile", treeBranches)}
}

func (_c *mockstoreWithExample_EmitPlainFile_Call) Run(run func(treeBranches sops.TreeBranches)) *mockstoreWithExample_EmitPlainFile_Call {
	_c.Call.Run(func(args mock.Arguments) {
		var arg0 sops.TreeBranches
		if args[0] != nil {
			arg0 = args[0].(sops.TreeBranches)
		}
		run(
			arg0,
		)
	})
	return _c
}

func (_c *mockstoreWithExample_EmitPlainFile_Call) Return(bytes []byte, err error) *mockstoreWithExample_EmitPlainFile_Call {
	_c.Call.Return(bytes, err)
	return _c
}

func (_c *mockstoreWithExample_EmitPlainFile_Call) RunAndReturn(run func(treeBranches sops.TreeBranches) ([]byte, error)) *mockstoreWithExample_EmitPlainFile_Call {
	_c.Call.Return(run)
	return _c
}

// EmitValue provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) EmitValue(ifaceVal interface{}) ([]byte, error) {
	ret := _mock.Called(ifaceVal)

	if len(ret) == 0 {
		panic("no return value specified for EmitValue")
	}

	var r0 []byte
	var r1 error
	if returnFunc, ok := ret.Get(0).(func(interface{}) ([]byte, error)); ok {
		return returnFunc(ifaceVal)
	}
	if returnFunc, ok := ret.Get(0).(func(interface{}) []byte); ok {
		r0 = returnFunc(ifaceVal)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}
	if returnFunc, ok := ret.Get(1).(func(interface{}) error); ok {
		r1 = returnFunc(ifaceVal)
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

// mockstoreWithExample_EmitValue_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'EmitValue'
type mockstoreWithExample_EmitValue_Call struct {
	*mock.Call
}

// EmitValue is a helper method to define mock.On call
//   - ifaceVal interface{}
func (_e *mockstoreWithExample_Expecter) EmitValue(ifaceVal interface{}) *mockstoreWithExample_EmitValue_Call {
	return &mockstoreWithExample_EmitValue_Call{Call: _e.mock.On("EmitValue", ifaceVal)}
}

func (_c *mockstoreWithExample_EmitValue_Call) Run(run func(ifaceVal interface{})) *mockstoreWithExample_EmitValue_Call {
	_c.Call.Run(func(args mock.Arguments) {
		var arg0 interface{}
		if args[0] != nil {
			arg0 = args[0].(interface{})
		}
		run(
			arg0,
		)
	})
	return _c
}

func (_c *mockstoreWithExample_EmitValue_Call) Return(bytes []byte, err error) *mockstoreWithExample_EmitValue_Call {
	_c.Call.Return(bytes, err)
	return _c
}

func (_c *mockstoreWithExample_EmitValue_Call) RunAndReturn(run func(ifaceVal interface{}) ([]byte, error)) *mockstoreWithExample_EmitValue_Call {
	_c.Call.Return(run)
	return _c
}

// HasSopsTopLevelKey provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) HasSopsTopLevelKey(treeBranch sops.TreeBranch) bool {
	ret := _mock.Called(treeBranch)

	if len(ret) == 0 {
		panic("no return value specified for HasSopsTopLevelKey")
	}

	var r0 bool
	if returnFunc, ok := ret.Get(0).(func(sops.TreeBranch) bool); ok {
		r0 = returnFunc(treeBranch)
	} else {
		r0 = ret.Get(0).(bool)
	}
	return r0
}

// mockstoreWithExample_HasSopsTopLevelKey_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'HasSopsTopLevelKey'
type mockstoreWithExample_HasSopsTopLevelKey_Call struct {
	*mock.Call
}

// HasSopsTopLevelKey is a helper method to define mock.On call
//   - treeBranch sops.TreeBranch
func (_e *mockstoreWithExample_Expecter) HasSopsTopLevelKey(treeBranch interface{}) *mockstoreWithExample_HasSopsTopLevelKey_Call {
	return &mockstoreWithExample_HasSopsTopLevelKey_Call{Call: _e.mock.On("HasSopsTopLevelKey", treeBranch)}
}

func (_c *mockstoreWithExample_HasSopsTopLevelKey_Call) Run(run func(treeBranch sops.TreeBranch)) *mockstoreWithExample_HasSopsTopLevelKey_Call {
	_c.Call.Run(func(args mock.Arguments) {
		var arg0 sops.TreeBranch
		if args[0] != nil {
			arg0 = args[0].(sops.TreeBranch)
		}
		run(
			arg0,
		)
	})
	return _c
}

func (_c *mockstoreWithExample_HasSopsTopLevelKey_Call) Return(b bool) *mockstoreWithExample_HasSopsTopLevelKey_Call {
	_c.Call.Return(b)
	return _c
}

func (_c *mockstoreWithExample_HasSopsTopLevelKey_Call) RunAndReturn(run func(treeBranch sops.TreeBranch) bool) *mockstoreWithExample_HasSopsTopLevelKey_Call {
	_c.Call.Return(run)
	return _c
}

// LoadEncryptedFile provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) LoadEncryptedFile(in []byte) (sops.Tree, error) {
	ret := _mock.Called(in)

	if len(ret) == 0 {
		panic("no return value specified for LoadEncryptedFile")
	}

	var r0 sops.Tree
	var r1 error
	if returnFunc, ok := ret.Get(0).(func([]byte) (sops.Tree, error)); ok {
		return returnFunc(in)
	}
	if returnFunc, ok := ret.Get(0).(func([]byte) sops.Tree); ok {
		r0 = returnFunc(in)
	} else {
		r0 = ret.Get(0).(sops.Tree)
	}
	if returnFunc, ok := ret.Get(1).(func([]byte) error); ok {
		r1 = returnFunc(in)
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

// mockstoreWithExample_LoadEncryptedFile_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'LoadEncryptedFile'
type mockstoreWithExample_LoadEncryptedFile_Call struct {
	*mock.Call
}

// LoadEncryptedFile is a helper method to define mock.On call
//   - in []byte
func (_e *mockstoreWithExample_Expecter) LoadEncryptedFile(in interface{}) *mockstoreWithExample_LoadEncryptedFile_Call {
	return &mockstoreWithExample_LoadEncryptedFile_Call{Call: _e.mock.On("LoadEncryptedFile", in)}
}

func (_c *mockstoreWithExample_LoadEncryptedFile_Call) Run(run func(in []byte)) *mockstoreWithExample_LoadEncryptedFile_Call {
	_c.Call.Run(func(args mock.Arguments) {
		var arg0 []byte
		if args[0] != nil {
			arg0 = args[0].([]byte)
		}
		run(
			arg0,
		)
	})
	return _c
}

func (_c *mockstoreWithExample_LoadEncryptedFile_Call) Return(tree sops.Tree, err error) *mockstoreWithExample_LoadEncryptedFile_Call {
	_c.Call.Return(tree, err)
	return _c
}

func (_c *mockstoreWithExample_LoadEncryptedFile_Call) RunAndReturn(run func(in []byte) (sops.Tree, error)) *mockstoreWithExample_LoadEncryptedFile_Call {
	_c.Call.Return(run)
	return _c
}

// LoadPlainFile provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) LoadPlainFile(in []byte) (sops.TreeBranches, error) {
	ret := _mock.Called(in)

	if len(ret) == 0 {
		panic("no return value specified for LoadPlainFile")
	}

	var r0 sops.TreeBranches
	var r1 error
	if returnFunc, ok := ret.Get(0).(func([]byte) (sops.TreeBranches, error)); ok {
		return returnFunc(in)
	}
	if returnFunc, ok := ret.Get(0).(func([]byte) sops.TreeBranches); ok {
		r0 = returnFunc(in)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(sops.TreeBranches)
		}
	}
	if returnFunc, ok := ret.Get(1).(func([]byte) error); ok {
		r1 = returnFunc(in)
	} else {
		r1 = ret.Error(1)
	}
	return r0, r1
}

// mockstoreWithExample_LoadPlainFile_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'LoadPlainFile'
type mockstoreWithExample_LoadPlainFile_Call struct {
	*mock.Call
}

// LoadPlainFile is a helper method to define mock.On call
//   - in []byte
func (_e *mockstoreWithExample_Expecter) LoadPlainFile(in interface{}) *mockstoreWithExample_LoadPlainFile_Call {
	return &mockstoreWithExample_LoadPlainFile_Call{Call: _e.mock.On("LoadPlainFile", in)}
}

func (_c *mockstoreWithExample_LoadPlainFile_Call) Run(run func(in []byte)) *mockstoreWithExample_LoadPlainFile_Call {
	_c.Call.Run(func(args mock.Arguments) {
		var arg0 []byte
		if args[0] != nil {
			arg0 = args[0].([]byte)
		}
		run(
			arg0,
		)
	})
	return _c
}

func (_c *mockstoreWithExample_LoadPlainFile_Call) Return(treeBranches sops.TreeBranches, err error) *mockstoreWithExample_LoadPlainFile_Call {
	_c.Call.Return(treeBranches, err)
	return _c
}

func (_c *mockstoreWithExample_LoadPlainFile_Call) RunAndReturn(run func(in []byte) (sops.TreeBranches, error)) *mockstoreWithExample_LoadPlainFile_Call {
	_c.Call.Return(run)
	return _c
}

// Name provides a mock function for the type mockstoreWithExample
func (_mock *mockstoreWithExample) Name() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for Name")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// mockstoreWithExample_Name_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Name'
type mockstoreWithExample_Name_Call struct {
	*mock.Call
}

// Name is a helper method to define mock.On call
func (_e *mockstoreWithExample_Expecter) Name() *mockstoreWithExample_Name_Call {
	return &mockstoreWithExample_Name_Call{Call: _e.mock.On("Name")}
}

func (_c *mockstoreWithExample_Name_Call) Run(run func()) *mockstoreWithExample_Name_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *mockstoreWithExample_Name_Call) Return(s string) *mockstoreWithExample_Name_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *mockstoreWithExample_Name_Call) RunAndReturn(run func() string) *mockstoreWithExample_Name_Call {
	_c.Call.Return(run)
	return _c
}
