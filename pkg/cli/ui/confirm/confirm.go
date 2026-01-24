// Package confirm provides confirmation prompt utilities for destructive operations.
package confirm

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
)

// ErrDeletionCancelled is returned when the user cancels a deletion operation.
var ErrDeletionCancelled = fmt.Errorf("deletion cancelled")

// DeletionPreview contains all resources that will be deleted.
type DeletionPreview struct {
	ClusterName string
	Provider    v1alpha1.Provider

	// Docker resources
	Nodes      []string // Container names for Docker nodes
	Registries []string // Registry container names

	// Hetzner Cloud resources
	Servers        []string // Server names
	PlacementGroup string   // Placement group name (if exists)
	Firewall       string   // Firewall name (if exists)
	Network        string   // Network name (if exists)
}

// Test override variables with mutexes for thread safety.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	stdinReaderMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	stdinReaderOverride io.Reader

	//nolint:gochecknoglobals // dependency injection for tests
	ttyCheckerMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	ttyCheckerOverride func() bool
)

// SetStdinReaderForTests overrides the stdin reader for testing.
// Returns a restore function that should be called to reset the override.
func SetStdinReaderForTests(reader io.Reader) func() {
	stdinReaderMu.Lock()

	previous := stdinReaderOverride
	stdinReaderOverride = reader

	stdinReaderMu.Unlock()

	return func() {
		stdinReaderMu.Lock()

		stdinReaderOverride = previous

		stdinReaderMu.Unlock()
	}
}

// SetTTYCheckerForTests overrides the TTY checker for testing.
// Returns a restore function that should be called to reset the override.
func SetTTYCheckerForTests(checker func() bool) func() {
	ttyCheckerMu.Lock()

	previous := ttyCheckerOverride
	ttyCheckerOverride = checker

	ttyCheckerMu.Unlock()

	return func() {
		ttyCheckerMu.Lock()

		ttyCheckerOverride = previous

		ttyCheckerMu.Unlock()
	}
}

// getStdinReader returns the stdin reader to use, respecting test overrides.
func getStdinReader() io.Reader {
	stdinReaderMu.RLock()
	defer stdinReaderMu.RUnlock()

	if stdinReaderOverride != nil {
		return stdinReaderOverride
	}

	return os.Stdin
}

// IsTTY returns true if stdin is connected to a terminal.
// This is used to skip confirmation prompts in non-interactive environments (CI/pipelines).
func IsTTY() bool {
	ttyCheckerMu.RLock()

	override := ttyCheckerOverride

	ttyCheckerMu.RUnlock()

	if override != nil {
		return override()
	}

	// Check if stdin is a terminal
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	// If stdin is a character device (terminal), ModeCharDevice will be set
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// ShouldSkipPrompt returns true if the confirmation prompt should be skipped.
// This happens when:
// - force flag is set, OR
// - stdin is not a TTY (non-interactive environment)
func ShouldSkipPrompt(force bool) bool {
	return force || !IsTTY()
}

// ShowDeletionPreview displays information about what will be deleted.
// This includes the cluster name, provider, and any associated resources.
func ShowDeletionPreview(writer io.Writer, preview *DeletionPreview) {
	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: "The following resources will be deleted:",
		Writer:  writer,
	})

	// Build the preview content
	var previewText strings.Builder

	previewText.WriteString(fmt.Sprintf("  Cluster:  %s\n", preview.ClusterName))
	previewText.WriteString(fmt.Sprintf("  Provider: %s", preview.Provider.String()))

	// Show provider-specific resources
	switch preview.Provider {
	case v1alpha1.ProviderDocker:
		if len(preview.Nodes) > 0 {
			previewText.WriteString("\n  Containers:")

			for _, node := range preview.Nodes {
				previewText.WriteString(fmt.Sprintf("\n    - %s", node))
			}
		}

		if len(preview.Registries) > 0 {
			previewText.WriteString("\n  Registries:")

			for _, reg := range preview.Registries {
				previewText.WriteString(fmt.Sprintf("\n    - %s", reg))
			}
		}
	case v1alpha1.ProviderHetzner:
		if len(preview.Servers) > 0 {
			previewText.WriteString("\n  Servers:")

			for _, server := range preview.Servers {
				previewText.WriteString(fmt.Sprintf("\n    - %s", server))
			}
		}

		if preview.PlacementGroup != "" {
			previewText.WriteString(fmt.Sprintf("\n  Placement Group: %s", preview.PlacementGroup))
		}

		if preview.Firewall != "" {
			previewText.WriteString(fmt.Sprintf("\n  Firewall: %s", preview.Firewall))
		}

		if preview.Network != "" {
			previewText.WriteString(fmt.Sprintf("\n  Network: %s", preview.Network))
		}
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: previewText.String(),
		Writer:  writer,
	})
}

// PromptForConfirmation asks the user to type "yes" to confirm.
// Returns true only if the user types exactly "yes" (case-insensitive).
func PromptForConfirmation(writer io.Writer) bool {
	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: `Type "yes" to confirm deletion: `,
		Writer:  writer,
	})

	reader := bufio.NewReader(getStdinReader())

	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	// Trim whitespace and compare case-insensitively
	input = strings.TrimSpace(input)

	return strings.EqualFold(input, "yes")
}
