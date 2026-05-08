package talosconfig_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"

	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
	talosconfigrepair "github.com/devantler-tech/ksail/v7/pkg/svc/repairer/talosconfig"
)

// generateValidCertDER produces a fresh Ed25519 self-signed CA cert.
func generateValidCertDER(t *testing.T) []byte {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"talos-test"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	return der
}

// corruptBasicConstraintsLength bumps the SEQUENCE length byte of the
// BasicConstraints extension from 0x0f down to 0x0e, reproducing the
// real-world corruption pattern.
func corruptBasicConstraintsLength(t *testing.T, der []byte) []byte {
	t.Helper()

	prefix := []byte{0x30, 0x0f, 0x06, 0x03, 0x55, 0x1d, 0x13, 0x01, 0x01, 0xff, 0x04, 0x05, 0x30, 0x03, 0x01, 0x01}

	idx := bytes.Index(der, prefix)
	if idx < 0 {
		t.Fatalf("could not find BasicConstraints SEQUENCE pattern in generated cert; "+
			"go's x509 encoding may have changed (DER head: %x...)", der[:32])
	}

	out := make([]byte, len(der))
	copy(out, der)
	out[idx+1] = 0x0e

	return out
}

// caFieldFromDER returns the talosconfig `ca` value: base64(PEM(der)).
func caFieldFromDER(der []byte) string {
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return base64.StdEncoding.EncodeToString(pemBytes)
}

// writeTalosConfig builds a minimal talosconfig YAML with one context
// whose CA equals the given base64(PEM) string.
func writeTalosConfig(t *testing.T, dir, ca string) string {
	t.Helper()

	body := `context: prod
contexts:
  prod:
    endpoints:
      - https://1.2.3.4:50000
    ca: "` + ca + `"
    crt: ""
    key: ""
`

	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write talosconfig: %v", err)
	}

	return path
}

func TestRepair_HappyPath(t *testing.T) {
	dir := t.TempDir()
	der := generateValidCertDER(t)
	corruptedDER := corruptBasicConstraintsLength(t, der)
	path := writeTalosConfig(t, dir, caFieldFromDER(corruptedDER))

	r := &talosconfigrepair.Repair{Path: path, Now: func() time.Time {
		return time.Date(2026, 5, 7, 23, 53, 32, 0, time.UTC)
	}}

	var log bytes.Buffer
	result := r.Run(context.Background(), &log)

	if result.Status != repairer.StatusRepaired {
		t.Fatalf("status = %s; log: %s", result.Status, log.String())
	}

	if result.BackupPath == "" || !strings.Contains(result.BackupPath, ".bak.20260507-235332") {
		t.Fatalf("expected timestamped backup, got %q", result.BackupPath)
	}

	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup not written: %v", err)
	}

	// Reload the file via Talos's own parser and re-validate the CA.
	reopened, err := clientconfig.Open(path)
	if err != nil {
		t.Fatalf("reopen repaired talosconfig: %v", err)
	}

	caBytes, err := base64.StdEncoding.DecodeString(reopened.Contexts["prod"].CA)
	if err != nil {
		t.Fatalf("base64 decode repaired CA: %v", err)
	}

	block, _ := pem.Decode(caBytes)
	if block == nil {
		t.Fatal("repaired CA is not a PEM block")
	}

	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Fatalf("repaired cert does not parse: %v", err)
	}
}

func TestRepair_AlreadyValid(t *testing.T) {
	dir := t.TempDir()
	der := generateValidCertDER(t)
	path := writeTalosConfig(t, dir, caFieldFromDER(der))

	r := &talosconfigrepair.Repair{Path: path}

	result := r.Run(context.Background(), io.Discard)
	if result.Status != repairer.StatusOK {
		t.Fatalf("expected StatusOK on valid input, got %s (%s)", result.Status, result.Detail)
	}

	if result.BackupPath != "" {
		t.Fatalf("no backup expected on no-op, got %q", result.BackupPath)
	}
}

func TestRepair_MissingFile(t *testing.T) {
	r := &talosconfigrepair.Repair{Path: filepath.Join(t.TempDir(), "does-not-exist")}

	result := r.Run(context.Background(), io.Discard)
	if result.Status != repairer.StatusSkipped {
		t.Fatalf("expected StatusSkipped, got %s", result.Status)
	}
}

func TestRepair_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := os.WriteFile(path, []byte(":\n  - not: valid\n   - yaml"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := &talosconfigrepair.Repair{Path: path}

	result := r.Run(context.Background(), io.Discard)
	if result.Status != repairer.StatusUnrepairable {
		t.Fatalf("expected StatusUnrepairable, got %s", result.Status)
	}

	if result.Err == nil {
		t.Fatal("expected wrapped YAML error")
	}
}

func TestRepair_CorruptionPatternMissing(t *testing.T) {
	// CA bytes that are valid PEM but not parseable as X.509, AND don't
	// contain the recognised corruption pattern.
	dir := t.TempDir()
	gibberish := bytes.Repeat([]byte{0x42}, 64)
	path := writeTalosConfig(t, dir, caFieldFromDER(gibberish))

	r := &talosconfigrepair.Repair{Path: path}

	var log bytes.Buffer
	result := r.Run(context.Background(), &log)

	if result.Status != repairer.StatusUnrepairable {
		t.Fatalf("expected StatusUnrepairable, got %s; log: %s", result.Status, log.String())
	}

	if !strings.Contains(log.String(), "[prod]") {
		t.Fatalf("log missing per-context line: %s", log.String())
	}
}

func TestRepair_BadBase64(t *testing.T) {
	dir := t.TempDir()
	path := writeTalosConfig(t, dir, "@not-base64!!")

	r := &talosconfigrepair.Repair{Path: path}

	var log bytes.Buffer
	result := r.Run(context.Background(), &log)

	if result.Status != repairer.StatusUnrepairable {
		t.Fatalf("expected StatusUnrepairable, got %s", result.Status)
	}

	if !strings.Contains(log.String(), "base64 decode failed") {
		t.Fatalf("expected base64 decode log line, got: %s", log.String())
	}
}

func TestRepair_NoCAField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	body := `context: prod
contexts:
  prod:
    endpoints:
      - https://1.2.3.4:50000
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := &talosconfigrepair.Repair{Path: path}

	result := r.Run(context.Background(), io.Discard)
	if result.Status != repairer.StatusOK {
		t.Fatalf("expected StatusOK on no CA, got %s (%s)", result.Status, result.Detail)
	}
}

func TestRepair_RegistersByDefault(t *testing.T) {
	// Importing the talosconfig package self-registers a default Repair
	// on [repairer.Default()]. This test inspects (but does not mutate)
	// the default registry.
	for _, r := range repairer.Default().All() {
		if r.Name() == "talosconfig-ca" {
			return
		}
	}

	t.Fatal("talosconfig-ca repair was not registered via init()")
}

func TestRepair_MissingFileIsSkipped(t *testing.T) {
	t.Parallel()

	// A path under t.TempDir() is guaranteed not to exist; the repair
	// should report StatusSkipped without surfacing an error.
	r := &talosconfigrepair.Repair{Path: filepath.Join(t.TempDir(), "missing")}

	result := r.Run(context.Background(), io.Discard)
	if result.Status != repairer.StatusSkipped {
		t.Fatalf("expected StatusSkipped for missing file, got %s", result.Status)
	}

	if result.Err != nil {
		t.Fatalf("expected no err for missing file, got %v", result.Err)
	}
}
