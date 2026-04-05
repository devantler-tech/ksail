package sops

import (
	"errors"
	"os"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/sirupsen/logrus"
)

// DecryptOpts contains all options needed for the decryption operation.
type DecryptOpts struct {
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

// EncryptConfig holds configuration options for SOPS encryption.
// It defines patterns for which values should be encrypted/unencrypted,
// key groups for encryption, and Shamir secret sharing threshold.
type EncryptConfig struct {
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

// EncryptOpts contains all options needed for the encryption operation.
// It combines encryption configuration with runtime parameters like cipher,
// stores, and key services.
type EncryptOpts struct {
	EncryptConfig

	Cipher        sops.Cipher
	InputStore    sops.Store
	OutputStore   sops.Store
	InputPath     string
	ReadFromStdin bool
	KeyServices   []keyservice.KeyServiceClient
}

// EditOpts contains all options needed for the edit operation.
type EditOpts struct {
	Cipher          sops.Cipher
	InputStore      sops.Store
	OutputStore     sops.Store
	InputPath       string
	IgnoreMAC       bool
	KeyServices     []keyservice.KeyServiceClient
	DecryptionOrder []string
	ShowMasterKeys  bool
}

// EditExampleOpts combines EditOpts with encryption configuration
// for creating and editing example files.
type EditExampleOpts struct {
	EditOpts

	EncryptConfig

	InputStoreWithExample StoreWithExample
}

// RunEditorUntilOkOpts contains options for the editor loop.
type RunEditorUntilOkOpts struct {
	TmpFileName    string
	OriginalHash   []byte
	InputStore     sops.Store
	ShowMasterKeys bool
	Tree           *sops.Tree
	Logger         *logrus.Logger
}

// StoreWithExample is an interface for stores that can emit example files.
type StoreWithExample interface {
	sops.Store
	EmitExample() []byte
}

// FileAlreadyEncryptedError indicates that a file already contains SOPS metadata
// and cannot be re-encrypted without first decrypting it.
type FileAlreadyEncryptedError struct{}

func (err *FileAlreadyEncryptedError) Error() string {
	return "file already encrypted"
}

// NotBinaryHint provides a user-friendly message when SOPS encounters binary data.
const NotBinaryHint = "This is likely not an encrypted binary file."

const (
	// TmpFilePermissions is the permission mode for temporary files during edit.
	TmpFilePermissions = os.FileMode(0o600)
	// EncryptedFilePermissions is the permission mode for encrypted output files.
	EncryptedFilePermissions = os.FileMode(0o644)
	// DecryptedFilePermissions is the permission mode for decrypted output files.
	DecryptedFilePermissions = os.FileMode(0o600)
	// AgeKeyFilePermissions is the permission mode for age key files.
	AgeKeyFilePermissions = os.FileMode(0o600)
	// AgeKeyDirPermissions is the permission mode for the age key directory.
	AgeKeyDirPermissions = os.FileMode(0o700)
	// AgeKeyPrefix is the expected prefix for age private keys.
	AgeKeyPrefix = "AGE-SECRET-KEY-"
	// MinAgeKeyLength is the minimum required length for age private keys.
	MinAgeKeyLength = 60
)

// Exported error variables for SOPS operations.
var (
	ErrDumpingTree              = errors.New("error dumping file")
	ErrInvalidExtractPath       = errors.New("invalid extract path format")
	ErrInvalidEditor            = errors.New("invalid editor configuration")
	ErrNoEditorAvailable        = errors.New("no editor available")
	ErrStoreNoExampleGeneration = errors.New("store does not support example file generation")
	ErrCouldNotGenerateDataKey  = errors.New("could not generate data key")
	ErrUnsupportedFileFormat    = errors.New("unsupported file format")
	ErrInvalidAgeKey            = errors.New("invalid age key format")
	ErrFailedToCreateDir        = errors.New("failed to create directory")
	ErrFailedToWriteKey         = errors.New("failed to write key")
	ErrFailedToDetermineAge     = errors.New("failed to determine age key path")
)
