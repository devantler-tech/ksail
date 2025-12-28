package configmanager

import (
	"reflect"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func (m *ConfigManager) addFlagFromField(
	cmd *cobra.Command,
	fieldSelector FieldSelector[v1alpha1.Cluster],
) {
	fieldPtr := fieldSelector.Selector(m.Config)
	if fieldPtr == nil {
		return
	}

	flagName := m.GenerateFlagName(fieldPtr)
	shorthand := m.GenerateShorthand(flagName)

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

// fieldMappings is a map for efficient field-to-flag-name lookup.
// We initialize this map once and reuse it for better performance.
func (m *ConfigManager) getFieldMappings() map[any]string {
	return map[any]string{
		&m.Config.Spec.Cluster.Distribution:               "distribution",
		&m.Config.Spec.Cluster.DistributionConfig:         "distribution-config",
		&m.Config.Spec.Workload.SourceDirectory:           "source-directory",
		&m.Config.Spec.Cluster.Connection.Context:         "context",
		&m.Config.Spec.Cluster.Connection.Kubeconfig:      "kubeconfig",
		&m.Config.Spec.Cluster.Connection.Timeout:         "timeout",
		&m.Config.Spec.Cluster.GitOpsEngine:               "gitops-engine",
		&m.Config.Spec.Cluster.CNI:                        "cni",
		&m.Config.Spec.Cluster.CSI:                        "csi",
		&m.Config.Spec.Cluster.MetricsServer:              "metrics-server",
		&m.Config.Spec.Cluster.CertManager:                "cert-manager",
		&m.Config.Spec.Cluster.LocalRegistry:              "local-registry",
		&m.Config.Spec.Cluster.LocalRegistryOpts.HostPort: "local-registry-port",
		&m.Config.Spec.Cluster.Flux.Interval:              "flux-interval",
		// Unified node counts for all distributions
		&m.Config.Spec.Cluster.Talos.ControlPlanes: "control-planes",
		&m.Config.Spec.Cluster.Talos.Workers:       "workers",
	}
}

// GenerateFlagName generates a user-friendly flag name from a field pointer.
func (m *ConfigManager) GenerateFlagName(fieldPtr any) string {
	fieldMappings := m.getFieldMappings()
	if flagName, exists := fieldMappings[fieldPtr]; exists {
		return flagName
	}

	return "unknown"
}

// GenerateShorthand generates a shorthand flag from the flag name.
func (m *ConfigManager) GenerateShorthand(flagName string) string {
	switch flagName {
	case "distribution":
		return "d"
	case "context":
		return "c"
	case "kubeconfig":
		return "k"
	case "timeout":
		return "t"
	case "source-directory":
		return "s"
	case "gitops-engine":
		return "g"
	case "distribution-config":
		return ""
	default:
		// For other flags, don't provide shorthand to avoid conflicts
		return ""
	}
}

// setFieldValue sets a field value using reflection.
func setFieldValue(fieldPtr any, value any) {
	if fieldPtr == nil || value == nil {
		return
	}

	fieldVal := reflect.ValueOf(fieldPtr)
	if fieldVal.Kind() != reflect.Ptr || fieldVal.IsNil() {
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
	// Convert custom types to string for pflag.Value.Set()
	switch val := defaultValue.(type) {
	case v1alpha1.Distribution:
		_ = pflagValue.Set(string(val))
	case v1alpha1.GitOpsEngine:
		_ = pflagValue.Set(string(val))
	case v1alpha1.CNI:
		_ = pflagValue.Set(string(val))
	case v1alpha1.CSI:
		_ = pflagValue.Set(string(val))
	case v1alpha1.MetricsServer:
		_ = pflagValue.Set(string(val))
	case v1alpha1.CertManager:
		_ = pflagValue.Set(string(val))
	case v1alpha1.LocalRegistry:
		_ = pflagValue.Set(string(val))
	default:
		if str, ok := val.(string); ok {
			_ = pflagValue.Set(str)
		}
	}
}
