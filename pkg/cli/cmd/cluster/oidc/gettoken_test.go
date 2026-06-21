package oidc_test

import (
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster/oidc"
	oidcsvc "github.com/devantler-tech/ksail/v7/pkg/svc/oidc"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTryFromCache_NoCachedToken verifies that an empty cache directory yields
// errNoCachedToken so the caller falls through to the interactive flow.
func TestTryFromCache_NoCachedToken(t *testing.T) {
	t.Parallel()

	token, err := oidc.TryFromCache(
		&cobra.Command{},
		t.TempDir(),
		"missing-key",
		"https://issuer.example.com",
		"client",
		nil,
		"",
	)

	require.ErrorIs(t, err, oidc.ErrNoCachedToken)
	assert.Nil(t, token)
}

// TestTryFromCache_ValidToken verifies that a cached, unexpired token is
// returned directly without any network round-trip.
func TestTryFromCache_ValidToken(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()

	const key = "valid-key"

	want := &oidcsvc.TokenResult{ //nolint:gosec // G101: test token, not a hardcoded credential
		IDToken: "cached-id-token",
		Expiry:  time.Now().Add(time.Hour),
	}
	require.NoError(t, oidcsvc.SaveCachedToken(cacheDir, key, want))

	token, err := oidc.TryFromCache(
		&cobra.Command{},
		cacheDir,
		key,
		"https://issuer.example.com",
		"client",
		nil,
		"",
	)

	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, want.IDToken, token.IDToken)
	assert.WithinDuration(t, want.Expiry, token.Expiry, time.Second)
}

// TestTryFromCache_ExpiredWithoutRefresh verifies that an expired token with no
// refresh token yields errTokenExpired rather than attempting a refresh.
func TestTryFromCache_ExpiredWithoutRefresh(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()

	const key = "expired-key"

	expired := &oidcsvc.TokenResult{
		IDToken: "stale-id-token",
		Expiry:  time.Now().Add(-time.Hour),
		// no RefreshToken
	}
	require.NoError(t, oidcsvc.SaveCachedToken(cacheDir, key, expired))

	token, err := oidc.TryFromCache(
		&cobra.Command{},
		cacheDir,
		key,
		"https://issuer.example.com",
		"client",
		nil,
		"",
	)

	require.ErrorIs(t, err, oidc.ErrTokenExpired)
	assert.Nil(t, token)
}

// TestGetTokenCmd_CachedValidTokenOutputsExecCredential drives the full
// `cluster oidc get-token` command through its cache-hit happy path and asserts it emits
// a valid client.authentication.k8s.io/v1 ExecCredential on stdout — the exact
// contract kubectl relies on. It is deterministic and network-free because the
// cached token is unexpired. Not parallel: it isolates HOME and captures the
// process-global os.Stdout.
//
//nolint:paralleltest // Isolates HOME and captures process-global os.Stdout.
func TestGetTokenCmd_CachedValidTokenOutputsExecCredential(t *testing.T) {
	cleanup := homeenv.Isolate()
	defer cleanup()

	const (
		issuer   = "https://dex.example.com"
		clientID = "kubectl"
		idToken  = "cached-id-token-xyz" //nolint:gosec // G101: test token, not a hardcoded credential
	)

	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	cacheDir, err := oidcsvc.CacheDir()
	require.NoError(t, err)

	key := oidcsvc.CacheKey(issuer, clientID, nil)
	require.NoError(t, oidcsvc.SaveCachedToken(cacheDir, key, &oidcsvc.TokenResult{
		IDToken: idToken,
		Expiry:  expiry,
	}))

	stdout := captureStdout(t, func() {
		cmd := oidc.NewOIDCCmd()
		cmd.SetArgs([]string{
			"get-token",
			"--issuer-url=" + issuer,
			"--client-id=" + clientID,
		})
		require.NoError(t, cmd.Execute())
	})

	var cred struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Status     struct {
			Token               string `json:"token"`
			ExpirationTimestamp string `json:"expirationTimestamp"`
		} `json:"status"`
	}

	require.NoError(t, json.Unmarshal([]byte(stdout), &cred))
	assert.Equal(t, "client.authentication.k8s.io/v1", cred.APIVersion)
	assert.Equal(t, "ExecCredential", cred.Kind)
	assert.Equal(t, idToken, cred.Status.Token)
	assert.Equal(t, expiry.Format(time.RFC3339), cred.Status.ExpirationTimestamp)
}

// captureStdout redirects the process-global os.Stdout for the duration of
// action and returns everything written to it.
func captureStdout(t *testing.T, action func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stdout
	os.Stdout = writer

	defer func() { os.Stdout = orig }()

	action()

	require.NoError(t, writer.Close())

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	return string(data)
}
