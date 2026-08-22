package hetznerbase_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// errFetchFailed stands in for a non-zero remote exit.
var errFetchFailed = errors.New("exit status 7")

const cleanUserData = `#cloud-config
runcmd:
  - [kubeadm, init, --config, /etc/kubernetes/kubeadm.yaml]
`

// The PEM body is meaningless filler, not a credential: this fixture exists
// because the guard under test is what detects exactly this shape.
//
//nolint:gosec // deliberately fake PEM fixture for the guard under test
const leakingUserData = `#cloud-config
write_files:
  - path: /etc/kubernetes/pki/ca.key
    content: |
      -----BEGIN RSA PRIVATE KEY-----
      MIIEowIBAAKCAQEA
      -----END RSA PRIVATE KEY-----
`

// gzipOf compresses text the way cloud-init accepts raw gzip user-data, so a
// marker hidden behind compression is exercised rather than assumed.
func gzipOf(t *testing.T, text string) string {
	t.Helper()

	var buf bytes.Buffer

	writer := gzip.NewWriter(&buf)

	_, err := writer.Write([]byte(text))
	if err != nil {
		t.Fatalf("write gzip: %v", err)
	}

	err = writer.Close()
	if err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buf.String()
}

func TestVerifyNoSigningMaterialAcceptsCleanUserData(t *testing.T) {
	t.Parallel()

	err := hetznerbase.VerifyNoSigningMaterial(cleanUserData)
	if err != nil {
		t.Fatalf("clean user-data rejected: %v", err)
	}
}

func TestVerifyNoSigningMaterialRejectsPEMPrivateKey(t *testing.T) {
	t.Parallel()

	err := hetznerbase.VerifyNoSigningMaterial(leakingUserData)
	if !errors.Is(err, hetznerbase.ErrPrivateKeyInUserData) {
		t.Fatalf("want ErrPrivateKeyInUserData, got %v", err)
	}
}

func TestVerifyNoSigningMaterialRejectsSigningTransport(t *testing.T) {
	t.Parallel()

	userData := "#cloud-config\nruncmd:\n  - kubeadm init --upload-certs\n"

	err := hetznerbase.VerifyNoSigningMaterial(userData)
	if !errors.Is(err, hetznerbase.ErrSigningTransportInUserData) {
		t.Fatalf("want ErrSigningTransportInUserData, got %v", err)
	}
}

func TestVerifyNoSigningMaterialRejectsBase64EncodedKey(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString(
		[]byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----"),
	)
	userData := "#cloud-config\nwrite_files:\n  - encoding: b64\n    content: " + encoded + "\n"

	err := hetznerbase.VerifyNoSigningMaterial(userData)
	if !errors.Is(err, hetznerbase.ErrPrivateKeyInUserData) {
		t.Fatalf("want ErrPrivateKeyInUserData for base64 payload, got %v", err)
	}
}

func TestVerifyNoSigningMaterialRejectsGzipHiddenKey(t *testing.T) {
	t.Parallel()

	err := hetznerbase.VerifyNoSigningMaterial(gzipOf(t, leakingUserData))
	if !errors.Is(err, hetznerbase.ErrPrivateKeyInUserData) {
		t.Fatalf("want ErrPrivateKeyInUserData behind gzip, got %v", err)
	}
}

// A readback must not fail for a write-path reason. The provider-acceptance
// ceiling bounds what may be SENT; a node that already booted is past that
// decision, so applying it here would report a leak-free node as a failure.
func TestVerifyNoSigningMaterialIgnoresProviderSizeCeiling(t *testing.T) {
	t.Parallel()

	large := "#cloud-config\nruncmd:\n" + strings.Repeat("  - [echo, padding]\n", 4000)
	if len(large) <= 32768 {
		t.Fatalf("fixture must exceed the provider ceiling, got %d bytes", len(large))
	}

	err := hetznerbase.VerifyNoSigningMaterial(large)
	if err != nil {
		t.Fatalf("oversize but clean user-data rejected: %v", err)
	}
}

// An empty metadata document means the provider-visible copy was not observed.
// Reading that as "clean" would make the strongest possible pass out of the
// weakest possible evidence.
func TestVerifyNodeUserDataRejectsEmptyDocument(t *testing.T) {
	t.Parallel()

	run := func(_ context.Context, _ string) ([]byte, []byte, error) {
		return []byte("   \n"), nil, nil
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if !errors.Is(err, hetznerbase.ErrNodeUserDataUnreadable) {
		t.Fatalf("want ErrNodeUserDataUnreadable for empty document, got %v", err)
	}
}

func TestVerifyNodeUserDataSurfacesFetchFailure(t *testing.T) {
	t.Parallel()

	sentinel := errFetchFailed
	run := func(_ context.Context, _ string) ([]byte, []byte, error) {
		return nil, nil, sentinel
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if !errors.Is(err, hetznerbase.ErrNodeUserDataUnreadable) {
		t.Fatalf("want ErrNodeUserDataUnreadable, got %v", err)
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("fetch cause not preserved: %v", err)
	}
}

func TestVerifyNodeUserDataDetectsLeakOnNode(t *testing.T) {
	t.Parallel()

	run := func(_ context.Context, _ string) ([]byte, []byte, error) {
		return []byte(leakingUserData), nil, nil
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if !errors.Is(err, hetznerbase.ErrPrivateKeyInUserData) {
		t.Fatalf("want ErrPrivateKeyInUserData from node, got %v", err)
	}
}

func TestVerifyNodeUserDataAcceptsCleanNode(t *testing.T) {
	t.Parallel()

	run := func(_ context.Context, _ string) ([]byte, []byte, error) {
		return []byte(cleanUserData), nil, nil
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if err != nil {
		t.Fatalf("clean node reported unclean: %v", err)
	}
}

// The verifier is only meaningful if it reads the provider's own copy, so the
// endpoint it queries is pinned rather than left to drift.
func TestVerifyNodeUserDataQueriesTheMetadataUserDataEndpoint(t *testing.T) {
	t.Parallel()

	var seen string

	run := func(_ context.Context, command string) ([]byte, []byte, error) {
		seen = command

		return []byte(cleanUserData), nil, nil
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(seen, "http://169.254.169.254/hetzner/v1/userdata") {
		t.Fatalf("metadata user-data endpoint not queried, command was %q", seen)
	}
}

func TestVerifyNodeUserDataRejectsMissingRunner(t *testing.T) {
	t.Parallel()

	err := hetznerbase.VerifyNodeUserData(t.Context(), nil)
	if !errors.Is(err, hetznerbase.ErrNodeUserDataUnreadable) {
		t.Fatalf("want ErrNodeUserDataUnreadable for nil runner, got %v", err)
	}
}

// A zero exit with a diagnostic on stderr is the case a stdout-only runner
// cannot express: the document may be truncated, yet nothing in stdout says so.
// Reporting it clean would turn a partial read into the strongest result the
// function can return, which is the same failure the empty-document guard
// refuses. curl runs with --show-error, so this is the shape a real fetch takes
// when it warns without failing.
func TestVerifyNodeUserDataRejectsStderrOnZeroExit(t *testing.T) {
	t.Parallel()

	diagnostic := []byte("curl: (18) transfer closed with outstanding read data")

	run := func(_ context.Context, _ string) ([]byte, []byte, error) {
		return []byte(cleanUserData), diagnostic, nil
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if !errors.Is(err, hetznerbase.ErrNodeUserDataUnreadable) {
		t.Fatalf("want ErrNodeUserDataUnreadable for stderr on a zero exit, got %v", err)
	}
}

// Whitespace-only stderr is not a diagnostic — a runner that always allocates a
// buffer would otherwise make every node unreadable, which would be a guard that
// fails closed so hard it can never pass.
func TestVerifyNodeUserDataIgnoresWhitespaceOnlyStderr(t *testing.T) {
	t.Parallel()

	run := func(_ context.Context, _ string) ([]byte, []byte, error) {
		return []byte(cleanUserData), []byte("  \n\t"), nil
	}

	err := hetznerbase.VerifyNodeUserData(t.Context(), run)
	if err != nil {
		t.Fatalf("whitespace-only stderr must not be treated as a diagnostic: %v", err)
	}
}
