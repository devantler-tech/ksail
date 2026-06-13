package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
)

// ErrUnsupportedOutputFormat is returned when the --output flag is set to an unsupported value.
var ErrUnsupportedOutputFormat = errors.New("unsupported --output format")

// outputFormatText is the default human-readable output format.
const outputFormatText = "text"

// outputFormatJSON is the machine-readable JSON output format.
const outputFormatJSON = "json"

// ChangeJSON is the JSON representation of a single configuration change.
// It is used by DiffJSONOutput for --output json mode.
type ChangeJSON struct {
	Field    string `json:"field"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// DiffJSONOutput is the JSON representation of the diff result, emitted when
// --output json is set. It is suitable for CI/MCP consumption.
type DiffJSONOutput struct {
	TotalChanges         int          `json:"totalChanges"`
	InPlaceChanges       []ChangeJSON `json:"inPlaceChanges"`
	RebootRequired       []ChangeJSON `json:"rebootRequired"`
	RecreateRequired     []ChangeJSON `json:"recreateRequired"`
	RollingRecreate      []ChangeJSON `json:"rollingRecreate"`
	WipeRequired         []ChangeJSON `json:"wipeRequired"`
	UnknownBaseline      []ChangeJSON `json:"unknownBaseline"`
	RequiresConfirmation bool         `json:"requiresConfirmation"`
}

// getOutputFormat returns the --output flag value from the command, defaulting to "text".
// The value is normalised to lower-case so that "--output JSON" is accepted.
// Safe to call even when the flag is not registered on cmd.
func getOutputFormat(cmd *cobra.Command) string {
	if cmd == nil {
		return outputFormatText
	}

	flag := cmd.Flags().Lookup("output")
	if flag == nil {
		return outputFormatText
	}

	return strings.ToLower(flag.Value.String())
}

// validateOutputFormat returns an error when the --output flag value is
// neither "text" nor "json".
func validateOutputFormat(cmd *cobra.Command) error {
	format := getOutputFormat(cmd)
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf(
			"%w: %q (expected %q or %q)",
			ErrUnsupportedOutputFormat,
			format,
			outputFormatText,
			outputFormatJSON,
		)
	}

	return nil
}

// diffToJSON converts an UpdateResult to a DiffJSONOutput struct.
func diffToJSON(diff *clusterupdate.UpdateResult) DiffJSONOutput {
	convertChanges := func(changes []clusterupdate.Change) []ChangeJSON {
		result := make([]ChangeJSON, len(changes))

		for i, change := range changes {
			result[i] = ChangeJSON{
				Field:    change.Field,
				OldValue: change.OldValue,
				NewValue: change.NewValue,
				Category: change.Category.String(),
				Reason:   change.Reason,
			}
		}

		return result
	}

	return DiffJSONOutput{
		TotalChanges:         diff.TotalChanges(),
		InPlaceChanges:       convertChanges(diff.InPlaceChanges),
		RebootRequired:       convertChanges(diff.RebootRequired),
		RecreateRequired:     convertChanges(diff.RecreateRequired),
		RollingRecreate:      convertChanges(diff.RollingRecreate),
		WipeRequired:         convertChanges(diff.WipeRequired),
		UnknownBaseline:      convertChanges(diff.UnknownBaseline),
		RequiresConfirmation: diff.NeedsUserConfirmation(),
	}
}

// emitDiffJSON serialises diff as indented JSON and writes it to cmd's stdout.
func emitDiffJSON(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	out := diffToJSON(diff)

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Keep '<', '>', '&' literal instead of \u-escaping them; this is CLI
	// output, not HTML.
	enc.SetEscapeHTML(false)

	err := enc.Encode(out)
	if err != nil {
		// Encoding a plain struct with only basic types never fails.
		notify.Errorf(cmd.OutOrStderr(), "failed to marshal diff to JSON: %v", err)

		return
	}

	// enc.Encode already appends a trailing newline.
	_, _ = fmt.Fprint(cmd.OutOrStdout(), buf.String())
}
