package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
)

// ErrDeletionCancelled is returned when the user cancels a deletion operation.
var ErrDeletionCancelled = errors.New("deletion cancelled")

// deletionPreviewBufSize is the initial buffer capacity for building deletion preview text.
const deletionPreviewBufSize = 256

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
	return ui.SetTTYCheckerForTests(checker)
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
	return ui.StdinIsTTY()
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
	previewText.Grow(deletionPreviewBufSize)

	previewText.WriteString("The following resources will be deleted:\n  Cluster:  ")
	previewText.WriteString(preview.ClusterName)
	previewText.WriteString("\n  Provider: ")
	previewText.WriteString(preview.Provider.String())

	// Show provider-specific resources
	switch preview.Provider {
	case v1alpha1.ProviderDocker:
		writeDockerResources(&previewText, preview)
	case v1alpha1.ProviderHetzner:
		writeHetznerResources(&previewText, preview)
	case v1alpha1.ProviderOmni:
		writeResourceList(&previewText, "Machines:", preview.Servers)
	case v1alpha1.ProviderAWS:
		// eksctl owns the underlying CloudFormation stacks so we only surface a
		// human-readable placeholder rather than individual AWS resource names.
		writeResourceList(&previewText, "EKS Resources:", preview.Servers)
	case v1alpha1.ProviderGCP:
		// The GKE API owns the managed control plane and node pools, so only a
		// human-readable placeholder is surfaced rather than GCP resource names.
		writeResourceList(&previewText, "GKE Resources:", preview.Servers)
	case v1alpha1.ProviderKubernetes:
		writeResourceList(&previewText, "Kubernetes Resources:", preview.Servers)
	}

	notify.Warningf(writer, "%s", previewText.String())

	// Print the confirmation prompt on its own line without a symbol prefix
	_, _ = fmt.Fprint(writer, `Type "yes" to confirm deletion: `)
}

// writeResourceList writes a resource section header followed by one indented
// list entry per item. Does nothing when items is empty.
func writeResourceList(previewText *strings.Builder, header string, items []string) {
	if len(items) == 0 {
		return
	}

	previewText.WriteString("\n  " + header)

	for _, item := range items {
		previewText.WriteString("\n    - " + item)
	}
}

// writeDockerResources writes Docker-specific resources to the preview.
func writeDockerResources(previewText *strings.Builder, preview *DeletionPreview) {
	writeResourceList(previewText, "Containers:", preview.Nodes)
	writeResourceList(previewText, "Registries:", preview.Registries)
	writeResourceList(
		previewText,
		"Shared containers (last Kind cluster):",
		preview.SharedContainers,
	)
}

// writeHetznerResources writes Hetzner-specific resources to the preview.
func writeHetznerResources(previewText *strings.Builder, preview *DeletionPreview) {
	writeResourceList(previewText, "Servers:", preview.Servers)

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
