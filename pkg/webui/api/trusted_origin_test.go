package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestLocalPageEndpointsAgreeOnTrustedOrigins drives one table of browser origins through both
// endpoints that trust a page served from this machine: the pod exec WebSocket upgrade and the plugin
// install guard. They apply one rule, so a change to what counts as a local page cannot reach one of
// them and leave the other on a weaker rule.
func TestLocalPageEndpointsAgreeOnTrustedOrigins(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		origin  string
		trusted bool
	}{
		{origin: "http://localhost:8080", trusted: true},
		{origin: "http://127.0.0.1:5173", trusted: true},
		{origin: "http://[::1]:8080", trusted: true},
		{origin: "wails://wails", trusted: true},
		{origin: "http://attacker.example", trusted: false},
		{origin: "wails://attacker", trusted: false},
		// The wails scheme carries exactly one trusted value. A loopback authority under it, or the
		// desktop origin in a different letter case, is not the desktop shell's origin.
		{origin: "wails://localhost", trusted: false},
		{origin: "wails://127.0.0.1", trusted: false},
		{origin: "WAILS://WAILS", trusted: false},
		{origin: "Wails://wails", trusted: false},
		// Only http and https pages on loopback are trusted; another scheme on a loopback host is not.
		{origin: "https://localhost:8443", trusted: true},
		{origin: "ftp://localhost", trusted: false},
		{origin: "ws://127.0.0.1:8080", trusted: false},
		{origin: "null", trusted: false},
	} {
		t.Run(testCase.origin, func(t *testing.T) {
			t.Parallel()

			require.Equal(
				t,
				testCase.trusted,
				execUpgradeAccepted(t, testCase.origin),
				"exec upgrade",
			)
			require.Equal(
				t,
				testCase.trusted,
				pluginInstallAccepted(t, testCase.origin),
				"plugin install",
			)
		})
	}
}

// execUpgradeAccepted reports whether the exec endpoint upgrades a WebSocket carrying origin. A refusal
// must be the origin check's 403, so an unrelated failure cannot pass as a rejection.
func execUpgradeAccepted(t *testing.T, origin string) bool {
	t.Helper()

	server := httptest.NewServer((&api.Server{Service: execStub{}}).Handler())
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", origin)

	conn, response, err := websocket.DefaultDialer.Dial(execWebSocketURL(server.URL), header)
	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}

	if err == nil {
		_ = conn.Close()

		return true
	}

	require.NotNil(t, response, "dial failed without a response: %v", err)
	require.Equal(t, http.StatusForbidden, response.StatusCode)

	return false
}

// pluginInstallAccepted reports whether the plugin install endpoint installs for origin. A refusal must
// be the guard's 403 with the installer never called.
func pluginInstallAccepted(t *testing.T, origin string) bool {
	t.Helper()

	installer := &pluginInstallerStub{}
	server := &api.Server{Service: installer}

	recorder := doPluginInstall(t, server, trustedInstallBody, map[string]string{
		"Content-Type": "application/json",
		"Origin":       origin,
	})

	switch recorder.Code {
	case http.StatusCreated:
		require.True(t, installer.installCalled, "201 without an install")

		return true
	case http.StatusForbidden:
		require.False(t, installer.installCalled, "403 after an install")

		return false
	default:
		t.Fatalf("install status = %d; body=%s", recorder.Code, recorder.Body.String())

		return false
	}
}
