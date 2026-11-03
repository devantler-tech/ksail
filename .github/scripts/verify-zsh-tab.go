//go:build ignore

// Run only in the opt-in disposable macOS cask verification job.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/caskcompletion"
)

func main() {
	err := run()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}

func run() error {
	result, err := caskcompletion.Verify(context.Background())
	if err != nil {
		return fmt.Errorf("observe native zsh Tab: %w", err)
	}

	err = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		return fmt.Errorf("encode Tab observation: %w", err)
	}

	return nil
}
