// Package experimental implements KSail's convention for gating experimental
// (feature-flagged) CLI features off by default.
//
// New, not-yet-stable commands ship behind [Guard]: they are hidden from help
// and tool-surface generation, and refuse to run unless the user opts in with
// the global --experimental flag. This decouples shipping a feature from
// releasing it — the code lands and is validated behind the gate, and the
// feature is "flipped on" for a user only by the explicit opt-in.
//
// Lifecycle: gate a feature with Guard when it arrives; graduate it to stable
// by deleting the single Guard call (which un-hides it and drops the opt-in
// requirement). See the "Feature-flag gating" convention in the ksail
// AGENTS.md ## Maintenance section.
//
// For richer runtime flag evaluation (per-user, remote), reach for the
// OpenFeature Go SDK instead — but the lightweight Guard is the default for the
// common "experimental until validated" case.
package experimental

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/spf13/cobra"
)

// ErrDisabled is returned when an experimental command is invoked without the
// global --experimental opt-in.
var ErrDisabled = errors.New(
	"experimental feature is disabled; re-run with --experimental to enable it",
)

// Guard marks cmd as an experimental feature and returns it (so it composes in
// a command constructor's return statement).
//
// The command is hidden from help and tool-surface generation, and its RunE is
// wrapped so an invocation without the global --experimental flag fails fast
// with ErrDisabled instead of executing. Argument and required-flag validation
// still run first (cobra validates those before RunE), so a user who also omits
// a required flag sees that error; a fully-formed invocation without the opt-in
// gets the clear experimental message.
//
// Guard is idempotent-safe on a nil command (returns nil).
func Guard(cmd *cobra.Command) *cobra.Command {
	if cmd == nil {
		return cmd
	}

	cmd.Hidden = true

	inner := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		enabled, err := flags.IsExperimentalEnabled(cmd)
		if err != nil {
			return fmt.Errorf("resolve --%s flag: %w", flags.ExperimentalFlagName, err)
		}

		if !enabled {
			return fmt.Errorf("%s: %w", cmd.CommandPath(), ErrDisabled)
		}

		if inner == nil {
			return nil
		}

		return inner(cmd, args)
	}

	return cmd
}
