package chat

// GetLoadChatConfig returns the loadChatConfig function for testing purposes.
func GetLoadChatConfig() func() chatConfig {
	return loadChatConfig
}

// GetResolveModel returns the resolveModel function for testing.
func GetResolveModel() func(string, string) string {
	return resolveModel
}

// GetValidateReasoningEffort returns the validateReasoningEffort function for testing.
func GetValidateReasoningEffort() func(string) error {
	return validateReasoningEffort
}

// GetResolveReasoningEffort returns the resolveReasoningEffort function for testing.
func GetResolveReasoningEffort() func(string, string) (string, error) {
	return resolveReasoningEffort
}

// GetFilterEnvVars returns the filterEnvVars function for testing.
func GetFilterEnvVars() func([]string, []string) []string {
	return filterEnvVars
}
