package sops

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/cmd/sops/codes"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/version"
	"github.com/sirupsen/logrus"
)

// EditExample creates and edits an example file when the target file doesn't exist.
func EditExample(opts EditExampleOpts) ([]byte, error) {
	fileBytes := opts.InputStoreWithExample.EmitExample()

	branches, err := opts.InputStoreWithExample.LoadPlainFile(fileBytes)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("Error unmarshalling file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	tree, err := CreateSOPSTree(branches, opts.EncryptConfig, opts.InputPath)
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

	return EditTree(opts.EditOpts, tree, dataKey)
}

// Edit loads, decrypts, and allows editing of an existing encrypted file.
func Edit(opts EditOpts) ([]byte, error) {
	// Convert EditOpts to DecryptOpts for decryption
	decOpts := DecryptOpts{
		Cipher:          opts.Cipher,
		InputStore:      opts.InputStore,
		OutputStore:     opts.OutputStore,
		InputPath:       opts.InputPath,
		ReadFromStdin:   false,
		IgnoreMAC:       opts.IgnoreMAC,
		KeyServices:     opts.KeyServices,
		DecryptionOrder: opts.DecryptionOrder,
	}

	tree, dataKey, err := DecryptTreeWithKey(decOpts)
	if err != nil {
		return nil, err
	}

	return EditTree(opts, tree, dataKey)
}

// EditTree handles the core edit workflow: write to temp file, launch editor, re-encrypt.
func EditTree(opts EditOpts, tree *sops.Tree, dataKey []byte) ([]byte, error) {
	tmpfileName, cleanupFn, err := CreateTempFileWithContent(opts, tree)
	if err != nil {
		return nil, err
	}
	defer cleanupFn()

	origHash, err := HashFile(tmpfileName)
	if err != nil {
		return nil, common.NewExitError(
			fmt.Sprintf("Could not hash file: %s", err),
			codes.CouldNotReadInputFile,
		)
	}

	logger := logrus.New()

	err = RunEditorUntilOk(RunEditorUntilOkOpts{
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

	return EncryptAndEmit(opts, tree, dataKey)
}

// CreateTempFileWithContent creates a temporary file with the tree content.
func CreateTempFileWithContent(opts EditOpts, tree *sops.Tree) (string, func(), error) {
	tmpdir, cleanup, err := CreateTempDir()
	if err != nil {
		return "", nil, err
	}

	tmpfileName, err := WriteTempFile(tmpdir, opts, tree, cleanup)
	if err != nil {
		return "", nil, err
	}

	return tmpfileName, cleanup, nil
}

// CreateTempDir creates a temporary directory for editing.
func CreateTempDir() (string, func(), error) {
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

// WriteTempFile writes the tree content to a temporary file.
func WriteTempFile(tmpdir string, opts EditOpts, tree *sops.Tree, cleanup func()) (string, error) {
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

	chmodErr := tmpfile.Chmod(TmpFilePermissions)
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

	out, err := EmitTreeContent(opts, tree)
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

// EmitTreeContent emits the tree content for editing.
func EmitTreeContent(opts EditOpts, tree *sops.Tree) ([]byte, error) {
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

// EncryptAndEmit encrypts the tree and emits the encrypted file.
func EncryptAndEmit(opts EditOpts, tree *sops.Tree, dataKey []byte) ([]byte, error) {
	return EncryptTreeAndEmit(tree, dataKey, opts.Cipher, opts.OutputStore)
}

// RunEditorUntilOk runs the editor in a loop until the file is valid or user cancels.
func RunEditorUntilOk(opts RunEditorUntilOkOpts) error {
	for {
		err := RunEditor(opts.TmpFileName)
		if err != nil {
			return common.NewExitError(
				fmt.Sprintf("Could not run editor: %s", err),
				codes.NoEditorFound,
			)
		}

		valid, err := ValidateEditedFile(opts)
		if err != nil {
			return err
		}

		if valid {
			break
		}
	}

	return nil
}

// ValidateEditedFile validates the edited file and updates the tree.
func ValidateEditedFile(opts RunEditorUntilOkOpts) (bool, error) {
	newHash, err := HashFile(opts.TmpFileName)
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

	return ProcessEditedContent(opts, edited)
}

// ProcessEditedContent processes the edited content and updates the tree.
//
//nolint:nilerr // Returns (false, nil) intentionally to continue editor loop on validation errors
func ProcessEditedContent(opts RunEditorUntilOkOpts, edited []byte) (bool, error) {
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
		err := HandleMasterKeysMode(opts, edited)
		if err != nil {
			return false, nil
		}
	}

	opts.Tree.Branches = newBranches

	return ValidateTreeMetadata(opts)
}

// HandleMasterKeysMode handles the show master keys mode validation.
func HandleMasterKeysMode(opts RunEditorUntilOkOpts, edited []byte) error {
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

// ValidateTreeMetadata validates the tree metadata and updates version if needed.
func ValidateTreeMetadata(opts RunEditorUntilOkOpts) (bool, error) {
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

// EditNewFile handles editing a new file that doesn't exist yet.
func EditNewFile(opts EditOpts, inputStore sops.Store) ([]byte, error) {
	storeWithEx, ok := inputStore.(StoreWithExample)
	if !ok {
		return nil, fmt.Errorf("%w", ErrStoreNoExampleGeneration)
	}

	encConfig := EncryptConfig{
		KeyGroups:      []sops.KeyGroup{},
		GroupThreshold: 0,
	}

	return EditExample(EditExampleOpts{
		EditOpts:              opts,
		EncryptConfig:         encConfig,
		InputStoreWithExample: storeWithEx,
	})
}

// HashFile computes the SHA256 hash of a file.
func HashFile(filePath string) ([]byte, error) {
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

// RunEditor launches the editor specified by SOPS_EDITOR or EDITOR environment variables.
// Falls back to vim, nano, or vi if no editor is configured.
func RunEditor(path string) error {
	cmd, err := CreateEditorCommand(path)
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

// CreateEditorCommand creates the exec.Cmd for the editor.
func CreateEditorCommand(path string) (*exec.Cmd, error) {
	envVar := "SOPS_EDITOR"
	editor := os.Getenv(envVar)

	if editor == "" {
		envVar = "EDITOR"
		editor = os.Getenv(envVar)
	}

	if editor == "" {
		editorPath, err := LookupAnyEditor("vim", "nano", "vi")
		if err != nil {
			return nil, err
		}

		//nolint:noctx // Interactive editor session doesn't benefit from context
		return exec.Command(editorPath, path), nil // #nosec G204
	}

	parts, err := ParseEditorCommand(editor, envVar)
	if err != nil {
		return nil, err
	}

	parts = append(parts, path)

	//nolint:noctx,gosec // G204: editor command comes from user-configured environment; required for interactive editing
	return exec.Command(
		parts[0],
		parts[1:]...), nil
}

// ParseEditorCommand parses the editor command string.
func ParseEditorCommand(editor, envVar string) ([]string, error) {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: $%s is empty", ErrInvalidEditor, envVar)
	}

	return parts, nil
}

// LookupAnyEditor searches for any of the specified editors in PATH.
func LookupAnyEditor(editorNames ...string) (string, error) {
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
		ErrNoEditorAvailable,
		strings.Join(editorNames, ", "),
	)
}
