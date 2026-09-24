package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// nodePrefix returns a command prefix that stands in for the route to a node.
// It serves the document in path only when the appended command is the
// metadata fetch, so a prefix that dropped or reordered the command fails
// instead of passing on canned output.
func nodePrefix(t *testing.T, document string) []string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "userdata")

	err := os.WriteFile(path, []byte(document), 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	script := `case "$0" in
  "curl "*"http://169.254.169.254/hetzner/v1/userdata") cat '` + path + `' ;;
  *) echo "unexpected command: $0" >&2; exit 9 ;;
esac`

	return []string{"sh", "-c", script}
}

func runTool(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	code := run(context.Background(), args, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func TestRunAcceptsCleanNode(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runTool(t, append([]string{"--"}, nodePrefix(t, cleanUserData)...)...)
	if code != exitVerified {
		t.Fatalf("want exit %d, got %d (stderr: %s)", exitVerified, code, stderr)
	}

	if !strings.Contains(stdout, "no cluster signing material") {
		t.Fatalf("want a verified message on stdout, got %q", stdout)
	}
}

func TestRunRejectsLeakingNode(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTool(t, append([]string{"--"}, nodePrefix(t, leakingUserData)...)...)
	if code != exitFailed {
		t.Fatalf("want exit %d, got %d", exitFailed, code)
	}

	if !strings.Contains(stderr, "private key material") {
		t.Fatalf("want the leak named on stderr, got %q", stderr)
	}
}

func TestRunRejectsEmptyDocument(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTool(t, append([]string{"--"}, nodePrefix(t, "")...)...)
	if code != exitFailed {
		t.Fatalf("want exit %d for an unreadable node, got %d", exitFailed, code)
	}

	if !strings.Contains(stderr, "could not be read") {
		t.Fatalf("want the unreadable-node error on stderr, got %q", stderr)
	}
}

func TestRunRejectsFailedNodeCommand(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTool(t, "--", "sh", "-c", "echo 'no route to node' >&2; exit 3")
	if code != exitFailed {
		t.Fatalf("want exit %d when the node command fails, got %d", exitFailed, code)
	}

	if !strings.Contains(stderr, "no route to node") {
		t.Fatalf("want the node command's stderr surfaced, got %q", stderr)
	}
}

func TestRunRejectsStderrOnZeroExit(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTool(
		t, "--", "sh", "-c", "printf '#cloud-config\\n'; echo 'partial transfer' >&2",
	)
	if code != exitFailed {
		t.Fatalf("want exit %d for a read that wrote to stderr, got %d", exitFailed, code)
	}

	if !strings.Contains(stderr, "partial transfer") {
		t.Fatalf("want the diagnostic surfaced, got %q", stderr)
	}
}

func TestRunRequiresCommandPrefix(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--"}, {"sh", "-c"}} {
		code, _, stderr := runTool(t, args...)
		if code != exitUsage {
			t.Fatalf("args %q: want exit %d, got %d", args, exitUsage, code)
		}

		if !strings.Contains(stderr, "usage") {
			t.Fatalf("args %q: want usage on stderr, got %q", args, stderr)
		}
	}
}
