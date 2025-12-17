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

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/version"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

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

	//nolint:noctx // Interactive editor session doesn't benefit from context
	return exec.Command(parts[0], parts[1:]...), nil // #nosec G204
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

	cmd := &cobra.Command{
		Use:   "edit <file>",
		Short: "Edit an encrypted file with SOPS",
		Long: `Edit an encrypted file using SOPS (Secrets OPerationS).

If the file exists and is encrypted, it will be decrypted for editing.
If the file does not exist, an example file will be created.

The editor is determined by the SOPS_EDITOR or EDITOR environment variables.
If neither is set, the command will try vim, nano, or vi in that order.

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault

Example:
  ksail cipher edit secrets.yaml
  SOPS_EDITOR="code --wait" ksail cipher edit secrets.yaml`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleEditRunE(cmd, args, ignoreMac, showMasterKeys)
		},
	}

	cmd.Flags().BoolVar(
		&ignoreMac,
		"ignore-mac",
		false,
		"ignore Message Authentication Code during decryption",
	)
	cmd.Flags().BoolVar(
		&showMasterKeys,
		"show-master-keys",
		false,
		"show master keys in the editor",
	)

	return cmd
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
func handleEditRunE(cmd *cobra.Command, args []string, ignoreMac, showMasterKeys bool) error {
	inputPath := args[0]

	inputStore, outputStore, err := getStores(inputPath)
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

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Successfully edited %s\n", inputPath)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}
