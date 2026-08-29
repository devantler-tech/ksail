package chat //nolint:testpackage // Provider wiring is intentionally tested through private seams.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errProviderStartupTest = errors.New("test failure")

func TestNewChatCmdExposesProviderFlags(t *testing.T) {
	t.Parallel()

	cmd := NewChatCmd()
	for _, name := range []string{
		"provider", "model", "base-url", "api-key-env", "wire-api", "azure-api-version",
	} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "missing --%s flag", name)
	}
}

func TestParseChatFlagsProviderFlagsOverrideConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "key")
	t.Chdir(t.TempDir())

	yamlContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  chat:
    provider: anthropic
    model: claude-sonnet-4-6
    baseUrl: https://api.anthropic.com
    apiKeyEnvVar: ANTHROPIC_API_KEY
    wireApi: completions
`
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(yamlContent), 0o600))

	cmd := NewChatCmd()
	require.NoError(t, cmd.Flags().Set("provider", "openai"))
	require.NoError(t, cmd.Flags().Set("model", "gpt-5"))
	require.NoError(t, cmd.Flags().Set("base-url", "https://gateway.example.test/v1"))
	require.NoError(t, cmd.Flags().Set("api-key-env", "OPENAI_API_KEY"))
	require.NoError(t, cmd.Flags().Set("wire-api", "responses"))

	got, err := parseChatFlags(cmd)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.AIProviderOpenAI, got.provider)
	assert.Equal(t, "gpt-5", got.model)
	assert.Equal(t, "https://gateway.example.test/v1", got.baseURL)
	assert.Equal(t, "OPENAI_API_KEY", got.apiKeyEnvVar)
	assert.Equal(t, "responses", got.wireAPI)
}

//nolint:paralleltest // t.Chdir mutates process-wide state.
func TestParseChatFlagsExplicitEmptyProviderFlagsClearConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	yamlContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  chat:
    provider: azure-openai
    baseUrl: https://resource.openai.azure.com
    apiKeyEnvVar: TEAM_AZURE_KEY
    wireApi: responses
    azureApiVersion: 2025-04-01-preview
`
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(yamlContent), 0o600))

	cmd := NewChatCmd()
	for _, name := range []string{
		"provider", "base-url", "api-key-env", "wire-api", "azure-api-version",
	} {
		require.NoError(t, cmd.Flags().Set(name, ""))
	}

	got, err := parseChatFlags(cmd)
	require.NoError(t, err)
	assert.Empty(t, got.provider)
	assert.Empty(t, got.baseURL)
	assert.Empty(t, got.apiKeyEnvVar)
	assert.Empty(t, got.wireAPI)
	assert.Empty(t, got.azureAPIVersion)
}

//nolint:paralleltest // t.Chdir mutates process-wide state.
func TestParseChatFlagsUsesProviderConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	yamlContent := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  chat:
    provider: azure-openai
    model: deployment-name
    baseUrl: https://resource.openai.azure.com
    apiKeyEnvVar: TEAM_AZURE_KEY
    wireApi: completions
    azureApiVersion: 2025-04-01-preview
`
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(yamlContent), 0o600))

	got, err := parseChatFlags(NewChatCmd())
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.AIProviderAzureOpenAI, got.provider)
	assert.Equal(t, "deployment-name", got.model)
	assert.Equal(t, "https://resource.openai.azure.com", got.baseURL)
	assert.Equal(t, "TEAM_AZURE_KEY", got.apiKeyEnvVar)
	assert.Equal(t, "completions", got.wireAPI)
	assert.Equal(t, "2025-04-01-preview", got.azureAPIVersion)
}

func TestBuildSessionConfigIncludesResolvedProvider(t *testing.T) {
	t.Parallel()

	sdkProvider := &copilot.ProviderConfig{
		Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "secret",
	}
	resolved := chatsvc.ResolvedProvider{
		Name: v1alpha1.AIProviderOpenAI, Model: "gpt-5", SDK: sdkProvider,
	}

	config := buildSessionConfig(resolved, "high", true, nil)
	assert.Equal(t, "gpt-5", config.Model)
	assert.Equal(t, "high", config.ReasoningEffort)
	assert.Same(t, sdkProvider, config.Provider)
}

func TestClientOptionsDisableCopilotLoginForBYOK(t *testing.T) {
	t.Setenv("KSAIL_COPILOT_TOKEN", "copilot-token-must-not-leak")
	t.Setenv("COPILOT_TOKEN", "fallback-must-not-leak")
	t.Setenv("COPILOT_SDK_AUTH_TOKEN", "sdk-token-must-not-leak")

	options := buildClientOptions(false)
	require.NotNil(t, options.UseLoggedInUser)
	assert.False(t, *options.UseLoggedInUser)
	assert.Empty(t, options.GitHubToken)

	for _, entry := range options.Env {
		assert.NotContains(t, entry, "COPILOT_TOKEN=")
		assert.NotContains(t, entry, "KSAIL_COPILOT_TOKEN=")
		assert.NotContains(t, entry, "COPILOT_SDK_AUTH_TOKEN=")
	}
}

func TestClientOptionsRetainCopilotAuthentication(t *testing.T) {
	t.Setenv("KSAIL_COPILOT_TOKEN", "copilot-token")

	options := buildClientOptions(true)
	assert.Nil(t, options.UseLoggedInUser)
	assert.Equal(t, "copilot-token", options.GitHubToken)
}

func TestConfigureStdioConnectionUsesSDKValueType(t *testing.T) {
	t.Parallel()

	options := &copilot.ClientOptions{}
	configureStdioConnection(options, "/tmp/copilot")

	connection, ok := options.Connection.(copilot.StdioConnection)
	require.True(t, ok, "SDK NewClient rejects the pointer form at runtime")
	assert.Equal(t, "/tmp/copilot", connection.Path)
	assert.NotPanics(t, func() { copilot.NewClient(options) })
}

func TestClientStartupAdviceDoesNotRequireCopilotAuthenticationForBYOK(t *testing.T) {
	t.Parallel()

	byokOptions := &copilot.ClientOptions{UseLoggedInUser: new(false)}
	byokErr := buildClientStartupError(byokOptions, errProviderStartupTest, "")
	assert.NotContains(t, byokErr.Error(), "KSAIL_COPILOT_TOKEN")
	assert.Contains(t, byokErr.Error(), "selected AI provider")

	copilotErr := buildClientStartupError(&copilot.ClientOptions{}, errProviderStartupTest, "")
	assert.Contains(t, copilotErr.Error(), "KSAIL_COPILOT_TOKEN")
}

func TestAuthenticateClientSkipsCopilotStatusForBYOK(t *testing.T) {
	t.Parallel()

	resolved := chatsvc.ResolvedProvider{Name: v1alpha1.AIProviderAnthropic}
	identity, err := authenticateClient(context.Background(), nil, resolved)
	require.NoError(t, err)
	assert.Equal(t, "Anthropic", identity)
}
