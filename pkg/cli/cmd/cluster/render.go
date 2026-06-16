package cluster

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
)

// reportNoApplicableChanges prints the appropriate message when there are no
// changes to apply. It distinguishes a genuinely clean cluster from one whose
// current state could not be read (unknown baseline), so the latter is not
// reported as "No changes detected". Any unknown-baseline table is rendered by
// the caller via displayChangesSummary before this is invoked.
func reportNoApplicableChanges(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	if diff != nil && diff.HasUnknownBaseline() {
		notify.Warningf(
			cmd.ErrOrStderr(),
			"Current cluster state could not be read for %d component(s); shown as "+
				"Unknown. No changes applied.",
			len(diff.UnknownBaseline),
		)

		return
	}

	notify.Infof(cmd.OutOrStdout(), "No changes detected")
}

// reportDryRun prints a summary for dry-run mode and confirms no changes were applied.
// When --output json is set, emits machine-readable JSON only for the empty-diff case
// (displayChangesSummary already emits JSON when there is anything to report).
func reportDryRun(cmd *cobra.Command, diff *clusterupdate.UpdateResult) error {
	if getOutputFormat(cmd) == outputFormatJSON {
		// displayChangesSummary already emitted JSON when there were changes or an
		// unknown baseline. Only emit JSON here for the genuinely empty case so
		// CI/MCP still get a result.
		if diff != nil && diff.TotalChanges() == 0 && !diff.HasUnknownBaseline() {
			emitDiffJSON(cmd, diff)
		}

		return nil
	}

	if diff != nil && diff.TotalChanges() == 0 {
		reportNoApplicableChanges(cmd, diff)

		return nil
	}

	notify.Infof(cmd.OutOrStdout(), "Dry run complete. No changes applied.")

	return nil
}

// reportFailedChanges prints any failed changes from the update result to stderr.
func reportFailedChanges(cmd *cobra.Command, result *clusterupdate.UpdateResult) {
	if len(result.FailedChanges) == 0 {
		return
	}

	var failBlock strings.Builder

	fmt.Fprintf(&failBlock, "%d changes failed to apply:\n", len(result.FailedChanges))

	for _, change := range result.FailedChanges {
		fmt.Fprintf(&failBlock, "  - %s: %s\n", change.Field, change.Reason)
	}

	notify.Errorf(cmd.OutOrStderr(), strings.TrimRight(failBlock.String(), "\n"))
}

// displayChangesSummary outputs a human-readable summary of configuration changes
// as a before/after table with one row per changed field and impact icons.
// Rows are ordered by severity: recreate-required → rolling-recreate → wipe-required →
// reboot-required → in-place.
// Fields with no change are omitted.
// When --output json is set, emits machine-readable JSON instead of the table.
func displayChangesSummary(cmd *cobra.Command, diff *clusterupdate.UpdateResult) {
	if diff.TotalChanges() == 0 && !diff.HasUnknownBaseline() {
		return
	}

	if getOutputFormat(cmd) == outputFormatJSON {
		emitDiffJSON(cmd, diff)

		return
	}

	notify.Titlef(cmd.OutOrStdout(), "🔍", "Change summary")

	notify.Infof(
		cmd.OutOrStdout(),
		formatDiffTable(diff),
	)
}

// diffRow holds a single row of the diff table.
type diffRow struct {
	icon   string
	field  string
	oldVal string
	newVal string
	impact string
}

// categoryIcon returns the severity icon for a change category.
func categoryIcon(cat clusterupdate.ChangeCategory) string {
	switch cat {
	case clusterupdate.ChangeCategoryRecreateRequired:
		return "🔴"
	case clusterupdate.ChangeCategoryRollingRecreate:
		return "🟠"
	case clusterupdate.ChangeCategoryWipeRequired:
		return "⚠️"
	case clusterupdate.ChangeCategoryRebootRequired:
		return "🟡"
	case clusterupdate.ChangeCategoryInPlace:
		return "🟢"
	case clusterupdate.ChangeCategoryUnknown:
		return "⚪"
	default:
		return "⚪"
	}
}

// formatDiffTable builds the formatted diff table string.
// The table has four columns: Component, Before, After, Impact.
// Rows are ordered by severity: 🔴 recreate → 🟠 rolling-recreate → ⚠️ wipe →
// 🟡 reboot → 🟢 in-place → ⚪ unknown.
func formatDiffTable(
	diff *clusterupdate.UpdateResult,
) string {
	realChanges := diff.TotalChanges()
	unknownCount := len(diff.UnknownBaseline)
	rows := collectDiffRows(diff, realChanges+unknownCount)

	// Column headers
	const (
		hdrComponent = "Component"
		hdrBefore    = "Before"
		hdrAfter     = "After"
		hdrImpact    = "Impact"
	)

	colW, colB, colA, colI := computeColumnWidths(
		rows, hdrComponent, hdrBefore, hdrAfter, hdrImpact,
	)

	var block strings.Builder

	// Pre-allocate: each row needs ~colW+colB+colA+colI bytes for data,
	// plus ~16 bytes overhead per row for spacing (6), emoji (4), newlines, padding.
	const tableOverheadRows = 4 // summary, header, separator, trailing

	const perRowPadding = 16 // spacing + emoji + newline

	block.Grow((len(rows) + tableOverheadRows) * (colW + colB + colA + colI + perRowPadding))

	writeSummaryLine(&block, realChanges, unknownCount)
	writeHeaderRow(&block, colW, colB, colA, hdrComponent, hdrBefore, hdrAfter, hdrImpact)
	writeSeparatorRow(&block, colW, colB, colA, colI)
	writeDataRows(&block, rows, colW, colB, colA)

	return strings.TrimRight(block.String(), "\n")
}

// appendChangesAsRows converts a slice of Changes into diffRows and appends
// them to rows, returning the extended slice.
func appendChangesAsRows(rows []diffRow, changes []clusterupdate.Change) []diffRow {
	for _, c := range changes {
		rows = append(rows, diffRow{
			categoryIcon(c.Category), c.Field, c.OldValue, c.NewValue, c.Category.String(),
		})
	}

	return rows
}

// collectDiffRows builds an ordered list of diff rows.
// Order: 🔴 recreate-required → 🟠 rolling-recreate → ⚠️ wipe-required →
// 🟡 reboot-required → 🟢 in-place → ⚪ unknown.
func collectDiffRows(
	diff *clusterupdate.UpdateResult,
	totalRows int,
) []diffRow {
	rows := make([]diffRow, 0, totalRows)
	rows = appendChangesAsRows(rows, diff.RecreateRequired)
	rows = appendChangesAsRows(rows, diff.RollingRecreate)
	rows = appendChangesAsRows(rows, diff.WipeRequired)
	rows = appendChangesAsRows(rows, diff.RebootRequired)
	rows = appendChangesAsRows(rows, diff.InPlaceChanges)
	rows = appendChangesAsRows(rows, diff.UnknownBaseline)

	return rows
}

// computeColumnWidths returns the max width for each table column.
func computeColumnWidths(
	rows []diffRow,
	hdrComp, hdrBefore, hdrAfter, hdrImpact string,
) (int, int, int, int) {
	widthComp := len(hdrComp)
	widthBefore := len(hdrBefore)
	widthAfter := len(hdrAfter)
	widthImpact := len(hdrImpact)

	for _, row := range rows {
		if length := len(row.field); length > widthComp {
			widthComp = length
		}

		if length := len(row.oldVal); length > widthBefore {
			widthBefore = length
		}

		if length := len(row.newVal); length > widthAfter {
			widthAfter = length
		}

		if length := len(row.impact); length > widthImpact {
			widthImpact = length
		}
	}

	return widthComp, widthBefore, widthAfter, widthImpact
}

func writeSummaryLine(block *strings.Builder, realChanges, unknownCount int) {
	switch {
	case unknownCount > 0 && realChanges > 0:
		fmt.Fprintf(block,
			"Detected %d configuration change(s); %d component(s) have an unknown "+
				"baseline (current cluster state could not be read):\n\n",
			realChanges, unknownCount)
	case unknownCount > 0:
		fmt.Fprintf(block,
			"Current cluster state could not be read; %d component(s) shown as Unknown:\n\n",
			unknownCount)
	default:
		fmt.Fprintf(block, "Detected %d configuration changes:\n\n", realChanges)
	}
}

// headerIndent is the number of leading spaces in the header and separator rows.
// This visually aligns with the emoji+space prefix in data rows:
// emoji renders as 2 terminal columns + 1 trailing space = 3 visual columns.
const headerIndent = "   "

func writeHeaderRow(
	block *strings.Builder,
	colW, colB, colA int,
	hdrComp, hdrBefore, hdrAfter, hdrImpact string,
) {
	fmt.Fprintf(block, "%s%-*s  %-*s  %-*s  %s\n",
		headerIndent,
		colW, hdrComp, colB, hdrBefore, colA, hdrAfter, hdrImpact)
}

func writeSeparatorRow(
	block *strings.Builder,
	colW, colB, colA, colI int,
) {
	fmt.Fprintf(block, "%s%s  %s  %s  %s\n",
		headerIndent,
		strings.Repeat("─", colW),
		strings.Repeat("─", colB),
		strings.Repeat("─", colA),
		strings.Repeat("─", colI))
}

func writeDataRows(
	block *strings.Builder,
	rows []diffRow,
	colW, colB, colA int,
) {
	for _, r := range rows {
		fmt.Fprintf(block, "%s %-*s  %-*s  %-*s  %s\n",
			r.icon, colW, r.field,
			colB, r.oldVal,
			colA, r.newVal,
			r.impact)
	}
}

// confirmDisruptiveChanges prompts for confirmation when the diff contains
// disruptive changes (node reboots or rolling node replacement) and consent
// (--yes, or the deprecated --force) was not given. It returns whether rolling
// node replacement is authorized and whether the update should proceed.
//
// Rolling replacement is authorized by consent OR an interactive confirmation.
// It is reported separately from the destructive --force-drain (which governs
// partition wipes and PDB-bypassing drains) so that confirming a rolling
// replacement never implicitly authorizes a wipe that may be discovered during
// apply and was not shown in the prompt.
func confirmDisruptiveChanges(
	cmd *cobra.Command,
	diff *clusterupdate.UpdateResult,
	consent bool,
) (bool, bool) {
	if !diff.HasRebootRequired() && !diff.HasRollingRecreate() {
		return consent, true
	}

	if confirm.ShouldSkipPrompt(consent) {
		return consent, true
	}

	if !promptForDisruptiveChanges(cmd, diff) {
		return false, false
	}

	return consent || diff.HasRollingRecreate(), true
}

// promptForDisruptiveChanges warns about reboot-required and rolling-recreate
// changes and prompts the user to confirm. It returns true when the user
// consents to proceed.
func promptForDisruptiveChanges(cmd *cobra.Command, diff *clusterupdate.UpdateResult) bool {
	var block strings.Builder

	if diff.HasRollingRecreate() {
		fmt.Fprintf(
			&block,
			"%d change(s) require rolling node replacement (one node at a time):\n",
			len(diff.RollingRecreate),
		)

		for _, change := range diff.RollingRecreate {
			fmt.Fprintf(
				&block, "  ⚠ %s: %s → %s. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}
	}

	if diff.HasRebootRequired() {
		fmt.Fprintf(&block, "%d change(s) require node reboots:\n", len(diff.RebootRequired))

		for _, change := range diff.RebootRequired {
			fmt.Fprintf(
				&block, "  ⚠ %s: %s → %s. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}
	}

	notify.Warningf(cmd.OutOrStderr(), "%s", strings.TrimRight(block.String(), "\n"))

	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Type \"yes\" to proceed with these changes: ",
	)

	return confirm.PromptForConfirmation(cmd.OutOrStdout())
}

// confirmRecreate prompts the user to confirm cluster recreation unless consent
// (--yes, or the deprecated --force) was given.
// It returns true if the update should proceed (confirmed or consented), and false if the user cancels.
func confirmRecreate(cmd *cobra.Command, clusterName string, consent bool) bool {
	if confirm.ShouldSkipPrompt(consent) {
		return true
	}

	var prompt strings.Builder

	prompt.WriteString(
		"Update will delete and recreate the cluster.\n",
	)
	prompt.WriteString("All workloads and data will be lost.")

	notify.Warningf(cmd.OutOrStderr(), "%s", prompt.String())

	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Type \"yes\" to proceed with updating cluster %q: ", clusterName,
	)

	if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
		notify.Infof(cmd.OutOrStdout(), "Update cancelled")

		return false
	}

	return true
}
