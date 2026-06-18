package configmanager

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrFieldSelectorMissingFlagName is returned (via panic) when a field selector
// is registered without a FlagName. Every selector must carry its flag name so
// the flag registration and override paths can resolve it; an empty name would
// previously register a flag literally called "unknown".
var ErrFieldSelectorMissingFlagName = errors.New(
	"field selector is missing a FlagName: every FieldSelector must set FlagName",
)

// AddFlagsFromFields adds CLI flags for all configured field selectors.
func (m *ConfigManager) AddFlagsFromFields(cmd *cobra.Command) {
	for _, fieldSelector := range m.fieldSelectors {
		m.addFlagFromField(cmd, fieldSelector)
	}
}

// handleBoolFlag handles bool type flags.
func (m *ConfigManager) handleBoolFlag(
	cmd *cobra.Command,
	ptr *bool,
	fieldSelector FieldSelector[v1alpha1.Cluster],
	flagName, shorthand string,
) {
	defaultBool := false

	if fieldSelector.DefaultValue != nil {
		if value, ok := fieldSelector.DefaultValue.(bool); ok {
			defaultBool = value
		}
	}

	cmd.Flags().BoolVarP(ptr, flagName, shorthand, defaultBool, fieldSelector.Description)
}

// handleInt32Flag handles int32 type flags.
func (m *ConfigManager) handleInt32Flag(
	cmd *cobra.Command,
	ptr *int32,
	fieldSelector FieldSelector[v1alpha1.Cluster],
	flagName, shorthand string,
) {
	var defaultValue int32

	if fieldSelector.DefaultValue != nil {
		if value, ok := fieldSelector.DefaultValue.(int32); ok {
			defaultValue = value
		}
	}

	cmd.Flags().Int32VarP(ptr, flagName, shorthand, defaultValue, fieldSelector.Description)
}

// addFlagFromField adds a CLI flag for a specific field using type assertion and reflection.
//
// The flag name and shorthand come straight from the FieldSelector metadata
// (FlagName/Shorthand). A selector without a FlagName is a programming error and
// panics at init time rather than silently registering a flag named "unknown".
func (m *ConfigManager) addFlagFromField(
	cmd *cobra.Command,
	fieldSelector FieldSelector[v1alpha1.Cluster],
) {
	fieldPtr := fieldSelector.Selector(m.Config)
	if fieldPtr == nil {
		return
	}

	if fieldSelector.FlagName == "" {
		panic(
			fmt.Errorf(
				"%w (description: %q)",
				ErrFieldSelectorMissingFlagName,
				fieldSelector.Description,
			),
		)
	}

	flagName := fieldSelector.FlagName
	shorthand := fieldSelector.Shorthand

	// Try to handle as pflag.Value interface first (for enum types)
	if !m.handlePflagValue(cmd, fieldPtr, fieldSelector, flagName, shorthand) {
		// Handle standard types that don't implement pflag.Value
		m.handleStandardTypes(cmd, fieldPtr, fieldSelector, flagName, shorthand)
	}

	// Bind the flag to viper (ignoring error for non-critical binding)
	_ = m.Viper.BindPFlag(flagName, cmd.Flags().Lookup(flagName))
}

// handlePflagValue handles fields that implement the pflag.Value interface.
func (m *ConfigManager) handlePflagValue(
	cmd *cobra.Command,
	fieldPtr any,
	fieldSelector FieldSelector[v1alpha1.Cluster],
	flagName, shorthand string,
) bool {
	pflagValue, isPflagValue := fieldPtr.(interface {
		Set(value string) error
		String() string
		Type() string
	})

	if !isPflagValue {
		return false
	}

	// Set default value if provided
	if fieldSelector.DefaultValue != nil {
		m.setPflagValueDefault(pflagValue, fieldSelector.DefaultValue)
	}

	// Use VarP for pflag.Value types to preserve type information
	if shorthand != "" {
		cmd.Flags().VarP(pflagValue, flagName, shorthand, fieldSelector.Description)
	} else {
		cmd.Flags().Var(pflagValue, flagName, fieldSelector.Description)
	}

	// Restore the bare (no-argument) flag form for enums that were bool flags
	// before migrating to a string enum. pflag only auto-sets NoOptDefVal for
	// bool-typed flags, so a Var-registered enum otherwise rejects the valueless
	// `--foo` form ("flag needs an argument"). Setting NoOptDefVal makes the bare
	// form resolve to the configured value, keeping existing scripts working.
	if fieldSelector.BareFlagValue != "" {
		cmd.Flags().Lookup(flagName).NoOptDefVal = fieldSelector.BareFlagValue
	}

	return true
}

// handleStandardTypes handles standard Go types for flag binding.
func (m *ConfigManager) handleStandardTypes(
	cmd *cobra.Command,
	fieldPtr any,
	fieldSelector FieldSelector[v1alpha1.Cluster],
	flagName, shorthand string,
) {
	switch ptr := fieldPtr.(type) {
	case *string:
		m.handleStringFlag(cmd, ptr, fieldSelector, flagName, shorthand)
	case *metav1.Duration:
		m.handleDurationFlag(cmd, ptr, fieldSelector, flagName, shorthand)
	case *bool:
		m.handleBoolFlag(cmd, ptr, fieldSelector, flagName, shorthand)
	case *int32:
		m.handleInt32Flag(cmd, ptr, fieldSelector, flagName, shorthand)
	}
}

// handleStringFlag handles string type flags.
func (m *ConfigManager) handleStringFlag(
	cmd *cobra.Command,
	ptr *string,
	fieldSelector FieldSelector[v1alpha1.Cluster],
	flagName, shorthand string,
) {
	defaultStr := ""

	if fieldSelector.DefaultValue != nil {
		if str, ok := fieldSelector.DefaultValue.(string); ok {
			defaultStr = str
		}
	}

	cmd.Flags().StringVarP(ptr, flagName, shorthand, defaultStr, fieldSelector.Description)
}

// handleDurationFlag handles metav1.Duration type flags.
func (m *ConfigManager) handleDurationFlag(
	cmd *cobra.Command,
	ptr *metav1.Duration,
	fieldSelector FieldSelector[v1alpha1.Cluster],
	flagName, shorthand string,
) {
	defaultDuration := time.Duration(0)

	if fieldSelector.DefaultValue != nil {
		if dur, ok := fieldSelector.DefaultValue.(metav1.Duration); ok {
			defaultDuration = dur.Duration
		}
	}

	cmd.Flags().DurationVarP(
		&ptr.Duration,
		flagName,
		shorthand,
		defaultDuration,
		fieldSelector.Description,
	)
}

// setFieldValue sets a field value using reflection.
func setFieldValue(fieldPtr any, value any) {
	if fieldPtr == nil || value == nil {
		return
	}

	fieldVal := reflect.ValueOf(fieldPtr)
	if fieldVal.Kind() != reflect.Pointer || fieldVal.IsNil() {
		return
	}

	fieldVal = fieldVal.Elem()
	valueVal := reflect.ValueOf(value)

	if fieldVal.Type().AssignableTo(valueVal.Type()) {
		fieldVal.Set(valueVal)
	} else if fieldVal.Type().ConvertibleTo(valueVal.Type()) {
		fieldVal.Set(valueVal.Convert(fieldVal.Type()))
	}
}

// setPflagValueDefault sets the default value for a pflag.Value.
func (m *ConfigManager) setPflagValueDefault(pflagValue interface {
	Set(value string) error
	String() string
	Type() string
}, defaultValue any,
) {
	// Try fmt.Stringer interface first (all our custom types implement this)
	if stringer, ok := defaultValue.(fmt.Stringer); ok {
		_ = pflagValue.Set(stringer.String())

		return
	}

	// Fallback for plain strings
	if str, ok := defaultValue.(string); ok {
		_ = pflagValue.Set(str)
	}
}
