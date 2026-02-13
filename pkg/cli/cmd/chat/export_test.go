package chat

// GetLoadChatConfig returns the loadChatConfig function for testing purposes.
func GetLoadChatConfig() func() chatConfig {
	return loadChatConfig
}
