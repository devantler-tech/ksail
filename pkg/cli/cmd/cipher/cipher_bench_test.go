package cipher //nolint:testpackage // Benchmarks require access to internal encrypt/decrypt functions.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/yaml"
)

// Benchmark scenarios:
// - Encrypt: Minimal (1 key), Small (5 keys), Medium (20 keys), Large (100 keys), Nested
// - Decrypt: Same sizes + WithExtract (partial decryption)
// - Roundtrip: Full encrypt-decrypt cycle

// --- Test data generators ---

// generateMinimalSecret creates a minimal YAML secret for benchmarking.
func generateMinimalSecret() []byte {
	return []byte(`password: "super-secret-password"`)
}

// generateSmallSecret creates a small YAML secret with 5 keys.
func generateSmallSecret() []byte {
	return []byte(`apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
type: Opaque
data:
  db-password: "postgres123"
  api-key: "key-abc-123"
  jwt-secret: "secret-jwt-token"
  smtp-password: "mail-pass-456"
  redis-password: "redis-789"
`)
}

// generateMediumSecret creates a medium YAML secret with 20 keys.
func generateMediumSecret() []byte {
	return []byte(`apiVersion: v1
kind: Secret
metadata:
  name: multi-service-secrets
  namespace: production
type: Opaque
data:
  db-host: "postgres.prod.svc.cluster.local"
  db-port: "5432"
  db-name: "production"
  db-user: "app_user"
  db-password: "super-secret-db-pass-123"
  api-key-primary: "key-primary-abc-123"
  api-key-secondary: "key-secondary-def-456"
  jwt-secret: "jwt-secret-token-xyz-789"
  jwt-expiry: "3600"
  smtp-host: "smtp.example.com"
  smtp-port: "587"
  smtp-user: "notifications@example.com"
  smtp-password: "mail-pass-456"
  redis-host: "redis.prod.svc.cluster.local"
  redis-port: "6379"
  redis-password: "redis-secret-789"
  s3-access-key: "AWS-ACCESS-KEY-ID"
  s3-secret-key: "AWS-SECRET-ACCESS-KEY"
  s3-bucket: "production-assets"
  cdn-api-key: "cdn-key-123-xyz"
`)
}

// generateLargeSecret creates a large YAML secret with 100 keys.
func generateLargeSecret() []byte {
	var buf bytes.Buffer

	buf.WriteString(
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: large-secrets\ntype: Opaque\ndata:\n",
	)

	for i := range 100 {
		fmt.Fprintf(&buf, "  key-%02d: \"secret-value-%02d-abcdef123456\"\n", i, i)
	}

	return buf.Bytes()
}

// generateNestedSecret creates a YAML secret with nested structure.
func generateNestedSecret() []byte {
	return []byte(`database:
  primary:
    host: "postgres-primary.prod.svc"
    port: 5432
    credentials:
      username: "admin"
      password: "super-secret-primary-pass"
  replica:
    host: "postgres-replica.prod.svc"
    port: 5432
    credentials:
      username: "readonly"
      password: "super-secret-replica-pass"
services:
  api:
    key: "api-key-123-abc"
    secret: "api-secret-456-def"
  smtp:
    host: "smtp.example.com"
    credentials:
      user: "noreply@example.com"
      password: "smtp-pass-789"
  storage:
    s3:
      access_key: "AWS-KEY-123"
      secret_key: "AWS-SECRET-456"
      bucket: "prod-assets"
    cdn:
      api_key: "CDN-KEY-789"
`)
}

// --- Benchmark helpers ---

// defaultKeyGroups generates an age key group for benchmarking and sets the
// SOPS_AGE_KEY environment variable so decryption can find the private key.
func defaultKeyGroups(b *testing.B) []sops.KeyGroup {
	b.Helper()

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		b.Fatalf("Failed to generate age identity: %v", err)
	}

	keyFile := filepath.Join(b.TempDir(), "keys.txt")

	err = os.WriteFile(keyFile, []byte(identity.String()+"\n"), 0o600)
	if err != nil {
		b.Fatalf("Failed to write age key file: %v", err)
	}

	b.Setenv("SOPS_AGE_KEY_FILE", keyFile)

	masterKey, err := sopsage.MasterKeyFromRecipient(identity.Recipient().String())
	if err != nil {
		b.Fatalf("Failed to create age master key: %v", err)
	}

	return []sops.KeyGroup{{masterKey}}
}

// writeTempSecret writes content to a temp YAML file and returns its path.
// Cleanup is handled automatically by b.TempDir().
func writeTempSecret(b *testing.B, content []byte) string {
	b.Helper()

	filePath := filepath.Join(b.TempDir(), "secret.yaml")

	err := os.WriteFile(filePath, content, 0o600)
	if err != nil {
		b.Fatalf("Failed to write test file: %v", err)
	}

	return filePath
}

// newEncryptOpts builds encryptOpts for the given file path with default settings.
func newEncryptOpts(
	filePath string,
	keyGroups []sops.KeyGroup,
) (encryptOpts, error) {
	inputStore, outputStore, err := getStores(filePath)
	if err != nil {
		return encryptOpts{}, err
	}

	return encryptOpts{
		encryptConfig: encryptConfig{
			KeyGroups:      keyGroups,
			GroupThreshold: 0,
		},
		Cipher:        aes.NewCipher(),
		InputStore:    inputStore,
		OutputStore:   outputStore,
		InputPath:     filePath,
		ReadFromStdin: false,
		KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	}, nil
}

// newDecryptOpts builds decryptOpts for the given file path with default settings.
func newDecryptOpts(
	filePath string,
	extract []any,
) (decryptOpts, error) {
	inputStore, outputStore, err := getDecryptStores(filePath, false)
	if err != nil {
		return decryptOpts{}, err
	}

	return decryptOpts{
		Cipher:          aes.NewCipher(),
		InputStore:      inputStore,
		OutputStore:     outputStore,
		InputPath:       filePath,
		ReadFromStdin:   false,
		IgnoreMAC:       false,
		Extract:         extract,
		KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		DecryptionOrder: []string{},
	}, nil
}

// encryptToFile encrypts content and writes the ciphertext to a temp file,
// returning the path. Used to set up decryption benchmarks.
func encryptToFile(b *testing.B, content []byte, keyGroups []sops.KeyGroup) string {
	b.Helper()

	filePath := writeTempSecret(b, content)

	opts := encryptOpts{
		encryptConfig: encryptConfig{
			KeyGroups:      keyGroups,
			GroupThreshold: 0,
		},
		Cipher:      aes.NewCipher(),
		InputStore:  &yaml.Store{},
		OutputStore: &yaml.Store{},
		InputPath:   filePath,
		KeyServices: []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	}

	encryptedData, err := encrypt(opts)
	if err != nil {
		b.Fatalf("Failed to encrypt test file: %v", err)
	}

	err = os.WriteFile(filePath, encryptedData, 0o600)
	if err != nil {
		b.Fatalf("Failed to write encrypted file: %v", err)
	}

	return filePath
}

// --- Encryption Benchmarks ---

func BenchmarkEncrypt(b *testing.B) {
	keyGroups := defaultKeyGroups(b)

	scenarios := []struct {
		name    string
		content []byte
	}{
		{"Minimal", generateMinimalSecret()},
		{"Small", generateSmallSecret()},
		{"Medium", generateMediumSecret()},
		{"Large", generateLargeSecret()},
		{"Nested", generateNestedSecret()},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ResetTimer()

			for range b.N {
				b.StopTimer()
				filePath := writeTempSecret(b, scenario.content)
				b.StartTimer()

				opts, err := newEncryptOpts(filePath, keyGroups)
				if err != nil {
					b.Fatal(err)
				}

				_, err = encrypt(opts)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// --- Decryption Benchmarks ---

func BenchmarkDecrypt(b *testing.B) {
	keyGroups := defaultKeyGroups(b)

	scenarios := []struct {
		name    string
		content []byte
		extract []any
	}{
		{"Minimal", generateMinimalSecret(), nil},
		{"Small", generateSmallSecret(), nil},
		{"Medium", generateMediumSecret(), nil},
		{"Large", generateLargeSecret(), nil},
		{"Nested", generateNestedSecret(), nil},
		{"WithExtract", generateMediumSecret(), []any{"data", "db-password"}},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			filePath := encryptToFile(b, scenario.content, keyGroups)

			b.ResetTimer()

			for range b.N {
				opts, err := newDecryptOpts(filePath, scenario.extract)
				if err != nil {
					b.Fatal(err)
				}

				_, err = decrypt(opts)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// --- Roundtrip Benchmark ---

// BenchmarkRoundtrip_Minimal benchmarks full encrypt-then-decrypt for a minimal secret.
func BenchmarkRoundtrip_Minimal(b *testing.B) {
	content := generateMinimalSecret()
	keyGroups := defaultKeyGroups(b)

	b.ResetTimer()

	for range b.N {
		b.StopTimer()
		filePath := writeTempSecret(b, content)
		b.StartTimer()

		encOpts, err := newEncryptOpts(filePath, keyGroups)
		if err != nil {
			b.Fatal(err)
		}

		encryptedData, err := encrypt(encOpts)
		if err != nil {
			b.Fatal(err)
		}

		err = os.WriteFile(filePath, encryptedData, 0o600)
		if err != nil {
			b.Fatal(err)
		}

		decOpts, err := newDecryptOpts(filePath, nil)
		if err != nil {
			b.Fatal(err)
		}

		_, err = decrypt(decOpts)
		if err != nil {
			b.Fatal(err)
		}
	}
}
