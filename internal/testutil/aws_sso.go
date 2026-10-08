// Package testutil provides isolated fixtures for KSail tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// SyntheticSSO supplies an isolated AWS exec plugin that never contacts a provider or opens a browser.
type SyntheticSSO struct {
	Root     string
	Provider *clientcmdapi.ExecConfig
}

// NewSyntheticSSO creates direct legacy and named-session profiles with an initially expired login.
func NewSyntheticSSO(t *testing.T, profile string) SyntheticSSO {
	t.Helper()
	root := t.TempDir()
	config := filepath.Join(root, "config")
	require.NoError(t, os.WriteFile(config, []byte(syntheticSSOProfiles), syntheticPrivateFileMode))

	command := filepath.Join(root, "aws")
	writeSyntheticExecutable(t, command, syntheticAWSCommand)

	provider := &clientcmdapi.ExecConfig{
		Command:         command,
		Args:            []string{"eks", "get-token", "--cluster-name", "synthetic"},
		APIVersion:      "client.authentication.k8s.io/v1beta1",
		InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
		Env: []clientcmdapi.ExecEnvVar{
			{Name: "HOME", Value: root},
			{Name: "USERPROFILE", Value: root},
			{Name: "AWS_CONFIG_FILE", Value: config},
			{Name: "AWS_SHARED_CREDENTIALS_FILE", Value: filepath.Join(root, "credentials")},
			{Name: "AWS_PROFILE", Value: profile},
			{Name: "AWS_DEFAULT_PROFILE", Value: ""},
			{Name: "AWS_ACCESS_KEY_ID", Value: ""},
			{Name: "AWS_SECRET_ACCESS_KEY", Value: ""},
			{Name: "AWS_SESSION_TOKEN", Value: ""},
			{Name: "AWS_WEB_IDENTITY_TOKEN_FILE", Value: ""},
			{Name: "FAKE_ROOT", Value: root},
		},
	}

	return SyntheticSSO{Root: root, Provider: provider}
}

const (
	syntheticPrivateFileMode = 0o600
	syntheticSSOProfiles     = `[profile legacy]
sso_start_url = https://example.invalid/start
sso_region = us-east-1
sso_account_id = 000000000000
sso_role_name = Reader
[profile modern]
sso_session = shared
sso_account_id = 000000000000
sso_role_name = Reader
[profile sibling]
sso_session = shared
sso_account_id = 000000000000
sso_role_name = AnotherReader
[sso-session shared]
sso_start_url = https://example.invalid/start
sso_region = us-east-1
[profile other]
region = us-east-1
`
	syntheticAWSCommand = `#!/bin/bash
set -eu
if [ "$1" = "sso" ]; then
  printf '%s\n' "$*" >> "$FAKE_ROOT/logins"
  if [ "${FAKE_LOGIN_BLOCK:-}" = yes ]; then echo "$$" >> "$FAKE_ROOT/pids"; exec sleep 600; fi
  if [ "${FAKE_LOGIN_GATE:-}" = yes ]; then while [ ! -f "$FAKE_ROOT/release" ]; do sleep 0.02; done; fi
  if [ "${FAKE_LOGIN_FAIL:-}" = yes ]; then echo "SENSITIVE-DIAGNOSTIC" >&2; exit 1; fi
  sleep "${FAKE_LOGIN_DELAY:-0.05}"
  touch "$FAKE_ROOT/ready"
  case " $* " in
    *" --use-device-code "*) echo "Open https://example.invalid/device and enter SYNTHETIC-CODE" ;;
    *) echo "SENSITIVE-LOGIN-OUTPUT" ;;
  esac
  exit 0
fi
if [ "${FAKE_PROBE_FAILURE:-}" = yes ]; then echo "AccessDenied SENSITIVE-DIAGNOSTIC" >&2; exit 1; fi
if [ ! -f "$FAKE_ROOT/ready" ]; then
  echo "The SSO session associated with this profile has expired or is otherwise invalid." >&2
  exit 255
fi
echo '{"apiVersion":"client.authentication.k8s.io/v1beta1","kind":"ExecCredential","status":{"token":"synthetic-only"}}'
`
)

// writeSyntheticExecutable writes the plugin without this process ever holding a write
// descriptor on it.
//
// The tests using this fixture run in parallel and start subprocesses constantly. On Linux a
// child forked while another goroutine still holds a file open for writing inherits that
// descriptor until its own exec completes, and executing the file inside that window fails with
// ETXTBSY. os.WriteFile closes before it returns, but it cannot stop a concurrent fork from
// copying the descriptor first. Writing through a short-lived child shell moves the write
// descriptor into a process nothing here forks from, so the file is closed for good by the time
// the child has exited (same approach as the CI harness stubs, #6199).
func writeSyntheticExecutable(t *testing.T, path, content string) {
	t.Helper()

	//nolint:gosec // path is a test-owned temp file; the shell fragment is a constant.
	command := exec.CommandContext(
		t.Context(), "sh", "-c", `umask 077 && cat >"$1" && chmod 0700 "$1"`, "sh", path,
	)
	command.Stdin = strings.NewReader(content)

	output, err := command.CombinedOutput()
	require.NoError(t, err, "write synthetic AWS command: %s", output)
}
