package io

import (
	"fmt"
	"os"
)

// FileWriter provides a reusable TryWrite helper for generators.
type FileWriter struct{}

// TryWrite writes content to opts.Output if set, handling force/overwrite messaging.
func (FileWriter) TryWrite(content string, output string, force bool) (string, error) {
	if output == "" {
		return content, nil
	}
	// Check if file exists and we're not forcing
	if _, err := os.Stat(output); err == nil && !force {
		fmt.Printf("► skipping '%s' as it already exists\n", output)
		return content, nil
	}
	// Determine the action message
	if _, err := os.Stat(output); err == nil {
		fmt.Printf("► overwriting '%s'\n", output)
	} else {
		fmt.Printf("► generating '%s'\n", output)
	}
	if err := os.WriteFile(output, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", output, err)
	}
	return content, nil
}
