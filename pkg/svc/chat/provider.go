package chat

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	copilot "github.com/github/copilot-sdk/go"
)

const genericAPIKeyEnvVar = "KSAIL_AI_API_KEY" //nolint:gosec // Environment variable name, not a credential.

var (
	// ErrUnsupportedAIProvider indicates an unknown chat provider name.
	ErrUnsupportedAIProvider = errors.New("unsupported AI provider")
	// ErrMissingAIModel indicates a BYOK provider without the required model name.
	ErrMissingAIModel = errors.New("AI model is required for API providers")
	// ErrMissingAIAPIKey indicates a hosted provider without an API key.
	ErrMissingAIAPIKey = errors.New("AI provider API key is required")
	// ErrMissingAIBaseURL indicates a provider whose endpoint cannot be inferred.
	ErrMissingAIBaseURL = errors.New("AI provider base URL is required")
	// ErrInvalidAIBaseURL indicates a malformed or provider-incompatible endpoint.
	ErrInvalidAIBaseURL = errors.New("invalid AI provider base URL")
	// ErrInvalidAIWireAPI indicates a wire format other than completions or responses.
	ErrInvalidAIWireAPI = errors.New("invalid AI provider wire API")
)

// ResolvedProvider is a validated chat provider ready to pass to the shared Copilot SDK runtime.
// SDK is nil only for GitHub Copilot itself; every API-key provider is isolated to a BYOK session.
type ResolvedProvider struct {
	Name  v1alpha1.AIProvider
	Model string
	SDK   *copilot.ProviderConfig
}

// UsesCopilot reports whether the provider needs GitHub Copilot authentication.
func (p ResolvedProvider) UsesCopilot() bool {
	return p.Name == v1alpha1.AIProviderCopilot
}

type providerPreset struct {
	sdkType     string
	baseURL     string
	keyEnvVars  []string
	requiresKey bool
}

// ResolveProvider validates spec and resolves its API key through lookupEnv. Explicit APIKeyEnvVar
// configuration fails closed: when named, no other variable is consulted. Otherwise the secure-store
// overlay/default KSAIL_AI_API_KEY wins over provider-conventional variables.
func ResolveProvider(
	spec v1alpha1.ChatSpec,
	lookupEnv func(string) string,
) (ResolvedProvider, error) {
	name := spec.Provider
	if name == "" {
		name = v1alpha1.AIProviderCopilot
	}

	if name == v1alpha1.AIProviderCopilot {
		return ResolvedProvider{Name: name, Model: normalizedModel(spec.Model)}, nil
	}

	preset, supported := providerPresetFor(name)
	if !supported {
		return ResolvedProvider{}, fmt.Errorf("%w: %q", ErrUnsupportedAIProvider, name)
	}

	return resolveAPIProvider(name, spec, preset, lookupEnv)
}

func resolveAPIProvider(
	name v1alpha1.AIProvider,
	spec v1alpha1.ChatSpec,
	preset providerPreset,
	lookupEnv func(string) string,
) (ResolvedProvider, error) {
	resolved := ResolvedProvider{Name: name, Model: normalizedModel(spec.Model)}
	if resolved.Model == "" {
		return ResolvedProvider{}, fmt.Errorf("%w: provider %q", ErrMissingAIModel, name)
	}

	if !validWireAPI(spec.WireAPI) {
		return ResolvedProvider{}, fmt.Errorf("%w: %q", ErrInvalidAIWireAPI, spec.WireAPI)
	}

	baseURL, err := resolveProviderBaseURL(name, spec.BaseURL, preset.baseURL)
	if err != nil {
		return ResolvedProvider{}, err
	}

	apiKey, source := resolveAPIKey(spec.APIKeyEnvVar, preset.keyEnvVars, lookupEnv)
	if preset.requiresKey && apiKey == "" {
		return ResolvedProvider{}, fmt.Errorf(
			"%w for provider %q (set %s)",
			ErrMissingAIAPIKey,
			name,
			source,
		)
	}

	resolved.SDK = &copilot.ProviderConfig{
		Type:    preset.sdkType,
		BaseURL: baseURL,
		APIKey:  apiKey,
		WireAPI: spec.WireAPI,
	}
	if name == v1alpha1.AIProviderAzureOpenAI && spec.AzureAPIVersion != "" {
		resolved.SDK.Azure = &copilot.AzureProviderOptions{APIVersion: spec.AzureAPIVersion}
	}

	return resolved, nil
}

func resolveProviderBaseURL(
	provider v1alpha1.AIProvider,
	configuredURL string,
	defaultURL string,
) (string, error) {
	baseURL := strings.TrimSpace(configuredURL)
	if baseURL == "" {
		baseURL = defaultURL
	}

	if baseURL == "" {
		return "", fmt.Errorf("%w: provider %q", ErrMissingAIBaseURL, provider)
	}

	err := validateProviderBaseURL(provider, baseURL)
	if err != nil {
		return "", err
	}

	return baseURL, nil
}

func providerPresetFor(provider v1alpha1.AIProvider) (providerPreset, bool) {
	switch provider {
	case v1alpha1.AIProviderOpenAI:
		return providerPreset{
			sdkType: "openai", baseURL: "https://api.openai.com/v1",
			keyEnvVars: []string{"OPENAI_API_KEY"}, requiresKey: true,
		}, true
	case v1alpha1.AIProviderAnthropic:
		return providerPreset{
			sdkType: "anthropic", baseURL: "https://api.anthropic.com",
			keyEnvVars: []string{"ANTHROPIC_API_KEY"}, requiresKey: true,
		}, true
	case v1alpha1.AIProviderGemini:
		return providerPreset{
			sdkType: "openai", baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
			keyEnvVars: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, requiresKey: true,
		}, true
	case v1alpha1.AIProviderAzureOpenAI:
		return providerPreset{
			sdkType: "azure", keyEnvVars: []string{"AZURE_OPENAI_API_KEY"}, requiresKey: true,
		}, true
	case v1alpha1.AIProviderOpenRouter:
		return providerPreset{
			sdkType: "openai", baseURL: "https://openrouter.ai/api/v1",
			keyEnvVars: []string{"OPENROUTER_API_KEY"}, requiresKey: true,
		}, true
	case v1alpha1.AIProviderOllama:
		return providerPreset{sdkType: "openai", baseURL: "http://localhost:11434/v1"}, true
	case v1alpha1.AIProviderOpenAICompatible:
		return providerPreset{sdkType: "openai"}, true
	case v1alpha1.AIProviderCopilot:
		return providerPreset{}, false
	default:
		return providerPreset{}, false
	}
}

func normalizedModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "auto" {
		return ""
	}

	return model
}

func validWireAPI(wireAPI string) bool {
	return wireAPI == "" || slices.Contains([]string{"completions", "responses"}, wireAPI)
}

func validateProviderBaseURL(provider v1alpha1.AIProvider, rawURL string) error {
	parsed, err := parseProviderBaseURL(rawURL)
	if err != nil {
		return err
	}

	if parsed.Scheme != "https" && provider != v1alpha1.AIProviderOllama &&
		provider != v1alpha1.AIProviderOpenAICompatible {
		return fmt.Errorf("%w: provider %q requires HTTPS", ErrInvalidAIBaseURL, provider)
	}

	if provider == v1alpha1.AIProviderAzureOpenAI && parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf(
			"%w: Azure OpenAI expects the resource host without a path",
			ErrInvalidAIBaseURL,
		)
	}

	return nil
}

func parseProviderBaseURL(rawURL string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAIBaseURL, rawURL)
	}

	return parsed, nil
}

func resolveAPIKey(
	explicitEnvVar string,
	providerEnvVars []string,
	lookupEnv func(string) string,
) (string, string) {
	if lookupEnv == nil {
		lookupEnv = func(string) string { return "" }
	}

	if explicitEnvVar != "" {
		return lookupEnv(explicitEnvVar), explicitEnvVar
	}

	candidates := append([]string{genericAPIKeyEnvVar}, providerEnvVars...)
	for _, name := range candidates {
		if value := lookupEnv(name); value != "" {
			return value, name
		}
	}

	return "", strings.Join(candidates, " or ")
}

// ProviderDisplayName returns a user-facing provider name.
func ProviderDisplayName(provider v1alpha1.AIProvider) string {
	switch provider {
	case "", v1alpha1.AIProviderCopilot:
		return "GitHub Copilot"
	case v1alpha1.AIProviderOpenAI:
		return "OpenAI"
	case v1alpha1.AIProviderAnthropic:
		return "Anthropic"
	case v1alpha1.AIProviderGemini:
		return "Google Gemini"
	case v1alpha1.AIProviderAzureOpenAI:
		return "Azure OpenAI"
	case v1alpha1.AIProviderOpenRouter:
		return "OpenRouter"
	case v1alpha1.AIProviderOllama:
		return "Ollama"
	case v1alpha1.AIProviderOpenAICompatible:
		return "OpenAI-compatible API"
	default:
		return string(provider)
	}
}
