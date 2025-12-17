package cmd

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TimingFlagName is the global/root persistent flag that enables timing output.
const TimingFlagName = "timing"

var (
	errNilCommand   = errors.New("nil command")
	errFlagNotFound = errors.New("flag not found")
)

func getBoolFlag(flagSet *pflag.FlagSet, name string) (bool, bool, error) {
	if flagSet == nil {
		return false, false, nil
	}

	if flagSet.Lookup(name) == nil {
		return false, false, nil
	}

	v, err := flagSet.GetBool(name)
	if err != nil {
		return false, true, fmt.Errorf("get %q flag: %w", name, err)
	}

	return v, true, nil
}

// IsTimingEnabled reports whether the current command invocation has timing enabled.
//
// The flag is defined as a root persistent flag and inherited by subcommands.
func IsTimingEnabled(cmd *cobra.Command) (bool, error) {
	if cmd == nil {
		return false, errNilCommand
	}

	value, found, err := getBoolFlag(cmd.Flags(), TimingFlagName)
	if found || err != nil {
		return value, err
	}

	value, found, err = getBoolFlag(cmd.InheritedFlags(), TimingFlagName)
	if found || err != nil {
		return value, err
	}

	value, found, err = getBoolFlag(cmd.PersistentFlags(), TimingFlagName)
	if found || err != nil {
		return value, err
	}

	return false, fmt.Errorf("%w: %q", errFlagNotFound, TimingFlagName)
}

// MaybeTimer returns the provided timer when timing output is enabled.
//
// When timing is disabled (or the flag is unavailable), it returns nil.
func MaybeTimer(cmd *cobra.Command, tmr timer.Timer) timer.Timer {
	if cmd == nil || tmr == nil {
		return nil
	}

	enabled, err := IsTimingEnabled(cmd)
	if err != nil || !enabled {
		return nil
	}

	return tmr
}
