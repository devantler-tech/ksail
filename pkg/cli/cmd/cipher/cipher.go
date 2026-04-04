package cipher

import (
	"fmt"
	"os"

	sopsclient "github.com/devantler-tech/ksail/v5/pkg/client/sops"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/keyservice"
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
		return err
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
		return err
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
	_, err = os.Stat(inputPath)
	fileExists := !os.IsNotExist(err)

	if fileExists {
		output, err = sopsclient.Edit(opts)
	} else {
		output, err = sopsclient.EditNewFile(opts, inputStore)
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
		return err
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
