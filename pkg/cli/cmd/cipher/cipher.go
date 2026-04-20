package cipher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/keys"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/spf13/cobra"
)

var (
	errRotationFailed    = errors.New("rotation failed")
	errRotationCancelled = errors.New("rotation cancelled")
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
	cmd.AddCommand(NewRotateCmd())

	return cmd
}

// NewDecryptCmd creates and returns the decrypt command.
func NewDecryptCmd() *cobra.Command {
	var (
		extract   string
		ignoreMac bool
		output    string
	)

	cmd := &cobra.Command{
		Use:   "decrypt [file]",
		Short: "Decrypt a file with SOPS",
		Long: `Decrypt a file using SOPS (Secrets OPerationS).

If no file is given, input is read from stdin.

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
  ksail cipher decrypt secrets.yaml --ignore-mac
  cat secrets.enc.yaml | ksail cipher decrypt`,
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

// handleDecryptRunE is the main handler for the decrypt command.
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

		canonPath, err := fsutil.EvalCanonicalPath(inputPath)
		if err != nil {
			return fmt.Errorf("resolve input path %q: %w", inputPath, err)
		}

		inputPath = canonPath
	}

	inputStore, outputStore, err := sopsclient.GetDecryptStores(inputPath, readFromStdin)
	if err != nil {
		return fmt.Errorf("failed to get decrypt stores: %w", err)
	}

	var extractPath []any

	if extract != "" {
		extractPath, err = sopsclient.ParseExtractPath(extract)
		if err != nil {
			return fmt.Errorf("failed to parse extract path: %w", err)
		}
	}

	opts := sopsclient.DecryptOpts{
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

	decryptedData, err := sopsclient.Decrypt(opts)
	if err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}

	return writeDecryptedOutput(cmd, decryptedData, output)
}

// writeDecryptedOutput writes decrypted data to either a file or stdout.
func writeDecryptedOutput(cmd *cobra.Command, data []byte, outputPath string) error {
	if outputPath != "" {
		canonOutput, err := fsutil.EvalCanonicalPath(outputPath)
		if err != nil {
			return fmt.Errorf("resolve output path %q: %w", outputPath, err)
		}

		err = os.WriteFile(canonOutput, data, sopsclient.DecryptedFilePermissions)
		if err != nil {
			return fmt.Errorf("failed to write decrypted file: %w", err)
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "decrypted to %s",
			Args:    []any{canonOutput},
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

// NewEditCmd creates and returns the edit command.
func NewEditCmd() *cobra.Command {
	var ignoreMac bool

	var showMasterKeys bool

	var editorStr string

	cmd := &cobra.Command{
		Use:          "edit <file>",
		Short:        "Edit an encrypted file with SOPS",
		Long:         editCommandLongDescription(),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleEditRunE(cmd, args, ignoreMac, showMasterKeys, editorStr)
		},
	}

	configureEditFlags(cmd, &ignoreMac, &showMasterKeys, &editorStr)

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// handleEditRunE is the main handler for the edit command.
func handleEditRunE(
	cmd *cobra.Command,
	args []string,
	ignoreMac, showMasterKeys bool,
	editorFlag string,
) error {
	inputPath, inputStore, outputStore, err := sopsclient.CanonicalizeAndGetStores(args[0])
	if err != nil {
		return fmt.Errorf("failed to get stores for edit: %w", err)
	}

	opts := sopsclient.EditOpts{
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
	_, statErr := os.Stat(inputPath)
	switch {
	case statErr == nil:
		output, err = sopsclient.Edit(opts)
	case os.IsNotExist(statErr):
		output, err = sopsclient.EditNewFile(opts, inputStore)
	default:
		return fmt.Errorf("failed to stat input file: %w", statErr)
	}

	if err != nil {
		return fmt.Errorf("edit failed: %w", err)
	}

	// Write the encrypted file
	err = os.WriteFile(inputPath, output, sopsclient.EncryptedFilePermissions)
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
func configureEditFlags(cmd *cobra.Command, ignoreMac, showMasterKeys *bool, editorStr *string) {
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
		editorStr,
		"editor",
		"",
		"editor command to use (e.g., 'code --wait', 'vim', 'nano')",
	)
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

// handleEncryptRunE is the main handler for the encrypt command.
func handleEncryptRunE(cmd *cobra.Command, args []string) error {
	inputPath, inputStore, outputStore, err := sopsclient.CanonicalizeAndGetStores(args[0])
	if err != nil {
		return fmt.Errorf("failed to get stores for encrypt: %w", err)
	}

	opts := sopsclient.EncryptOpts{
		EncryptConfig: sopsclient.EncryptConfig{
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

	encryptedData, err := sopsclient.Encrypt(opts)
	if err != nil {
		return fmt.Errorf("encryption failed: %w", err)
	}

	err = os.WriteFile(inputPath, encryptedData, sopsclient.EncryptedFilePermissions)
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
	err := sopsclient.ImportKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to import age key: %w", err)
	}

	targetPath, err := sopsclient.GetAgeKeyPath()
	if err != nil {
		return fmt.Errorf("%w: %w", sopsclient.ErrFailedToDetermineAge, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "imported age key to %s",
		Args:    []any{targetPath},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

const rotateCmdLong = `Rotate data keys for SOPS-encrypted files.

This command generates a new data encryption key and re-encrypts all values
in the target file(s). This is the same behavior as the native 'sops rotate'
command, extended with batch directory support.

When the target is a file, only that file is rotated. When the target is a
folder, all SOPS-encrypted YAML and JSON files in the folder are rotated.
Use --recursive to include subdirectories.

Optionally, master key recipients can be added or removed during rotation:
  --add-key adds a new master key recipient
  --remove-key removes an existing master key recipient

By default, the command shows which files will be affected and prompts for
confirmation. Use --force to skip the confirmation prompt. In non-interactive
environments (no TTY), the prompt is automatically skipped.

Key type is auto-detected from the key format:
  - Age keys (age1...)

Examples:
  # Rotate all encrypted files in a folder (with confirmation)
  ksail cipher rotate ./k8s

  # Rotate without confirmation prompt
  ksail cipher rotate ./k8s --force

  # Rotate recursively through subdirectories
  ksail cipher rotate ./k8s --recursive

  # Rotate a single file
  ksail cipher rotate secrets.yaml

  # Add a new age recipient during rotation
  ksail cipher rotate ./k8s --add-key age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p

  # Remove an old age recipient during rotation
  ksail cipher rotate ./k8s --remove-key age1oldkey...

  # Replace a recipient (add new, remove old)
  ksail cipher rotate ./k8s --add-key age1newkey... --remove-key age1oldkey...`

// NewRotateCmd creates and returns the rotate command.
func NewRotateCmd() *cobra.Command {
	var (
		addKey    string
		removeKey string
		recursive bool
		force     bool
	)

	cmd := &cobra.Command{
		Use:          "rotate <file/folder>",
		Short:        "Rotate data keys for SOPS-encrypted files",
		Long:         rotateCmdLong,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return handleRotateRunE(cmd, args[0], addKey, removeKey, recursive, force)
		},
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().StringVar(&addKey, "add-key", "", "public key to add as a master key recipient")
	cmd.Flags().
		StringVar(&removeKey, "remove-key", "", "public key to remove from master key recipients")
	cmd.Flags().
		BoolVarP(&recursive, "recursive", "r", false, "scan subdirectories when target is a folder")
	cmd.Flags().
		BoolVarP(&force, "force", "f", false, "skip confirmation prompt and rotate immediately")

	return cmd
}

// handleRotateRunE is the main handler for the rotate command.
func handleRotateRunE(
	cmd *cobra.Command,
	target, addKey, removeKey string,
	recursive, force bool,
) error {
	writer := cmd.OutOrStdout()

	canonPath, err := fsutil.EvalCanonicalPath(target)
	if err != nil {
		return fmt.Errorf("resolve target path %q: %w", target, err)
	}

	opts, err := buildRotateOpts(addKey, removeKey)
	if err != nil {
		return err
	}

	files, isDir, err := collectRotateTargets(canonPath, recursive, writer)
	if err != nil {
		return err
	}

	if files == nil {
		return nil
	}

	if !confirm.ShouldSkipPrompt(force) {
		showRotatePreview(writer, files, canonPath, isDir)

		if !confirm.PromptForConfirmation(writer) {
			return errRotationCancelled
		}
	}

	return handleRotateApply(writer, files, opts)
}

// collectRotateTargets resolves the target path into a list of encrypted files.
// Returns nil (not empty) when no files are found and a message has been printed.
// The bool return indicates whether the target was a directory.
func collectRotateTargets(
	canonPath string,
	recursive bool,
	writer io.Writer,
) ([]string, bool, error) {
	info, err := os.Stat(canonPath)
	if err != nil {
		return nil, false, fmt.Errorf("stat %q: %w", canonPath, err)
	}

	if info.IsDir() {
		files, findErr := sopsclient.FindEncryptedFiles(canonPath, recursive)
		if findErr != nil {
			return nil, true, fmt.Errorf("finding encrypted files: %w", findErr)
		}

		if len(files) == 0 {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "no SOPS-encrypted files found in %s",
				Args:    []any{canonPath},
				Writer:  writer,
			})

			return nil, true, nil
		}

		return files, true, nil
	}

	// For explicitly-targeted single files, validate format before checking encryption.
	_, _, storeErr := sopsclient.GetStores(canonPath)
	if storeErr != nil {
		return nil, false, fmt.Errorf("unsupported file format for %q: %w", canonPath, storeErr)
	}

	encrypted, encErr := sopsclient.IsFileEncrypted(canonPath)
	if encErr != nil {
		return nil, false, fmt.Errorf("checking file %q: %w", canonPath, encErr)
	}

	if !encrypted {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "file %s is not SOPS-encrypted, skipping",
			Args:    []any{canonPath},
			Writer:  writer,
		})

		return nil, false, nil
	}

	return []string{canonPath}, false, nil
}

// buildRotateOpts constructs RotateOpts from CLI flag values.
func buildRotateOpts(addKey, removeKey string) (sopsclient.RotateOpts, error) {
	opts := sopsclient.RotateOpts{
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	if addKey != "" {
		masterKey, err := sopsclient.ParseKeyType(addKey)
		if err != nil {
			return opts, fmt.Errorf("parsing --add-key: %w", err)
		}

		opts.AddKeys = []keys.MasterKey{masterKey}
	}

	if removeKey != "" {
		opts.RemoveKeys = []string{removeKey}
	}

	return opts, nil
}

// showRotatePreview prints a summary of which files will be rotated and prompts for confirmation.
func showRotatePreview(writer io.Writer, files []string, scanPath string, isDir bool) {
	if isDir {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "the following %d file(s) in %s will be rotated:",
			Args:    []any{len(files), scanPath},
			Writer:  writer,
		})
	} else {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "the following file will be rotated:",
			Args:    []any{},
			Writer:  writer,
		})
	}

	for _, file := range files {
		_, _ = fmt.Fprintf(writer, "  %s\n", file)
	}

	_, _ = fmt.Fprint(writer, `Type "yes" to confirm rotation: `)
}

// handleRotateApply performs the actual key rotation on all files.
func handleRotateApply(writer io.Writer, files []string, opts sopsclient.RotateOpts) error {
	rotated := 0

	var rotateErrors []string

	for _, file := range files {
		err := sopsclient.RotateFile(file, opts)
		if err != nil {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: "  failed %s: %v",
				Args:    []any{file, err},
				Writer:  writer,
			})

			rotateErrors = append(rotateErrors, file)

			continue
		}

		rotated++

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "rotated %s",
			Args:    []any{file},
			Writer:  writer,
		})
	}

	if len(rotateErrors) > 0 {
		return fmt.Errorf(
			"%w for %d file(s): %s",
			errRotationFailed,
			len(rotateErrors),
			strings.Join(rotateErrors, ", "),
		)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "rotated %d file(s)",
		Args:    []any{rotated},
		Writer:  writer,
	})

	return nil
}
