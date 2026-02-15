package cipher

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/stores/yaml"
)

// Benchmark scenarios:
// - Minimal secret (single key-value)
// - Small config (5 keys)
// - Medium config (20 keys)
// - Large config (100 keys)
// - Nested structure
// - Extract operations

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
	buf.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n  name: large-secrets\ntype: Opaque\ndata:\n")

	for i := 0; i < 100; i++ {
		buf.WriteString("  key-")
		buf.WriteString(string(rune('0' + (i / 10))))
		buf.WriteString(string(rune('0' + (i % 10))))
		buf.WriteString(": \"secret-value-")
		buf.WriteString(string(rune('0' + (i / 10))))
		buf.WriteString(string(rune('0' + (i % 10))))
		buf.WriteString("-abcdef123456\"\n")
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

// setupEncryptionTest creates a temp file with the given content and returns cleanup function.
func setupEncryptionTest(b *testing.B, content []byte) (string, func()) {
	b.Helper()

	tmpDir := b.TempDir()
	filePath := filepath.Join(tmpDir, "secret.yaml")

	err := os.WriteFile(filePath, content, 0o600)
	if err != nil {
		b.Fatalf("Failed to write test file: %v", err)
	}

	cleanup := func() {
		// TempDir auto-cleanup, but we can add explicit cleanup if needed
	}

	return filePath, cleanup
}

// setupDecryptionTest creates a temp file with encrypted content and returns cleanup function.
func setupDecryptionTest(b *testing.B, content []byte) (string, func()) {
	b.Helper()

	tmpDir := b.TempDir()
	filePath := filepath.Join(tmpDir, "secret.yaml")

	// Write plaintext first
	err := os.WriteFile(filePath, content, 0o600)
	if err != nil {
		b.Fatalf("Failed to write test file: %v", err)
	}

	// Encrypt it
	inputStore := &yaml.Store{}
	outputStore := &yaml.Store{}

	opts := encryptOpts{
		encryptConfig: encryptConfig{
			KeyGroups:      []sops.KeyGroup{},
			GroupThreshold: 0,
		},
		Cipher:        aes.NewCipher(),
		InputStore:    inputStore,
		OutputStore:   outputStore,
		InputPath:     filePath,
		ReadFromStdin: false,
		KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
	}

	encryptedData, err := encrypt(opts)
	if err != nil {
		b.Fatalf("Failed to encrypt test file: %v", err)
	}

	// Write encrypted data back
	err = os.WriteFile(filePath, encryptedData, 0o600)
	if err != nil {
		b.Fatalf("Failed to write encrypted file: %v", err)
	}

	cleanup := func() {
		// TempDir auto-cleanup
	}

	return filePath, cleanup
}

// BenchmarkEncrypt_Minimal benchmarks encryption of a minimal secret (1 key).
func BenchmarkEncrypt_Minimal(b *testing.B) {
	content := generateMinimalSecret()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		opts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		_, err = encrypt(opts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// BenchmarkEncrypt_Small benchmarks encryption of a small secret (5 keys).
func BenchmarkEncrypt_Small(b *testing.B) {
	content := generateSmallSecret()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		opts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		_, err = encrypt(opts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// BenchmarkEncrypt_Medium benchmarks encryption of a medium secret (20 keys).
func BenchmarkEncrypt_Medium(b *testing.B) {
	content := generateMediumSecret()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		opts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		_, err = encrypt(opts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// BenchmarkEncrypt_Large benchmarks encryption of a large secret (100 keys).
func BenchmarkEncrypt_Large(b *testing.B) {
	content := generateLargeSecret()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		opts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		_, err = encrypt(opts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// BenchmarkEncrypt_Nested benchmarks encryption of a nested secret structure.
func BenchmarkEncrypt_Nested(b *testing.B) {
	content := generateNestedSecret()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		opts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		_, err = encrypt(opts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// BenchmarkDecrypt_Minimal benchmarks decryption of a minimal encrypted secret.
func BenchmarkDecrypt_Minimal(b *testing.B) {
	content := generateMinimalSecret()

	b.StopTimer()
	filePath, cleanup := setupDecryptionTest(b, content)
	defer cleanup()

	b.StartTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inputStore, outputStore, err := getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		opts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         nil,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_Small benchmarks decryption of a small encrypted secret.
func BenchmarkDecrypt_Small(b *testing.B) {
	content := generateSmallSecret()

	b.StopTimer()
	filePath, cleanup := setupDecryptionTest(b, content)
	defer cleanup()

	b.StartTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inputStore, outputStore, err := getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		opts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         nil,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_Medium benchmarks decryption of a medium encrypted secret.
func BenchmarkDecrypt_Medium(b *testing.B) {
	content := generateMediumSecret()

	b.StopTimer()
	filePath, cleanup := setupDecryptionTest(b, content)
	defer cleanup()

	b.StartTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inputStore, outputStore, err := getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		opts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         nil,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_Large benchmarks decryption of a large encrypted secret.
func BenchmarkDecrypt_Large(b *testing.B) {
	content := generateLargeSecret()

	b.StopTimer()
	filePath, cleanup := setupDecryptionTest(b, content)
	defer cleanup()

	b.StartTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inputStore, outputStore, err := getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		opts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         nil,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_Nested benchmarks decryption of a nested encrypted secret.
func BenchmarkDecrypt_Nested(b *testing.B) {
	content := generateNestedSecret()

	b.StopTimer()
	filePath, cleanup := setupDecryptionTest(b, content)
	defer cleanup()

	b.StartTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inputStore, outputStore, err := getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		opts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         nil,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecrypt_WithExtract benchmarks decryption with key extraction.
func BenchmarkDecrypt_WithExtract(b *testing.B) {
	content := generateMediumSecret()

	b.StopTimer()
	filePath, cleanup := setupDecryptionTest(b, content)
	defer cleanup()

	b.StartTimer()
	b.ResetTimer()

	extractPath := []any{"data", "db-password"}

	for i := 0; i < b.N; i++ {
		inputStore, outputStore, err := getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		opts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         extractPath,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(opts)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRoundtrip_Minimal benchmarks full encrypt-decrypt cycle for minimal secret.
func BenchmarkRoundtrip_Minimal(b *testing.B) {
	content := generateMinimalSecret()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		// Encrypt
		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		encOpts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		encryptedData, err := encrypt(encOpts)
		if err != nil {
			b.Fatal(err)
		}

		err = os.WriteFile(filePath, encryptedData, 0o600)
		if err != nil {
			b.Fatal(err)
		}

		// Decrypt
		inputStore, outputStore, err = getDecryptStores(filePath, false)
		if err != nil {
			b.Fatal(err)
		}

		decOpts := decryptOpts{
			Cipher:          aes.NewCipher(),
			InputStore:      inputStore,
			OutputStore:     outputStore,
			InputPath:       filePath,
			ReadFromStdin:   false,
			IgnoreMAC:       false,
			Extract:         nil,
			KeyServices:     []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
			DecryptionOrder: []string{},
		}

		_, err = decrypt(decOpts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// BenchmarkEncryptWithAge benchmarks encryption using age encryption (if configured).
// This benchmark will skip if age keys are not available.
func BenchmarkEncryptWithAge(b *testing.B) {
	content := generateSmallSecret()

	// Try to create age key group
	keyGroup, err := createAgeKeyGroup()
	if err != nil {
		b.Skipf("Skipping age benchmark: %v", err)

		return
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		filePath, cleanup := setupEncryptionTest(b, content)
		b.StartTimer()

		inputStore, outputStore, err := getStores(filePath)
		if err != nil {
			b.Fatal(err)
		}

		opts := encryptOpts{
			encryptConfig: encryptConfig{
				KeyGroups:      []sops.KeyGroup{keyGroup},
				GroupThreshold: 0,
			},
			Cipher:        aes.NewCipher(),
			InputStore:    inputStore,
			OutputStore:   outputStore,
			InputPath:     filePath,
			ReadFromStdin: false,
			KeyServices:   []keyservice.KeyServiceClient{keyservice.NewLocalClient()},
		}

		_, err = encrypt(opts)
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
	}
}

// createAgeKeyGroup attempts to create an age key group for benchmarking.
// Returns an error if age keys cannot be generated.
func createAgeKeyGroup() (sops.KeyGroup, error) {
	// Generate a temporary age key for benchmarking
	ageKey, err := age.GenerateKeypair()
	if err != nil {
		return sops.KeyGroup{}, err
	}

	return sops.KeyGroup{
		age.MasterKey{
			Recipient: ageKey.PublicKey,
		},
	}, nil
}
