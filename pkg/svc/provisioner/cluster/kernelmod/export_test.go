package kernelmod

// ContainsModule exposes containsModule for testing.
func ContainsModule(content, name string) bool {
	return containsModule(content, name)
}
