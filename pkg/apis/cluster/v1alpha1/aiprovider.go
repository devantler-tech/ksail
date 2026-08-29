package v1alpha1

// AIProvider identifies the API provider used by the KSail chat assistant.
// The empty value preserves the historical GitHub Copilot default.
type AIProvider string

const (
	// AIProviderCopilot uses the GitHub Copilot service and its authentication flow.
	AIProviderCopilot AIProvider = "copilot"
	// AIProviderOpenAI uses the OpenAI API.
	AIProviderOpenAI AIProvider = "openai"
	// AIProviderAnthropic uses Anthropic's native Messages API.
	AIProviderAnthropic AIProvider = "anthropic"
	// AIProviderGemini uses Google Gemini's OpenAI-compatible API.
	AIProviderGemini AIProvider = "gemini"
	// AIProviderAzureOpenAI uses an Azure OpenAI resource endpoint.
	AIProviderAzureOpenAI AIProvider = "azure-openai"
	// AIProviderOpenRouter uses OpenRouter's OpenAI-compatible API.
	AIProviderOpenRouter AIProvider = "openrouter"
	// AIProviderOllama uses a local Ollama OpenAI-compatible API.
	AIProviderOllama AIProvider = "ollama"
	// AIProviderOpenAICompatible uses a caller-supplied OpenAI-compatible endpoint.
	AIProviderOpenAICompatible AIProvider = "openai-compatible"
)

// ValidAIProviders returns the supported chat provider values in UI display order.
func ValidAIProviders() []AIProvider {
	return []AIProvider{
		AIProviderCopilot,
		AIProviderOpenAI,
		AIProviderAnthropic,
		AIProviderGemini,
		AIProviderAzureOpenAI,
		AIProviderOpenRouter,
		AIProviderOllama,
		AIProviderOpenAICompatible,
	}
}
