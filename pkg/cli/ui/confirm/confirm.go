package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
)

// ErrDeletionCancelled is returned when the user cancels a deletion operation.
var ErrDeletionCancelled = errors.New("deletion cancelled")

// DeletionPreview contains all resources that will be deleted.
type DeletionPreview struct {
	ClusterName string
	Provider    v1alpha1.Provider

	// Docker resources
	Nodes            []string // Container names for Docker nodes
	Registries       []string // Registry container names
	SharedContainers []string // Shared containers (e.g., cloud-provider-kind) to be deleted with last cluster

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
	defer ttyCheckerMu.RUnlock()

	if ttyCheckerOverride != nil {
		return ttyCheckerOverride()
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
// This happens when force flag is set OR stdin is not a TTY (non-interactive environment).
func ShouldSkipPrompt(force bool) bool {
	return force || !IsTTY()
}

// ShowDeletionPreview displays information about what will be deleted and the confirmation prompt.
// This includes the cluster name, provider, and any associated resources.
func ShowDeletionPreview(writer io.Writer, preview *DeletionPreview) {
	// Build the preview content as a single block
	var previewText strings.Builder

	previewText.WriteString("The following resources will be deleted:\n")
	previewText.WriteString("  Cluster:  " + preview.ClusterName + "\n")
	previewText.WriteString("  Provider: " + preview.Provider.String())

	// Show provider-specific resources
	switch preview.Provider {
	case v1alpha1.ProviderDocker:
		writeDockerResources(&previewText, preview)
	case v1alpha1.ProviderHetzner:
		writeHetznerResources(&previewText, preview)
	}

	notify.Warningf(writer, "%s", previewText.String())

	// Print the confirmation prompt on its own line without a symbol prefix
	_, _ = fmt.Fprint(writer, `Type "yes" to confirm deletion: `)
}

// writeDockerResources writes Docker-specific resources to the preview.
func writeDockerResources(previewText *strings.Builder, preview *DeletionPreview) {
	if len(preview.Nodes) > 0 {
		previewText.WriteString("\n  Containers:")

		for _, node := range preview.Nodes {
			previewText.WriteString("\n    - " + node)
		}
	}

	if len(preview.Registries) > 0 {
		previewText.WriteString("\n  Registries:")

		for _, reg := range preview.Registries {
			previewText.WriteString("\n    - " + reg)
		}
	}

	if len(preview.SharedContainers) > 0 {
		previewText.WriteString("\n  Shared containers (last Kind cluster):")

		for _, container := range preview.SharedContainers {
			previewText.WriteString("\n    - " + container)
		}
	}
}

// writeHetznerResources writes Hetzner-specific resources to the preview.
func writeHetznerResources(previewText *strings.Builder, preview *DeletionPreview) {
	if len(preview.Servers) > 0 {
		previewText.WriteString("\n  Servers:")

		for _, server := range preview.Servers {
			previewText.WriteString("\n    - " + server)
		}
	}

	if preview.PlacementGroup != "" {
		previewText.WriteString("\n  Placement Group: " + preview.PlacementGroup)
	}

	if preview.Firewall != "" {
		previewText.WriteString("\n  Firewall: " + preview.Firewall)
	}

	if preview.Network != "" {
		previewText.WriteString("\n  Network: " + preview.Network)
	}
}

// PromptForConfirmation reads user input and checks for "yes" confirmation.
// Returns true only if the user types exactly "yes" (case-insensitive).
// Note: The prompt text is displayed by ShowDeletionPreview.
func PromptForConfirmation(_ io.Writer) bool {
	reader := bufio.NewReader(getStdinReader())

	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	// Trim whitespace and compare case-insensitively
	input = strings.TrimSpace(input)

	return strings.EqualFold(input, "yes")
}
