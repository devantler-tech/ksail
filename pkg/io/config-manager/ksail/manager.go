package configmanager

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	mapstructure "github.com/go-viper/mapstructure/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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

	// Expand environment variables in all string fields.
	// This happens immediately after unmarshaling, before applying defaults
	// or making paths absolute, so that env vars can be used anywhere.
	m.Config.ExpandEnvVars()

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
