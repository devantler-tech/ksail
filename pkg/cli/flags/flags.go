package flags

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	// BenchmarkFlagName is the global/root persistent flag that enables benchmark output.
	BenchmarkFlagName = "benchmark"
	// ConfigFlagName is the global/root persistent flag that specifies an alternate config file path.
	ConfigFlagName = "config"
	// ExperimentalFlagName is the global/root persistent flag that opts into experimental features.
	ExperimentalFlagName = "experimental"
)

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

// lookupBoolFlagTiered resolves a bool flag across the command's own, inherited, and persistent
// flag sets (in that order), returning the first set that defines it. found is false when no tier
// defines the flag, letting each caller apply its own not-found behavior.
func lookupBoolFlagTiered(cmd *cobra.Command, name string) (bool, bool, error) {
	for _, flagSet := range []*pflag.FlagSet{
		cmd.Flags(),
		cmd.InheritedFlags(),
		cmd.PersistentFlags(),
	} {
		value, found, err := getBoolFlag(flagSet, name)
		if found || err != nil {
			return value, found, err
		}
	}

	return false, false, nil
}

// IsBenchmarkEnabled reports whether the current command invocation has benchmark enabled.
//
// The flag is defined as a root persistent flag and inherited by subcommands.
func IsBenchmarkEnabled(cmd *cobra.Command) (bool, error) {
	if cmd == nil {
		return false, errNilCommand
	}

	value, found, err := lookupBoolFlagTiered(cmd, BenchmarkFlagName)
	if err != nil {
		return false, err
	}

	if !found {
		return false, fmt.Errorf("%w: %q", errFlagNotFound, BenchmarkFlagName)
	}

	return value, nil
}

// IsExperimentalEnabled reports whether the current command invocation opted into experimental
// features via the root --experimental persistent flag.
//
// The flag is defined as a root persistent flag and inherited by subcommands. Unlike
// IsBenchmarkEnabled, a missing flag is treated as disabled (the safe default) rather than an
// error: a gated command constructed outside the root tree — e.g. in a unit test — stays off
// unless the flag is explicitly present and true.
func IsExperimentalEnabled(cmd *cobra.Command) (bool, error) {
	if cmd == nil {
		return false, errNilCommand
	}

	value, _, err := lookupBoolFlagTiered(cmd, ExperimentalFlagName)

	return value, err
}

func getStringFlag(flagSet *pflag.FlagSet, name string) (string, bool, error) {
	if flagSet == nil {
		return "", false, nil
	}

	if flagSet.Lookup(name) == nil {
		return "", false, nil
	}

	v, err := flagSet.GetString(name)
	if err != nil {
		return "", true, fmt.Errorf("get %q flag: %w", name, err)
	}

	return v, true, nil
}

// GetConfigPath returns the config file path from the --config persistent flag.
// Returns an empty string if the flag is not set or not found.
func GetConfigPath(cmd *cobra.Command) (string, error) {
	if cmd == nil {
		return "", errNilCommand
	}

	value, found, err := getStringFlag(cmd.Flags(), ConfigFlagName)
	if found || err != nil {
		return value, err
	}

	value, found, err = getStringFlag(cmd.InheritedFlags(), ConfigFlagName)
	if found || err != nil {
		return value, err
	}

	value, _, err = getStringFlag(cmd.PersistentFlags(), ConfigFlagName)

	return value, err
}

// MaybeTimer returns the provided timer when benchmark output is enabled.
//
// When benchmark is disabled (or the flag is unavailable), it returns nil.
func MaybeTimer(cmd *cobra.Command, tmr timer.Timer) timer.Timer {
	if cmd == nil || tmr == nil {
		return nil
	}

	enabled, err := IsBenchmarkEnabled(cmd)
	if err != nil || !enabled {
		return nil
	}

	return tmr
}
