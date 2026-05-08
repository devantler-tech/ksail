package talosprovisioner_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

const prodContextName = "prod"

func mustValidCA(t *testing.T) string {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"talos"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return base64.StdEncoding.EncodeToString(pemBytes)
}

func TestValidateCurrentContextCA_Valid(t *testing.T) {
	t.Parallel()

	cfg := &clientconfig.Config{
		Context: prodContextName,
		Contexts: map[string]*clientconfig.Context{
			prodContextName: {CA: mustValidCA(t)},
		},
	}

	err := talosprovisioner.ValidateCurrentContextCAForTest(cfg, "/tmp/talosconfig")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateCurrentContextCA_NilCfg(t *testing.T) {
	t.Parallel()

	err := talosprovisioner.ValidateCurrentContextCAForTest(nil, "")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateCurrentContextCA_NoContext(t *testing.T) {
	t.Parallel()

	cfg := &clientconfig.Config{Context: "missing", Contexts: map[string]*clientconfig.Context{}}

	err := talosprovisioner.ValidateCurrentContextCAForTest(cfg, "")
	if err != nil {
		t.Fatalf("expected nil for missing context, got %v", err)
	}
}

func TestValidateCurrentContextCA_EmptyCA(t *testing.T) {
	t.Parallel()

	cfg := &clientconfig.Config{
		Context:  prodContextName,
		Contexts: map[string]*clientconfig.Context{prodContextName: {}},
	}

	err := talosprovisioner.ValidateCurrentContextCAForTest(cfg, "")
	if err != nil {
		t.Fatalf("expected nil for empty CA, got %v", err)
	}
}

func TestValidateCurrentContextCA_BadBase64(t *testing.T) {
	t.Parallel()

	cfg := &clientconfig.Config{
		Context:  prodContextName,
		Contexts: map[string]*clientconfig.Context{prodContextName: {CA: "@@notbase64@@"}},
	}

	err := talosprovisioner.ValidateCurrentContextCAForTest(cfg, "/tmp/x")
	if !errors.Is(err, talosprovisioner.ErrMalformedTalosConfigCA) {
		t.Fatalf("expected talosprovisioner.ErrMalformedTalosConfigCA, got %v", err)
	}

	if !strings.Contains(err.Error(), "ksail cluster repair") {
		t.Fatalf("error message missing repair pointer: %s", err.Error())
	}
}

func TestValidateCurrentContextCA_NotPEM(t *testing.T) {
	t.Parallel()

	encodedCA := base64.StdEncoding.EncodeToString([]byte("hello world"))
	cfg := &clientconfig.Config{
		Context:  prodContextName,
		Contexts: map[string]*clientconfig.Context{prodContextName: {CA: encodedCA}},
	}

	err := talosprovisioner.ValidateCurrentContextCAForTest(cfg, "/tmp/x")
	if !errors.Is(err, talosprovisioner.ErrMalformedTalosConfigCA) {
		t.Fatalf("expected talosprovisioner.ErrMalformedTalosConfigCA, got %v", err)
	}
}

func TestValidateCurrentContextCA_BadDER(t *testing.T) {
	t.Parallel()

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	cfg := &clientconfig.Config{
		Context: prodContextName,
		Contexts: map[string]*clientconfig.Context{
			prodContextName: {CA: base64.StdEncoding.EncodeToString(pemBytes)},
		},
	}

	err := talosprovisioner.ValidateCurrentContextCAForTest(cfg, "/tmp/x")
	if !errors.Is(err, talosprovisioner.ErrMalformedTalosConfigCA) {
		t.Fatalf("expected talosprovisioner.ErrMalformedTalosConfigCA, got %v", err)
	}

	var typed *talosprovisioner.MalformedTalosConfigCAError
	if !errors.As(err, &typed) {
		t.Fatalf("expected typed error, got %T", err)
	}

	if typed.Path != "/tmp/x" || typed.Context != prodContextName {
		t.Fatalf("unexpected error fields: %+v", typed)
	}

	if typed.Cause == nil {
		t.Fatal("expected wrapped cause")
	}
}
