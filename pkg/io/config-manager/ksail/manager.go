package configmanager

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	"github.com/devantler-tech/ksail/v5/pkg/io/config-manager/helpers"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	ksailvalidator "github.com/devantler-tech/ksail/v5/pkg/io/validator/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/ui/timer"
	mapstructure "github.com/go-viper/mapstructure/v2"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const defaultLocalRegistryPort int32 = v1alpha1.DefaultLocalRegistryPort

// ErrDistributionConfigNotFound is returned when a distribution config file is not found.
var ErrDistributionConfigNotFound = errors.New("distribution config file not found")

// ConfigManager implements configuration management for KSail v1alpha1.Cluster configurations.
type ConfigManager struct {
	Viper                         *viper.Viper
	fieldSelectors                []FieldSelector[v1alpha1.Cluster]
	Config                        *v1alpha1.Cluster // Exposed config property as suggested
	configLoaded                  bool              // Track if config has been actually loaded
	configFileFound               bool              // Track if a config file was found and read
	Writer                        io.Writer         // Writer for output notifications
	command                       *cobra.Command    // Associated Cobra command for flag introspection
	localRegistryExplicit         bool              // Tracks if config explicitly set the local registry behavior
	localRegistryHostPortExplicit bool              // Tracks if config explicitly set the registry host port
}

// Compile-time interface compliance verification.
// This ensures ConfigManager properly implements configmanagerinterface.ConfigManager[v1alpha1.Cluster].
var _ configmanagerinterface.ConfigManager[v1alpha1.Cluster] = (*ConfigManager)(nil)

// NewConfigManager creates a new configuration manager with the specified field selectors.
// Initializes Viper with all configuration including paths and environment handling.
func NewConfigManager(
	writer io.Writer,
	fieldSelectors ...FieldSelector[v1alpha1.Cluster],
) *ConfigManager {
	viperInstance := InitializeViper()
	config := v1alpha1.NewCluster()

	manager := &ConfigManager{
		Viper:          viperInstance,
		fieldSelectors: fieldSelectors,
		Config:         config,
		configLoaded:   false,
		Writer:         writer,
	}

	return manager
}

// NewCommandConfigManager constructs a ConfigManager bound to the provided Cobra command.
// It registers the supplied field selectors, binds flags from struct fields, and writes output
// to the command's standard output writer.
func NewCommandConfigManager(
	cmd *cobra.Command,
	selectors []FieldSelector[v1alpha1.Cluster],
) *ConfigManager {
	manager := NewConfigManager(cmd.OutOrStdout(), selectors...)
	manager.command = cmd
	manager.AddFlagsFromFields(cmd)

	return manager
}

// LoadConfig loads the configuration from files and environment variables.
// Returns the loaded config (either freshly loaded or previously cached) and an error if loading failed.
// Returns nil config on error.
// Configuration priority: defaults < config files < environment variables < flags.
// If timer is provided, timing information will be included in the success notification.
func (m *ConfigManager) LoadConfig(tmr timer.Timer) (*v1alpha1.Cluster, error) {
	return m.loadConfigWithOptions(tmr, false, false)
}

// LoadConfigSilent loads the configuration without outputting notifications.
// Returns the loaded config, either freshly loaded or previously cached.
func (m *ConfigManager) LoadConfigSilent() (*v1alpha1.Cluster, error) {
	return m.loadConfigWithOptions(nil, true, false)
}

// LoadConfigFromFlagsOnly loads configuration from flags and defaults only, ignoring on-disk config files.
// No notifications are emitted during the loading process.
func (m *ConfigManager) LoadConfigFromFlagsOnly() (*v1alpha1.Cluster, error) {
	return m.loadConfigWithOptions(nil, true, true)
}

// loadConfigWithOptions is the internal implementation with silent option.
//
//nolint:cyclop // config loading requires multiple option checks
func (m *ConfigManager) loadConfigWithOptions(
	tmr timer.Timer,
	silent bool,
	ignoreConfigFile bool,
) (*v1alpha1.Cluster, error) {
	if !silent {
		m.notifyLoadingStart()
	}

	if m.configLoaded {
		if !silent {
			m.notifyConfigReused()
		}

		return m.Config, nil
	}

	if !silent {
		m.notifyLoadingConfig()
	}

	if !ignoreConfigFile {
		// Use native Viper API to read configuration
		err := m.readConfig(silent)
		if err != nil {
			return nil, err
		}
	}

	// Unmarshal and apply defaults
	flagOverrides := m.captureChangedFlagValues()

	err := m.unmarshalAndApplyDefaults()
	if err != nil {
		return nil, err
	}

	err = m.applyFlagOverrides(flagOverrides)
	if err != nil {
		return nil, err
	}

	m.applyGitOpsAwareDefaults(flagOverrides)
	m.applyDistributionConfigDefaults()

	err = m.validateConfig()
	if err != nil {
		return nil, err
	}

	if !silent {
		m.notifyLoadingComplete(tmr)
	}

	m.configLoaded = true

	return m.Config, nil
}

func (m *ConfigManager) readConfig(silent bool) error {
	err := m.Viper.ReadInConfig()
	if err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return fmt.Errorf("failed to read config file: %w", err)
		}

		m.configFileFound = false
		if !silent {
			m.notifyUsingDefaults()
		}
	} else {
		m.configFileFound = true
		if !silent {
			m.notifyConfigFound()
		}
	}

	return nil
}

func (m *ConfigManager) unmarshalAndApplyDefaults() error {
	decoderConfig := func(dc *mapstructure.DecoderConfig) {
		dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			metav1DurationDecodeHook(),
		)
	}

	// Reset derived defaults so we can detect whether users explicitly configured these values.
	m.Config.Spec.Cluster.LocalRegistry = ""

	// Reset TypeMeta fields only if a config file was found.
	// This allows validation to catch incorrect values from config files
	// while preserving defaults when loading from environment variables only.
	if m.configFileFound {
		m.Config.APIVersion = ""
		m.Config.Kind = ""
	}

	err := m.Viper.Unmarshal(m.Config, decoderConfig)
	if err != nil {
		return fmt.Errorf("failed to unmarshal configuration: %w", err)
	}

	// Do NOT restore defaults for TypeMeta fields - they should be validated as-is.
	// This ensures validation will catch incorrect/missing apiVersion and kind values.

	m.localRegistryExplicit = m.Config.Spec.Cluster.LocalRegistry != ""
	m.localRegistryHostPortExplicit = m.Config.Spec.Cluster.Options.LocalRegistry.HostPort != 0

	// Apply field selector defaults for empty fields
	for _, fieldSelector := range m.fieldSelectors {
		fieldPtr := fieldSelector.Selector(m.Config)
		if fieldPtr != nil && isFieldEmpty(fieldPtr) {
			setFieldValue(fieldPtr, fieldSelector.DefaultValue)
		}
	}

	return nil
}

func (m *ConfigManager) captureChangedFlagValues() map[string]string {
	if m.command == nil {
		return nil
	}

	flags := m.command.Flags()
	overrides := make(map[string]string)

	flags.Visit(func(f *pflag.Flag) {
		overrides[f.Name] = f.Value.String()
	})

	return overrides
}

func (m *ConfigManager) applyFlagOverrides(overrides map[string]string) error {
	if overrides == nil {
		return nil
	}

	for _, selector := range m.fieldSelectors {
		fieldPtr := selector.Selector(m.Config)
		if fieldPtr == nil {
			continue
		}

		flagName := m.GenerateFlagName(fieldPtr)

		value, ok := overrides[flagName]
		if !ok {
			continue
		}

		err := setFieldValueFromFlag(fieldPtr, value)
		if err != nil {
			return fmt.Errorf("failed to apply flag override for %s: %w", flagName, err)
		}
	}

	return nil
}

func (m *ConfigManager) applyGitOpsAwareDefaults(flagOverrides map[string]string) {
	if m.Config == nil {
		return
	}

	if !m.wasLocalRegistryExplicit(flagOverrides) {
		m.Config.Spec.Cluster.LocalRegistry = m.defaultLocalRegistryBehavior()
	}

	hostPortExplicit := m.wasLocalRegistryHostPortExplicit(flagOverrides)
	m.applyLocalRegistryPortDefaults(hostPortExplicit)
}

func (m *ConfigManager) wasLocalRegistryExplicit(flagOverrides map[string]string) bool {
	if m.localRegistryExplicit {
		return true
	}

	if flagOverrides == nil {
		return false
	}

	_, ok := flagOverrides["local-registry"]

	return ok
}

func (m *ConfigManager) wasLocalRegistryHostPortExplicit(flagOverrides map[string]string) bool {
	if m.localRegistryHostPortExplicit {
		return true
	}

	if flagOverrides == nil {
		return false
	}

	_, ok := flagOverrides["local-registry-port"]

	return ok
}

func (m *ConfigManager) defaultLocalRegistryBehavior() v1alpha1.LocalRegistry {
	if m.gitOpsEngineSelected() {
		return v1alpha1.LocalRegistryEnabled
	}

	return v1alpha1.LocalRegistryDisabled
}

func (m *ConfigManager) applyLocalRegistryPortDefaults(hostPortExplicit bool) {
	if m.Config.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		if !hostPortExplicit && m.Config.Spec.Cluster.Options.LocalRegistry.HostPort == 0 {
			m.Config.Spec.Cluster.Options.LocalRegistry.HostPort = defaultLocalRegistryPort
		}

		return
	}

	if !hostPortExplicit {
		m.Config.Spec.Cluster.Options.LocalRegistry.HostPort = 0
	}
}

func (m *ConfigManager) gitOpsEngineSelected() bool {
	if m.Config == nil {
		return false
	}

	switch m.Config.Spec.Cluster.GitOpsEngine {
	case "", v1alpha1.GitOpsEngineNone:
		return false
	case v1alpha1.GitOpsEngineFlux:
		return true
	case v1alpha1.GitOpsEngineArgoCD:
		return true
	default:
		return true
	}
}

type flagValueSetter interface {
	Set(value string) error
}

func setFieldValueFromFlag(fieldPtr any, raw string) error {
	if setter, ok := fieldPtr.(flagValueSetter); ok {
		err := setter.Set(raw)
		if err != nil {
			return fmt.Errorf("set flag value: %w", err)
		}

		return nil
	}

	switch ptr := fieldPtr.(type) {
	case *string:
		*ptr = raw

		return nil
	case *metav1.Duration:
		return setDurationFromFlag(ptr, raw)
	case *bool:
		return setBoolFromFlag(ptr, raw)
	case *int32:
		return setInt32FromFlag(ptr, raw)
	default:
		return nil
	}
}

func setDurationFromFlag(target *metav1.Duration, raw string) error {
	if raw == "" {
		target.Duration = 0

		return nil
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}

	target.Duration = duration

	return nil
}

func setBoolFromFlag(target *bool, raw string) error {
	if raw == "" {
		*target = false

		return nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("parse bool %q: %w", raw, err)
	}

	*target = value

	return nil
}

func setInt32FromFlag(target *int32, raw string) error {
	if raw == "" {
		*target = 0

		return nil
	}

	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return fmt.Errorf("parse int32 %q: %w", raw, err)
	}

	*target = int32(value)

	return nil
}

func (m *ConfigManager) notifyLoadingStart() {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Load config...",
		Emoji:   "⏳",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyConfigReused() {
	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "config already loaded, reusing existing config",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyLoadingConfig() {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "loading ksail config",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyUsingDefaults() {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "using default config",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyConfigFound() {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "'%s' found",
		Args:    []any{m.Viper.ConfigFileUsed()},
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyLoadingComplete(tmr timer.Timer) {
	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "config loaded",
		Timer:   tmr,
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) validateConfig() error {
	// Create validator with distribution config for cross-validation
	validator, err := m.createValidatorForDistribution()
	if err != nil {
		// Distribution config loading failed - propagate the error
		return fmt.Errorf("failed to load distribution config for validation: %w", err)
	}

	result := validator.Validate(m.Config)

	if !result.Valid {
		errorMessages := helpers.FormatValidationErrorsMultiline(result)
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "%s",
			Args:    []any{errorMessages},
			Writer:  m.Writer,
		})

		warnings := helpers.FormatValidationWarnings(result)
		for _, warning := range warnings {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: warning,
				Writer:  m.Writer,
			})
		}

		// Return validation summary error instead of full error stack
		return helpers.NewValidationSummaryError(len(result.Errors), len(result.Warnings))
	}

	warnings := helpers.FormatValidationWarnings(result)
	for _, warning := range warnings {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: warning,
			Writer:  m.Writer,
		})
	}

	return nil
}

func (m *ConfigManager) applyDistributionConfigDefaults() {
	if m.Config == nil {
		return
	}

	expected := expectedDistributionConfigName(m.Config.Spec.Cluster.Distribution)
	if expected == "" {
		return
	}

	current := strings.TrimSpace(m.Config.Spec.Cluster.DistributionConfig)
	if current == "" ||
		distributionConfigIsOppositeDefault(current, m.Config.Spec.Cluster.Distribution) {
		m.Config.Spec.Cluster.DistributionConfig = expected
	}
}

func expectedDistributionConfigName(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionKind:
		return "kind.yaml"
	case v1alpha1.DistributionK3d:
		return "k3d.yaml"
	default:
		return ""
	}
}

func distributionConfigIsOppositeDefault(current string, distribution v1alpha1.Distribution) bool {
	switch distribution {
	case v1alpha1.DistributionKind:
		return current == expectedDistributionConfigName(v1alpha1.DistributionK3d)
	case v1alpha1.DistributionK3d:
		return current == expectedDistributionConfigName(v1alpha1.DistributionKind)
	default:
		return false
	}
}

// isFieldEmpty checks if a field pointer points to an empty/zero value.
func isFieldEmpty(fieldPtr any) bool {
	if fieldPtr == nil {
		return true
	}

	fieldVal := reflect.ValueOf(fieldPtr)
	if fieldVal.Kind() != reflect.Ptr || fieldVal.IsNil() {
		return true
	}

	fieldVal = fieldVal.Elem()

	return fieldVal.IsZero()
}

// createValidatorForDistribution creates a validator with the appropriate distribution config.
// Only loads distribution config when Cilium CNI is requested for validation.
// Returns error if distribution config loading fails.
//
//nolint:cyclop // Switch statement with error handling requires multiple branches
func (m *ConfigManager) createValidatorForDistribution() (*ksailvalidator.Validator, error) {
	// Only load distribution config for Cilium CNI validation
	if m.Config.Spec.Cluster.DistributionConfig == "" ||
		m.Config.Spec.Cluster.CNI != v1alpha1.CNICilium {
		return ksailvalidator.NewValidator(), nil
	}

	// Create distribution-specific validator based on configured distribution
	switch m.Config.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		kindConfig, err := m.loadKindConfig()
		if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
			return nil, err
		}

		if kindConfig != nil {
			return ksailvalidator.NewValidatorForKind(kindConfig), nil
		}
	case v1alpha1.DistributionK3d:
		k3dConfig, err := m.loadK3dConfig()
		if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
			return nil, err
		}

		if k3dConfig != nil {
			return ksailvalidator.NewValidatorForK3d(k3dConfig), nil
		}
	}

	return ksailvalidator.NewValidator(), nil
}

// loadKindConfig loads the Kind distribution configuration if it exists.
// Returns ErrDistributionConfigNotFound if the file doesn't exist.
// Returns error if config loading or validation fails.
func (m *ConfigManager) loadKindConfig() (*kindv1alpha4.Cluster, error) {
	// Check if the file actually exists before trying to load it
	// This prevents validation against default configs during init
	_, err := os.Stat(m.Config.Spec.Cluster.DistributionConfig)
	if os.IsNotExist(err) {
		// File doesn't exist
		return nil, ErrDistributionConfigNotFound
	}

	kindManager := kindconfigmanager.NewConfigManager(m.Config.Spec.Cluster.DistributionConfig)

	config, err := kindManager.LoadConfig(nil)
	if err != nil {
		// Propagate validation errors
		return nil, fmt.Errorf("failed to load Kind config: %w", err)
	}

	return config, nil
}

// loadK3dConfig loads the K3d distribution configuration if it exists.
// Returns ErrDistributionConfigNotFound if the file doesn't exist.
// Returns error if config loading or validation fails.
func (m *ConfigManager) loadK3dConfig() (*k3dv1alpha5.SimpleConfig, error) {
	// Check if the file actually exists before trying to load it
	// This prevents validation against default configs during init
	_, err := os.Stat(m.Config.Spec.Cluster.DistributionConfig)
	if os.IsNotExist(err) {
		// File doesn't exist
		return nil, ErrDistributionConfigNotFound
	}

	k3dManager := k3dconfigmanager.NewConfigManager(m.Config.Spec.Cluster.DistributionConfig)

	config, err := k3dManager.LoadConfig(nil)
	if err != nil {
		// Propagate validation errors
		return nil, fmt.Errorf("failed to load K3d config: %w", err)
	}

	return config, nil
}
