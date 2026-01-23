package configmanager

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	"github.com/devantler-tech/ksail/v5/pkg/io/config-manager/loader"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/talos"
	"github.com/devantler-tech/ksail/v5/pkg/io/validator"
	ksailvalidator "github.com/devantler-tech/ksail/v5/pkg/io/validator/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	mapstructure "github.com/go-viper/mapstructure/v2"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ErrDistributionConfigNotFound is returned when a distribution config file is not found.
var ErrDistributionConfigNotFound = errors.New("distribution config file not found")

// ConfigManager implements configuration management for KSail v1alpha1.Cluster configurations.
type ConfigManager struct {
	Viper              *viper.Viper
	fieldSelectors     []FieldSelector[v1alpha1.Cluster]
	Config             *v1alpha1.Cluster
	DistributionConfig *clusterprovisioner.DistributionConfig
	configLoaded       bool
	configFileFound    bool
	Writer             io.Writer
	command            *cobra.Command
	// localRegistryExplicit tracks if config explicitly set the local registry behavior
	localRegistryExplicit bool
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

// Load loads the configuration with the specified options.
// Returns the loaded config (either freshly loaded or previously cached) and an error if loading failed.
// Returns nil config on error.
// Configuration priority: defaults < config files < environment variables < flags.
func (m *ConfigManager) Load(opts configmanagerinterface.LoadOptions) (*v1alpha1.Cluster, error) {
	return m.loadConfigWithOptions(
		opts.Timer,
		opts.Silent,
		opts.IgnoreConfigFile,
		opts.SkipValidation,
	)
}

// IsConfigFileFound returns true if a configuration file was found during LoadConfig.
// This should only be called after LoadConfig has been called.
func (m *ConfigManager) IsConfigFileFound() bool {
	return m.configFileFound
}

// shouldSkipValidation determines if validation should be skipped.
// Validation is skipped when:
//   - Explicitly requested via skipValidation parameter.
//   - In silent mode with no config file found (probing for config existence).
func (m *ConfigManager) shouldSkipValidation(silent, skipValidation bool) bool {
	return skipValidation || (silent && !m.configFileFound)
}

// loadConfigWithOptions is the internal implementation with silent and skip validation options.
func (m *ConfigManager) loadConfigWithOptions(
	tmr timer.Timer,
	silent bool,
	ignoreConfigFile bool,
	skipValidation bool,
) (*v1alpha1.Cluster, error) {
	// Check if config was already loaded before outputting any messages
	if m.configLoaded {
		return m.Config, nil
	}

	if !silent {
		m.notifyLoadingStart()
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
	// Pass ignoreConfigFile so path resolution knows not to make paths absolute
	// when no config file is being used (e.g., during init command)
	err := m.unmarshalWithFlagOverrides(ignoreConfigFile)
	if err != nil {
		return nil, err
	}

	// Run validation unless skipped
	if !m.shouldSkipValidation(silent, skipValidation) {
		err = m.validateAndFinalizeConfig()
		if err != nil {
			return nil, err
		}
	}

	if !silent {
		m.notifyLoadingComplete(tmr)
	}

	m.configLoaded = true

	return m.Config, nil
}

// unmarshalWithFlagOverrides unmarshals config and applies all overrides and defaults.
// When ignoreConfigFile is true, paths are kept relative since they'll be joined with
// an explicit output directory later (e.g., during init command scaffolding).
func (m *ConfigManager) unmarshalWithFlagOverrides(ignoreConfigFile bool) error {
	flagOverrides := m.captureChangedFlagValues()

	err := m.unmarshalAndApplyDefaults(ignoreConfigFile)
	if err != nil {
		return err
	}

	err = m.applyFlagOverrides(flagOverrides)
	if err != nil {
		return err
	}

	m.applyGitOpsAwareDefaults(flagOverrides)
	m.applyDistributionConfigDefaults()

	return nil
}

// validateAndFinalizeConfig validates the config and loads distribution-specific configuration.
func (m *ConfigManager) validateAndFinalizeConfig() error {
	err := m.validateConfig()
	if err != nil {
		return err
	}

	// Load distribution config after validation (reuses cached configs from validation)
	return m.loadAndCacheDistributionConfig()
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

func (m *ConfigManager) unmarshalAndApplyDefaults(ignoreConfigFile bool) error {
	decoderConfig := func(dc *mapstructure.DecoderConfig) {
		dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			metav1DurationDecodeHook(),
		)
	}

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

	// Track whether local-registry was explicitly set in config
	m.localRegistryExplicit = m.Viper.IsSet("spec.cluster.localRegistry.registry") ||
		m.Viper.IsSet("local-registry")

	// Apply field selector defaults for empty fields
	for _, fieldSelector := range m.fieldSelectors {
		fieldPtr := fieldSelector.Selector(m.Config)
		if fieldPtr != nil && isFieldEmpty(fieldPtr) {
			setFieldValue(fieldPtr, fieldSelector.DefaultValue)
		}
	}

	// Make kubeconfig path absolute relative to config file directory
	err = m.makeKubeconfigPathAbsolute()
	if err != nil {
		return fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}

	// Make source directory path absolute relative to config file directory.
	// Skip when ignoreConfigFile is true (e.g., during init command scaffolding)
	// because the path will be joined with an explicit output directory later.
	if !ignoreConfigFile {
		err = m.makeSourceDirectoryAbsolute()
		if err != nil {
			return fmt.Errorf("failed to resolve source directory path: %w", err)
		}
	}

	return nil
}

// makePathAbsolute converts a relative path to an absolute path.
// If the path is empty, starts with ~/, or is already absolute, it returns the path unchanged.
// Otherwise, the path is made absolute relative to the config file's directory,
// or the current working directory if no config file was used.
func (m *ConfigManager) makePathAbsolute(relativePath string) (string, error) {
	if relativePath == "" {
		return relativePath, nil
	}

	// If it starts with ~/, that will be handled by ExpandHomePath later
	// If it's already absolute, no change needed
	if strings.HasPrefix(relativePath, "~/") || filepath.IsAbs(relativePath) {
		return relativePath, nil
	}

	// Path is relative - make it absolute
	var basePath string

	if m.configFileFound && m.Viper.ConfigFileUsed() != "" {
		// Make it relative to the config file's directory
		configDir := filepath.Dir(m.Viper.ConfigFileUsed())
		basePath = configDir
	} else {
		// No config file - use current working directory
		var err error

		basePath, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	return filepath.Join(basePath, relativePath), nil
}

// makeKubeconfigPathAbsolute converts the kubeconfig path to an absolute path.
// If the path is relative, it's made absolute relative to the config file's directory.
// If the path starts with ~/, it's expanded to the user's home directory.
// If no config file was used, the path is made absolute relative to the current working directory.
func (m *ConfigManager) makeKubeconfigPathAbsolute() error {
	absPath, err := m.makePathAbsolute(m.Config.Spec.Cluster.Connection.Kubeconfig)
	if err != nil {
		return err
	}

	m.Config.Spec.Cluster.Connection.Kubeconfig = absPath

	return nil
}

// makeSourceDirectoryAbsolute converts the source directory path to an absolute path.
// If the path is relative, it's made absolute relative to the config file's directory.
// If the path starts with ~/, it's expanded to the user's home directory.
// If no config file was used, the path is made absolute relative to the current working directory.
// This ensures scaffolded clusters can find their k8s/ directory regardless of where the command is run from.
func (m *ConfigManager) makeSourceDirectoryAbsolute() error {
	absPath, err := m.makePathAbsolute(m.Config.Spec.Workload.SourceDirectory)
	if err != nil {
		return err
	}

	m.Config.Spec.Workload.SourceDirectory = absPath

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

	// Apply default local registry when GitOps engine is configured and no explicit registry was set
	if !m.wasLocalRegistryExplicit(flagOverrides) && m.gitOpsEngineSelected() {
		// Default to localhost:5050 when GitOps is enabled but no registry specified
		if m.Config.Spec.Cluster.LocalRegistry.Registry == "" {
			m.Config.Spec.Cluster.LocalRegistry.Registry = "localhost:5050"
		}
	}
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
		errorMessages := loader.FormatValidationErrorsMultiline(result)
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "%s",
			Args:    []any{errorMessages},
			Writer:  m.Writer,
		})

		m.writeValidationWarnings(result)

		// Return validation summary error instead of full error stack
		return loader.NewValidationSummaryError(len(result.Errors), len(result.Warnings))
	}

	m.writeValidationWarnings(result)

	return nil
}

// writeValidationWarnings outputs all validation warnings to the configured writer.
func (m *ConfigManager) writeValidationWarnings(result *validator.ValidationResult) {
	warnings := loader.FormatValidationWarnings(result)
	for _, warning := range warnings {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: warning,
			Writer:  m.Writer,
		})
	}
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
	case v1alpha1.DistributionVanilla:
		return "kind.yaml"
	case v1alpha1.DistributionK3s:
		return "k3d.yaml"
	case v1alpha1.DistributionTalos:
		return "talos"
	default:
		return ""
	}
}

func distributionConfigIsOppositeDefault(current string, distribution v1alpha1.Distribution) bool {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return current == expectedDistributionConfigName(v1alpha1.DistributionK3s) ||
			current == expectedDistributionConfigName(v1alpha1.DistributionTalos)
	case v1alpha1.DistributionK3s:
		return current == expectedDistributionConfigName(v1alpha1.DistributionVanilla) ||
			current == expectedDistributionConfigName(v1alpha1.DistributionTalos)
	case v1alpha1.DistributionTalos:
		return current == expectedDistributionConfigName(v1alpha1.DistributionVanilla) ||
			current == expectedDistributionConfigName(v1alpha1.DistributionK3s)
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
func (m *ConfigManager) createValidatorForDistribution() (*ksailvalidator.Validator, error) {
	// Only load distribution config for Cilium CNI validation
	if m.Config.Spec.Cluster.DistributionConfig == "" ||
		m.Config.Spec.Cluster.CNI != v1alpha1.CNICilium {
		return ksailvalidator.NewValidator(), nil
	}

	return m.createDistributionValidator()
}

// createDistributionValidator creates a validator based on the configured distribution.
func (m *ConfigManager) createDistributionValidator() (*ksailvalidator.Validator, error) {
	switch m.Config.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return m.createKindValidator()
	case v1alpha1.DistributionK3s:
		return m.createK3dValidator()
	case v1alpha1.DistributionTalos:
		return m.createTalosValidator()
	default:
		return ksailvalidator.NewValidator(), nil
	}
}

// createKindValidator loads Kind config and returns a validator.
func (m *ConfigManager) createKindValidator() (*ksailvalidator.Validator, error) {
	kindConfig, err := m.loadKindConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return nil, err
	}

	if kindConfig != nil {
		return ksailvalidator.NewValidatorForKind(kindConfig), nil
	}

	return ksailvalidator.NewValidator(), nil
}

// createK3dValidator loads K3d config and returns a validator.
func (m *ConfigManager) createK3dValidator() (*ksailvalidator.Validator, error) {
	k3dConfig, err := m.loadK3dConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return nil, err
	}

	if k3dConfig != nil {
		return ksailvalidator.NewValidatorForK3d(k3dConfig), nil
	}

	return ksailvalidator.NewValidator(), nil
}

// createTalosValidator loads Talos config and returns a validator.
func (m *ConfigManager) createTalosValidator() (*ksailvalidator.Validator, error) {
	talosConfig, err := m.loadTalosConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return nil, err
	}

	if talosConfig != nil {
		return ksailvalidator.NewValidatorForTalos(talosConfig), nil
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

	config, err := kindManager.Load(configmanagerinterface.LoadOptions{})
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

	config, err := k3dManager.Load(configmanagerinterface.LoadOptions{})
	if err != nil {
		// Propagate validation errors
		return nil, fmt.Errorf("failed to load K3d config: %w", err)
	}

	return config, nil
}

// loadTalosConfig loads the Talos distribution configuration if the patches directory exists.
// Returns ErrDistributionConfigNotFound if the directory doesn't exist.
// Returns error if config loading or validation fails.
func (m *ConfigManager) loadTalosConfig() (*talosconfigmanager.Configs, error) {
	// For Talos, DistributionConfig points to the patches directory (e.g., "talos")
	patchesDir := m.Config.Spec.Cluster.DistributionConfig
	if patchesDir == "" {
		patchesDir = talosconfigmanager.DefaultPatchesDir
	}

	// Check if the directory exists
	info, err := os.Stat(patchesDir)
	if os.IsNotExist(err) {
		return nil, ErrDistributionConfigNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("failed to stat talos patches directory: %w", err)
	}

	if !info.IsDir() {
		return nil, ErrDistributionConfigNotFound
	}

	// Get cluster name from context or use default.
	// Uses ResolveClusterName helper which handles the "admin@<cluster-name>" pattern.
	clusterName := talosconfigmanager.ResolveClusterName(m.Config, nil)

	talosManager := talosconfigmanager.NewConfigManager(
		patchesDir,
		clusterName,
		"", // Use default Kubernetes version
		"", // Use default network CIDR
	)

	config, err := talosManager.Load(configmanagerinterface.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load Talos config: %w", err)
	}

	return config, nil
}

// loadAndCacheDistributionConfig loads the distribution-specific configuration based on
// the cluster's distribution type and caches it in the ConfigManager.
// This allows commands to access the distribution config via cfgManager.DistributionConfig.
// If distribution config file doesn't exist, an empty DistributionConfig is created.
func (m *ConfigManager) loadAndCacheDistributionConfig() error {
	m.DistributionConfig = &clusterprovisioner.DistributionConfig{}

	switch m.Config.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return m.cacheKindConfig()
	case v1alpha1.DistributionK3s:
		return m.cacheK3dConfig()
	case v1alpha1.DistributionTalos:
		return m.cacheTalosConfig()
	default:
		return nil
	}
}

func (m *ConfigManager) cacheKindConfig() error {
	kindConfig, err := m.loadKindConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return fmt.Errorf("failed to load Kind distribution config: %w", err)
	}

	if kindConfig == nil {
		// Create a valid default Kind config with required TypeMeta fields
		kindConfig = &kindv1alpha4.Cluster{
			TypeMeta: kindv1alpha4.TypeMeta{
				Kind:       "Cluster",
				APIVersion: "kind.x-k8s.io/v1alpha4",
			},
		}
	}

	m.DistributionConfig.Kind = kindConfig

	return nil
}

func (m *ConfigManager) cacheK3dConfig() error {
	k3dConfig, err := m.loadK3dConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return fmt.Errorf("failed to load K3d distribution config: %w", err)
	}

	if k3dConfig == nil {
		// Create a valid default K3d config with required TypeMeta fields
		k3dConfig = k3dconfigmanager.NewK3dSimpleConfig("", "", "")
	}

	m.DistributionConfig.K3d = k3dConfig

	return nil
}

func (m *ConfigManager) cacheTalosConfig() error {
	talosConfig, err := m.loadTalosConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return fmt.Errorf("failed to load Talos distribution config: %w", err)
	}

	if talosConfig == nil {
		// Create a valid default Talos config with required bundle.
		// When metrics-server is enabled on Talos, we need to add the kubelet-csr-approver
		// as an extraManifest so it's installed during bootstrap. Without this,
		// metrics-server cannot validate kubelet TLS certificates (missing IP SANs).
		patches := m.getDefaultTalosPatches()

		talosConfig, err = talosconfigmanager.NewDefaultConfigsWithPatches(patches)
		if err != nil {
			return fmt.Errorf("failed to create default Talos config: %w", err)
		}
	}

	m.DistributionConfig.Talos = talosConfig

	return nil
}

// getDefaultTalosPatches returns patches that should be applied to the default Talos config
// based on the current cluster configuration.
func (m *ConfigManager) getDefaultTalosPatches() []talosconfigmanager.Patch {
	var patches []talosconfigmanager.Patch

	// When metrics-server is enabled on Talos, we need two patches:
	// 1. Enable kubelet certificate rotation (rotate-server-certificates: true)
	// 2. Install kubelet-serving-cert-approver via extraManifests to approve the CSRs
	//
	// Note: We use alex1989hu/kubelet-serving-cert-approver for Talos because it provides
	// a single manifest URL suitable for extraManifests during bootstrap. For non-Talos
	// distributions, postfinance/kubelet-csr-approver is used via Helm post-bootstrap.
	//
	// See: https://docs.siderolabs.com/kubernetes-guides/monitoring-and-observability/deploy-metrics-server/
	if m.Config.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled {
		// Patch 1: Enable kubelet certificate rotation
		kubeletCertRotationPatch := talosconfigmanager.Patch{
			Path:  "kubelet-cert-rotation",
			Scope: talosconfigmanager.PatchScopeCluster,
			Content: []byte(`machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "true"
`),
		}
		patches = append(patches, kubeletCertRotationPatch)

		// Patch 2: Install kubelet-serving-cert-approver during bootstrap
		kubeletCSRApproverPatch := talosconfigmanager.Patch{
			Path:  "kubelet-csr-approver-extramanifest",
			Scope: talosconfigmanager.PatchScopeCluster,
			Content: []byte(`cluster:
  extraManifests:
    - ` + talosgenerator.KubeletServingCertApproverManifestURL + `
`),
		}
		patches = append(patches, kubeletCSRApproverPatch)
	}

	return patches
}
