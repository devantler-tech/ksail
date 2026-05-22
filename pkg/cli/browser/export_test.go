package browser

// CommandFor exposes the unexported platform-command mapping to black-box tests.
func CommandFor(goos, url string) (string, []string) {
	return commandFor(goos, url)
}
