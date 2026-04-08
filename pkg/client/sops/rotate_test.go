package sops_test

import (
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	sopsclient "github.com/devantler-tech/ksail/v5/pkg/client/sops"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keys"
	"github.com/getsops/sops/v3/keyservice"
)

// setupAgeKey generates an age identity and configures the SOPS env for tests.
func setupAgeKey(t *testing.T) (*age.X25519Identity, []sops.KeyGroup) {
	t.Helper()

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age identity: %v", err)
	}

	keyFile := filepath.Join(t.TempDir(), "keys.txt")

	err = os.WriteFile(keyFile, []byte(identity.String()+"\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	masterKey, err := sopsage.MasterKeyFromRecipient(identity.Recipient().String())
	if err != nil {
		t.Fatalf("failed to create age master key: %v", err)
	}

	return identity, []sops.KeyGroup{{masterKey}}
}

// encryptTestFile creates a plaintext YAML file, encrypts it in-place, and
// returns the file path.
func encryptTestFile(t *testing.T, dir, name string, keyGroups []sops.KeyGroup) string {
	t.Helper()

	content := []byte("password: super-secret\n")
	filePath := filepath.Join(dir, name)

	err := os.WriteFile(filePath, content, 0o600)
	if err != nil {
		t.Fatalf("write plaintext: %v", err)
	}

	inputStore, outputStore, err := sopsclient.GetStores(filePath)
	if err != nil {
		t.Fatalf("get stores: %v", err)
	}

	opts := sopsclient.EncryptOpts{
		EncryptConfig: sopsclient.EncryptConfig{
			KeyGroups:      keyGroups,
			GroupThreshold: 0,
		},
		Cipher:      aes.NewCipher(),
		InputStore:  inputStore,
		OutputStore: outputStore,
		InputPath:   filePath,
		KeyServices: []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	}

	encrypted, err := sopsclient.Encrypt(opts)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	err = os.WriteFile(filePath, encrypted, 0o644)
	if err != nil {
		t.Fatalf("write encrypted: %v", err)
	}

	return filePath
}

func TestFindEncryptedFiles_Recursive(t *testing.T) {
	_, keyGroups := setupAgeKey(t)

	dir := t.TempDir()

	// Encrypted files at root and in subdirectory
	encryptTestFile(t, dir, "root.yaml", keyGroups)

	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	encryptTestFile(t, subDir, "nested.yaml", keyGroups)

	// Plain (non-encrypted) file
	err := os.WriteFile(filepath.Join(dir, "plain.yaml"), []byte("key: value\n"), 0o600)
	if err != nil {
		t.Fatalf("write plain: %v", err)
	}

	// Non-YAML file
	err = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello\n"), 0o600)
	if err != nil {
		t.Fatalf("write txt: %v", err)
	}

	files, err := sopsclient.FindEncryptedFiles(dir, true)
	if err != nil {
		t.Fatalf("FindEncryptedFiles: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 encrypted files, got %d: %v", len(files), files)
	}
}

func TestFindEncryptedFiles_Flat(t *testing.T) {
	_, keyGroups := setupAgeKey(t)

	dir := t.TempDir()

	encryptTestFile(t, dir, "root.yaml", keyGroups)

	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	encryptTestFile(t, subDir, "nested.yaml", keyGroups)

	files, err := sopsclient.FindEncryptedFiles(dir, false)
	if err != nil {
		t.Fatalf("FindEncryptedFiles: %v", err)
	}

	// Flat mode should only find root.yaml, not nested.yaml
	if len(files) != 1 {
		t.Fatalf("expected 1 encrypted file (flat mode), got %d: %v", len(files), files)
	}
}

func TestFindEncryptedFiles_SkipsHiddenDirs(t *testing.T) {
	_, keyGroups := setupAgeKey(t)

	dir := t.TempDir()

	hiddenDir := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	encryptTestFile(t, hiddenDir, "secret.yaml", keyGroups)

	files, err := sopsclient.FindEncryptedFiles(dir, true)
	if err != nil {
		t.Fatalf("FindEncryptedFiles: %v", err)
	}

	if len(files) != 0 {
		t.Fatalf("expected 0 files (hidden dir), got %d: %v", len(files), files)
	}
}

func TestRotateFile(t *testing.T) {
	_, keyGroups := setupAgeKey(t)

	dir := t.TempDir()
	filePath := encryptTestFile(t, dir, "secret.yaml", keyGroups)

	// Read original encrypted content
	originalData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	opts := sopsclient.RotateOpts{
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	err = sopsclient.RotateFile(filePath, opts)
	if err != nil {
		t.Fatalf("RotateFile: %v", err)
	}

	// Verify file was modified (new data key means different ciphertext)
	rotatedData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read rotated: %v", err)
	}

	if string(originalData) == string(rotatedData) {
		t.Error("expected rotated file to differ from original")
	}

	// Verify the file is still valid SOPS-encrypted and can be decrypted
	inputStore, outputStore, err := sopsclient.GetDecryptStores(filePath, false)
	if err != nil {
		t.Fatalf("get decrypt stores: %v", err)
	}

	decOpts := sopsclient.DecryptOpts{
		Cipher:          aes.NewCipher(),
		InputStore:      inputStore,
		OutputStore:     outputStore,
		InputPath:       filePath,
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	decrypted, err := sopsclient.Decrypt(decOpts)
	if err != nil {
		t.Fatalf("Decrypt after rotate: %v", err)
	}

	if len(decrypted) == 0 {
		t.Error("decrypted data should not be empty")
	}
}

func TestRotateFile_AddKey(t *testing.T) {
	_, keyGroups := setupAgeKey(t)

	// Generate a second age identity to add
	identity2, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate second identity: %v", err)
	}

	newMasterKey, err := sopsage.MasterKeyFromRecipient(identity2.Recipient().String())
	if err != nil {
		t.Fatalf("create master key from second identity: %v", err)
	}

	dir := t.TempDir()
	filePath := encryptTestFile(t, dir, "secret.yaml", keyGroups)

	opts := sopsclient.RotateOpts{
		AddKeys:         []keys.MasterKey{newMasterKey},
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	err = sopsclient.RotateFile(filePath, opts)
	if err != nil {
		t.Fatalf("RotateFile with AddKeys: %v", err)
	}

	// Verify the file can be decrypted with the second key
	keyFile2 := filepath.Join(t.TempDir(), "keys2.txt")

	err = os.WriteFile(keyFile2, []byte(identity2.String()+"\n"), 0o600)
	if err != nil {
		t.Fatalf("write key file 2: %v", err)
	}

	t.Setenv("SOPS_AGE_KEY_FILE", keyFile2)

	inputStore, outputStore, err := sopsclient.GetDecryptStores(filePath, false)
	if err != nil {
		t.Fatalf("get decrypt stores: %v", err)
	}

	decOpts := sopsclient.DecryptOpts{
		Cipher:          aes.NewCipher(),
		InputStore:      inputStore,
		OutputStore:     outputStore,
		InputPath:       filePath,
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	decrypted, err := sopsclient.Decrypt(decOpts)
	if err != nil {
		t.Fatalf("Decrypt with new key: %v", err)
	}

	if len(decrypted) == 0 {
		t.Error("decrypted data should not be empty")
	}
}

func TestRotateFile_RemoveKey(t *testing.T) {
	// Create two identities and encrypt with both
	identity1, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity1: %v", err)
	}

	identity2, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity2: %v", err)
	}

	// Write identity1 for decryption during rotate
	keyFile := filepath.Join(t.TempDir(), "keys.txt")

	err = os.WriteFile(keyFile, []byte(identity1.String()+"\n"), 0o600)
	if err != nil {
		t.Fatalf("write key file: %v", err)
	}

	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	masterKey1, err := sopsage.MasterKeyFromRecipient(identity1.Recipient().String())
	if err != nil {
		t.Fatalf("create master key 1: %v", err)
	}

	masterKey2, err := sopsage.MasterKeyFromRecipient(identity2.Recipient().String())
	if err != nil {
		t.Fatalf("create master key 2: %v", err)
	}

	keyGroups := []sops.KeyGroup{{masterKey1, masterKey2}}

	dir := t.TempDir()
	filePath := encryptTestFile(t, dir, "secret.yaml", keyGroups)

	// Remove identity2's public key
	opts := sopsclient.RotateOpts{
		RemoveKeys:      []string{identity2.Recipient().String()},
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	err = sopsclient.RotateFile(filePath, opts)
	if err != nil {
		t.Fatalf("RotateFile with RemoveKeys: %v", err)
	}

	// Verify the file can still be decrypted with identity1
	inputStore, outputStore, err := sopsclient.GetDecryptStores(filePath, false)
	if err != nil {
		t.Fatalf("get decrypt stores: %v", err)
	}

	decOpts := sopsclient.DecryptOpts{
		Cipher:          aes.NewCipher(),
		InputStore:      inputStore,
		OutputStore:     outputStore,
		InputPath:       filePath,
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}

	_, err = sopsclient.Decrypt(decOpts)
	if err != nil {
		t.Fatalf("Decrypt with remaining key: %v", err)
	}
}

func TestParseKeyType_Age(t *testing.T) {
	t.Parallel()

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	key, err := sopsclient.ParseKeyType(identity.Recipient().String())
	if err != nil {
		t.Fatalf("ParseKeyType: %v", err)
	}

	if key == nil {
		t.Fatal("expected non-nil key")
	}

	if key.ToString() != identity.Recipient().String() {
		t.Errorf("expected key to match recipient, got %q", key.ToString())
	}
}

func TestParseKeyType_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := sopsclient.ParseKeyType("some-unsupported-key")
	if err == nil {
		t.Error("expected error for unsupported key type")
	}
}
