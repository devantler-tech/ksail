package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen,gosec // The table is the provider compatibility matrix; values are test-only placeholders.
func TestResolveProviderPresets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		spec         v1alpha1.ChatSpec
		environment  map[string]string
		wantName     v1alpha1.AIProvider
		wantType     string
		wantBaseURL  string
		wantAPIKey   string
		wantWireAPI  string
		wantAzureAPI string
		wantCopilot  bool
	}{
		{
			name:        "empty defaults to copilot",
			wantName:    v1alpha1.AIProviderCopilot,
			wantCopilot: true,
		},
		{
			name: "copilot remains explicit option",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderCopilot,
				Model:    "gpt-5",
			},
			wantName:    v1alpha1.AIProviderCopilot,
			wantCopilot: true,
		},
		{
			name: "openai responses",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAI,
				Model:    "gpt-5",
				WireAPI:  "responses",
			},
			environment: map[string]string{"OPENAI_API_KEY": "openai-key"},
			wantName:    v1alpha1.AIProviderOpenAI,
			wantType:    "openai",
			wantBaseURL: "https://api.openai.com/v1",
			wantAPIKey:  "openai-key",
			wantWireAPI: "responses",
		},
		{
			name: "anthropic native wire format",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderAnthropic,
				Model:    "claude-sonnet-4-6",
			},
			environment: map[string]string{"ANTHROPIC_API_KEY": "anthropic-key"},
			wantName:    v1alpha1.AIProviderAnthropic,
			wantType:    "anthropic",
			wantBaseURL: "https://api.anthropic.com",
			wantAPIKey:  "anthropic-key",
		},
		{
			name: "gemini openai compatibility",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderGemini,
				Model:    "gemini-2.5-pro",
			},
			environment: map[string]string{"GOOGLE_API_KEY": "gemini-key"},
			wantName:    v1alpha1.AIProviderGemini,
			wantType:    "openai",
			wantBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
			wantAPIKey:  "gemini-key",
		},
		{
			name: "azure openai",
			spec: v1alpha1.ChatSpec{
				Provider:        v1alpha1.AIProviderAzureOpenAI,
				Model:           "production-gpt",
				BaseURL:         "https://example.openai.azure.com",
				AzureAPIVersion: "2025-04-01-preview",
			},
			environment:  map[string]string{"AZURE_OPENAI_API_KEY": "azure-key"},
			wantName:     v1alpha1.AIProviderAzureOpenAI,
			wantType:     "azure",
			wantBaseURL:  "https://example.openai.azure.com",
			wantAPIKey:   "azure-key",
			wantAzureAPI: "2025-04-01-preview",
		},
		{
			name: "openrouter",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenRouter,
				Model:    "anthropic/claude-sonnet-4",
			},
			environment: map[string]string{"OPENROUTER_API_KEY": "router-key"},
			wantName:    v1alpha1.AIProviderOpenRouter,
			wantType:    "openai",
			wantBaseURL: "https://openrouter.ai/api/v1",
			wantAPIKey:  "router-key",
		},
		{
			name: "ollama is keyless",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOllama,
				Model:    "qwen3:8b",
			},
			wantName:    v1alpha1.AIProviderOllama,
			wantType:    "openai",
			wantBaseURL: "http://localhost:11434/v1",
		},
		{
			name: "custom openai compatible endpoint is keyless when desired",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAICompatible,
				Model:    "local-model",
				BaseURL:  "http://127.0.0.1:9000/v1",
			},
			wantName:    v1alpha1.AIProviderOpenAICompatible,
			wantType:    "openai",
			wantBaseURL: "http://127.0.0.1:9000/v1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved, err := chat.ResolveProvider(testCase.spec, mapLookup(testCase.environment))
			require.NoError(t, err)
			assert.Equal(t, testCase.wantName, resolved.Name)
			assert.Equal(t, testCase.spec.Model, resolved.Model)
			assert.Equal(t, testCase.wantCopilot, resolved.UsesCopilot())

			if testCase.wantCopilot {
				assert.Nil(t, resolved.SDK)

				return
			}

			require.NotNil(t, resolved.SDK)
			assert.Equal(t, testCase.wantType, resolved.SDK.Type)
			assert.Equal(t, testCase.wantBaseURL, resolved.SDK.BaseURL)
			assert.Equal(t, testCase.wantAPIKey, resolved.SDK.APIKey)
			assert.Equal(t, testCase.wantWireAPI, resolved.SDK.WireAPI)
			assert.Empty(t, resolved.SDK.ModelID)
			assert.Empty(t, resolved.SDK.WireModel)

			if testCase.wantAzureAPI == "" {
				assert.Nil(t, resolved.SDK.Azure)
			} else {
				require.NotNil(t, resolved.SDK.Azure)
				assert.Equal(t, testCase.wantAzureAPI, resolved.SDK.Azure.APIVersion)
			}
		})
	}
}

func TestResolveProviderAPIKeyPrecedence(t *testing.T) {
	t.Parallel()

	spec := v1alpha1.ChatSpec{Provider: v1alpha1.AIProviderOpenAI, Model: "gpt-5"}
	environment := map[string]string{
		"KSAIL_AI_API_KEY": "ksail-key",
		"OPENAI_API_KEY":   "openai-key",
	}

	resolved, err := chat.ResolveProvider(spec, mapLookup(environment))
	require.NoError(t, err)
	require.NotNil(t, resolved.SDK)
	assert.Equal(t, "ksail-key", resolved.SDK.APIKey)

	spec.APIKeyEnvVar = "TEAM_AI_KEY"
	environment["TEAM_AI_KEY"] = "team-key"

	resolved, err = chat.ResolveProvider(spec, mapLookup(environment))
	require.NoError(t, err)
	assert.Equal(t, "team-key", resolved.SDK.APIKey)
}

func TestResolveProviderNormalizesModel(t *testing.T) {
	t.Parallel()

	resolved, err := chat.ResolveProvider(
		v1alpha1.ChatSpec{Provider: v1alpha1.AIProviderOllama, Model: "  qwen3:8b  "},
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "qwen3:8b", resolved.Model)
}

//nolint:funlen,gosec // The validation matrix stays together; credential-like values are inert fixtures.
func TestResolveProviderRejectsIncompleteOrInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    v1alpha1.ChatSpec
		env     map[string]string
		wantErr error
	}{
		{
			name:    "unsupported provider",
			spec:    v1alpha1.ChatSpec{Provider: "bedrock", Model: "model"},
			wantErr: chat.ErrUnsupportedAIProvider,
		},
		{
			name:    "byok model required",
			spec:    v1alpha1.ChatSpec{Provider: v1alpha1.AIProviderOpenAI},
			wantErr: chat.ErrMissingAIModel,
		},
		{
			name: "cloud key required",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderAnthropic,
				Model:    "claude-sonnet-4-6",
			},
			wantErr: chat.ErrMissingAIAPIKey,
		},
		{
			name: "explicit key variable fails closed",
			spec: v1alpha1.ChatSpec{
				Provider:     v1alpha1.AIProviderOpenAI,
				Model:        "gpt-5",
				APIKeyEnvVar: "MISSING_TEAM_KEY",
			},
			env:     map[string]string{"OPENAI_API_KEY": "must-not-fallback"},
			wantErr: chat.ErrMissingAIAPIKey,
		},
		{
			name: "azure endpoint required",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderAzureOpenAI,
				Model:    "deployment",
			},
			env:     map[string]string{"AZURE_OPENAI_API_KEY": "key"},
			wantErr: chat.ErrMissingAIBaseURL,
		},
		{
			name: "custom endpoint required",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAICompatible,
				Model:    "model",
			},
			wantErr: chat.ErrMissingAIBaseURL,
		},
		{
			name: "invalid endpoint",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAICompatible,
				Model:    "model",
				BaseURL:  "not-a-url",
			},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "hosted provider rejects plaintext endpoint",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAI,
				Model:    "gpt-5",
				BaseURL:  "http://gateway.example.test/v1",
			},
			env:     map[string]string{"OPENAI_API_KEY": "key"},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "remote keyless compatible endpoint rejects plaintext",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAICompatible,
				Model:    "model",
				BaseURL:  "http://gateway.example.test/v1",
			},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "generic key rejects plaintext compatible endpoint",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAICompatible,
				Model:    "model",
				BaseURL:  "http://127.0.0.1:9000/v1",
			},
			env:     map[string]string{"KSAIL_AI_API_KEY": "key"},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "explicit key rejects plaintext compatible endpoint",
			spec: v1alpha1.ChatSpec{
				Provider:     v1alpha1.AIProviderOpenAICompatible,
				Model:        "model",
				BaseURL:      "http://localhost:9000/v1",
				APIKeyEnvVar: "TEAM_AI_KEY",
			},
			env:     map[string]string{"TEAM_AI_KEY": "key"},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "generic key rejects plaintext ollama endpoint",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOllama,
				Model:    "qwen3:8b",
			},
			env:     map[string]string{"KSAIL_AI_API_KEY": "key"},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "endpoint rejects embedded credentials",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAICompatible,
				Model:    "model",
				BaseURL:  "https://user:secret@gateway.example.test/v1",
			},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "azure endpoint must be host only",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderAzureOpenAI,
				Model:    "deployment",
				BaseURL:  "https://example.openai.azure.com/openai/v1",
			},
			env:     map[string]string{"AZURE_OPENAI_API_KEY": "key"},
			wantErr: chat.ErrInvalidAIBaseURL,
		},
		{
			name: "invalid wire api",
			spec: v1alpha1.ChatSpec{
				Provider: v1alpha1.AIProviderOpenAI,
				Model:    "gpt-5",
				WireAPI:  "messages",
			},
			env:     map[string]string{"OPENAI_API_KEY": "key"},
			wantErr: chat.ErrInvalidAIWireAPI,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := chat.ResolveProvider(testCase.spec, mapLookup(testCase.env))
			require.Error(t, err)
			assert.ErrorIs(
				t,
				err,
				testCase.wantErr,
				"error %q does not wrap %q",
				err,
				testCase.wantErr,
			)
		})
	}
}

func TestProviderDisplayName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "GitHub Copilot", chat.ProviderDisplayName(v1alpha1.AIProviderCopilot))
	assert.Equal(t, "Google Gemini", chat.ProviderDisplayName(v1alpha1.AIProviderGemini))
	assert.Equal(
		t,
		"OpenAI-compatible API",
		chat.ProviderDisplayName(v1alpha1.AIProviderOpenAICompatible),
	)
}

func mapLookup(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}
