package chat //nolint:testpackage // Copilot authentication is intentionally tested through private seams.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientOptionsUseStoredSignInWithoutToken pins the default: with no Copilot token set, the client
// carries no token and leaves the Copilot CLI's stored sign-in in charge. A general-purpose GitHub
// token in the environment never stands in for it.
func TestClientOptionsUseStoredSignInWithoutToken(t *testing.T) {
	t.Setenv("KSAIL_COPILOT_TOKEN", "")
	t.Setenv("COPILOT_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "general-purpose-token")

	options := buildClientOptions(true)
	assert.Nil(t, options.UseLoggedInUser)
	assert.Empty(t, options.GitHubToken)

	for _, entry := range options.Env {
		assert.False(t, strings.HasPrefix(entry, "GITHUB_TOKEN="), "GITHUB_TOKEN reached the CLI")
	}
}

// TestClientOptionsTokenOverridesStoredSignIn pins that a set token stays an override, with
// KSAIL_COPILOT_TOKEN ahead of COPILOT_TOKEN.
func TestClientOptionsTokenOverridesStoredSignIn(t *testing.T) {
	t.Setenv("KSAIL_COPILOT_TOKEN", "")
	t.Setenv("COPILOT_TOKEN", "fallback-token")
	assert.Equal(t, "fallback-token", buildClientOptions(true).GitHubToken)

	t.Setenv("KSAIL_COPILOT_TOKEN", "ksail-token")
	assert.Equal(t, "ksail-token", buildClientOptions(true).GitHubToken)
}

// TestCopilotFixAdviceOffersSignInBeforeToken pins every Copilot fix list: it tells the user to sign
// in with the Copilot CLI, and offers a token only on the last line, for non-interactive environments.
func TestCopilotFixAdviceOffersSignInBeforeToken(t *testing.T) {
	t.Setenv("COPILOT_CLI_PATH", filepath.Join(t.TempDir(), "missing-copilot"))

	_, loginErr := attemptInlineLogin(context.Background(), nil)
	require.Error(t, loginErr)

	startupErr := buildClientStartupError(&copilot.ClientOptions{}, errProviderStartupTest, "")

	for name, message := range map[string]string{
		"client startup": startupErr.Error(),
		"startup format": fmt.Errorf(startupErrFmt, errProviderStartupTest, "").Error(),
		"CLI not found":  loginErr.Error(),
	} {
		_, fixes, found := strings.Cut(message, "To fix:\n")
		require.True(t, found, "%s: no fix list in %q", name, message)

		lines := strings.Split(fixes, "\n")
		last := len(lines) - 1

		assert.Contains(
			t,
			strings.Join(lines[:last], "\n"),
			"copilot auth login",
			"%s: sign-in",
			name,
		)
		assert.Contains(t, lines[last], "KSAIL_COPILOT_TOKEN", "%s: last fix", name)

		for _, line := range lines[:last] {
			assert.NotContains(t, line, "TOKEN", "%s: a token is offered before the last fix", name)
		}
	}
}

// TestCopilotFixAdviceForSetTokenStartsWithTheToken pins the advice when a Copilot token was set:
// the token replaces the stored sign-in, so the first fix is to replace or unset the token.
func TestCopilotFixAdviceForSetTokenStartsWithTheToken(t *testing.T) {
	t.Parallel()

	message := buildClientStartupError(
		&copilot.ClientOptions{GitHubToken: "set-token"},
		errProviderStartupTest,
		"",
	).Error()

	_, fixes, found := strings.Cut(message, "To fix:\n")
	require.True(t, found, "no fix list in %q", message)

	first, _, _ := strings.Cut(fixes, "\n")
	assert.Contains(t, first, "KSAIL_COPILOT_TOKEN")
	assert.Contains(t, first, "unset it")
	assert.Contains(t, first, "copilot auth login")
	assert.ErrorIs(t, buildClientStartupError(
		&copilot.ClientOptions{GitHubToken: "set-token"}, errProviderStartupTest, "",
	), errStartCopilotClient)
}
