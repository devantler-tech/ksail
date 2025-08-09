package generator

import (
	"fmt"
	"os"
)

// Options defines options for generators when emitting files.
type Options struct {
	Output string
	Force  bool
}

// Generator is implemented by specific distribution generators (kind, k3d, kustomization).
type Generator interface {
	Generate(opts Options) (string, error)
}

// writeMaybe writes content to opts.Output if set, handling force/overwrite messaging.
func writeMaybe(content string, opts Options) (string, error) {
	output := opts.Output
	if output == "" {
		return content, nil
	}
	// Check if file exists and we're not forcing
	if _, err := os.Stat(output); err == nil && !opts.Force {
		fmt.Printf("► skipping %s as it already exists, use --force to overwrite\n", output)
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

// Note: YAML-specific generator moved to yaml.go
