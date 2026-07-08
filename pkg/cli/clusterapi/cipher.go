package clusterapi

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	sopsclient "github.com/devantler-tech/ksail/v7/pkg/client/sops"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
)

// Ensure the local backend can encrypt/decrypt secrets.
var _ api.CipherService = (*Service)(nil)

const (
	cipherFormatJSON    = "json"
	agePrivateKeyPrefix = "AGE-SECRET-KEY-"
)

// secretFileExtension maps a requested format to a temp-file extension so SOPS selects the matching
// store. Anything other than "json" is treated as YAML.
func secretFileExtension(format string) string {
	if strings.EqualFold(format, cipherFormatJSON) {
		return ".json"
	}

	return ".yaml"
}

// prepareSecretFile stages content in a temp file (via writeTempSecret) and selects its SOPS
// input/output stores via storeSelector (sopsclient.GetStores for encrypt,
// sopsclient.GetDecryptStores for decrypt) — the setup shared by EncryptSecret and DecryptSecret
// before they diverge on the actual cipher operation. On a store-selection error the temp file is
// cleaned up before returning.
func prepareSecretFile(
	content, format string,
	storeSelector func(path string) (sops.Store, sops.Store, error),
) (string, sops.Store, sops.Store, func(), error) {
	path, cleanup, err := writeTempSecret(content, format)
	if err != nil {
		return "", nil, nil, nil, err
	}

	inputStore, outputStore, err := storeSelector(path)
	if err != nil {
		cleanup()

		return "", nil, nil, nil, fmt.Errorf("select sops store: %w", err)
	}

	return path, inputStore, outputStore, cleanup, nil
}

// writeTempSecret stages content in a 0600 temp file with the format's extension and returns the
// path + a cleanup func. SOPS reads its input from a file path, so in-process encrypt/decrypt write
// to a short-lived temp file (owner-only, removed immediately after) rather than holding it on disk.
func writeTempSecret(content, format string) (string, func(), error) {
	file, err := os.CreateTemp("", "ksail-secret-*"+secretFileExtension(format))
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}

	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }

	_, writeErr := file.WriteString(content)

	closeErr := file.Close()
	if writeErr == nil {
		writeErr = closeErr
	}

	if writeErr != nil {
		cleanup()

		return "", nil, fmt.Errorf("write temp file: %w", writeErr)
	}

	return path, cleanup, nil
}

// EncryptSecret encrypts plaintext (YAML/JSON per format) with SOPS for the given age recipient, or
// the first locally-available age key when recipient is empty.
func (s *Service) EncryptSecret(
	_ context.Context,
	plaintext, recipient, format string,
) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", fmt.Errorf("%w: plaintext is empty", api.ErrInvalid)
	}

	if recipient == "" {
		recipients, err := cipherRecipients()
		if err != nil {
			return "", err
		}

		if len(recipients) == 0 {
			return "", fmt.Errorf(
				"%w: no age recipient supplied and no local age key found", api.ErrInvalid,
			)
		}

		recipient = recipients[0]
	}

	masterKey, err := sopsage.MasterKeyFromRecipient(recipient)
	if err != nil {
		return "", fmt.Errorf("%w: invalid age recipient: %w", api.ErrInvalid, err)
	}

	return runCipherOp(plaintext, format, sopsclient.GetStores, "encrypt secret",
		func(path string, inputStore, outputStore sops.Store) ([]byte, error) {
			return sopsclient.Encrypt(sopsclient.EncryptOpts{
				EncryptConfig: sopsclient.EncryptConfig{KeyGroups: []sops.KeyGroup{{masterKey}}},
				Cipher:        aes.NewCipher(),
				InputStore:    inputStore,
				OutputStore:   outputStore,
				InputPath:     path,
				KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			})
		})
}

// DecryptSecret decrypts a SOPS document (YAML/JSON per format) using the local age keys.
func (s *Service) DecryptSecret(_ context.Context, encrypted, format string) (string, error) {
	if strings.TrimSpace(encrypted) == "" {
		return "", fmt.Errorf("%w: encrypted input is empty", api.ErrInvalid)
	}

	getDecryptStores := func(p string) (sops.Store, sops.Store, error) {
		return sopsclient.GetDecryptStores(p, false)
	}

	return runCipherOp(encrypted, format, getDecryptStores, "decrypt secret",
		func(path string, inputStore, outputStore sops.Store) ([]byte, error) {
			return sopsclient.Decrypt(sopsclient.DecryptOpts{
				Cipher:      aes.NewCipher(),
				InputStore:  inputStore,
				OutputStore: outputStore,
				InputPath:   path,
				KeyServices: []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			})
		})
}

// runCipherOp shares the prepareSecretFile setup/cleanup + result-formatting boilerplate common to
// EncryptSecret and DecryptSecret; op performs the actual SOPS cipher call and errMsg wraps its error.
func runCipherOp(
	content, format string,
	storeSelector func(path string) (sops.Store, sops.Store, error),
	errMsg string,
	cipherFn func(path string, inputStore, outputStore sops.Store) ([]byte, error),
) (string, error) {
	path, inputStore, outputStore, cleanup, err := prepareSecretFile(content, format, storeSelector)
	if err != nil {
		return "", err
	}
	defer cleanup()

	result, err := cipherFn(path, inputStore, outputStore)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errMsg, err)
	}

	return string(result), nil
}

// CipherRecipients lists the age public keys (age1…) derivable from the local age key file.
func (s *Service) CipherRecipients(_ context.Context) ([]string, error) {
	return cipherRecipients()
}

// cipherRecipients reads the local age key file and derives a deduped list of public keys. A missing
// key file is not an error (returns an empty list) — the UI just won't pre-fill a recipient.
func cipherRecipients() ([]string, error) {
	keyPath, err := sopsclient.GetAgeKeyPath()
	if err != nil {
		return nil, fmt.Errorf("resolve age key path: %w", err)
	}

	// keyPath is resolved by sops.GetAgeKeyPath() from SOPS_AGE_KEY_FILE/XDG/OS defaults — a trusted
	// local config path, never request input.
	file, err := os.Open(keyPath) //nolint:gosec // G304: trusted SOPS age-key path, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("open age key file: %w", err)
	}

	defer func() { _ = file.Close() }()

	var recipients []string

	seen := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, agePrivateKeyPrefix) {
			continue
		}

		public, deriveErr := sopsclient.DerivePublicKey(line)
		if deriveErr != nil {
			continue
		}

		if !seen[public] {
			seen[public] = true
			recipients = append(recipients, public)
		}
	}

	err = scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("read age key file: %w", err)
	}

	return recipients, nil
}
