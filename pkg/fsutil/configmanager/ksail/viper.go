package configmanager

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/spf13/viper"
)

const (
	// DefaultConfigFileName is the default configuration file name (without extension).
	DefaultConfigFileName = "ksail"
	// EnvPrefix is the prefix for environment variables.
	EnvPrefix = "KSAIL"
	// SuggestionsMinimumDistance represents the minimum levenshtein distance for command suggestions.
	SuggestionsMinimumDistance = 2
)

// InitializeViper creates a new Viper instance with basic KSail configuration settings.
// This function handles only the essential Viper setup and delegates specific concerns
// to other functions. Configuration priority is: defaults < config files < environment variables < flags.
// When configFilePath is non-empty, the exact file is used and directory traversal is skipped.
// The path is canonicalized (home-expanded, absolute, symlink-resolved) to prevent
// symlink-escape issues.
func InitializeViper(configFilePath string) (*viper.Viper, error) {
	viperInstance := viper.New()

	if configFilePath != "" {
		// Canonicalize the user-supplied path: expand ~, make absolute, resolve symlinks.
		expanded, err := fsutil.ExpandHomePath(configFilePath)
		if err != nil {
			return nil, fmt.Errorf("expanding config path %q: %w", configFilePath, err)
		}

		canonical, err := fsutil.EvalCanonicalPath(expanded)
		if err != nil {
			return nil, fmt.Errorf("canonicalizing config path %q: %w", configFilePath, err)
		}

		configFilePath = canonical
		// Use the explicit config file path — skip name/type/path discovery.
		// Still set config type so Viper can decode files without a .yaml/.yml extension.
		viperInstance.SetConfigFile(configFilePath)
		viperInstance.SetConfigType("yaml")
	} else {
		// Configure file settings first (highest precedence after flags/env)
		configureViperFileSettings(viperInstance)

		// Add standard configuration paths
		configureViperPaths(viperInstance)

		// Setup directory traversal for parent directories
		addParentDirectoriesToViperPaths(viperInstance)
	}

	// Setup environment variable handling (higher precedence than config files)
	configureViperEnvironment(viperInstance)

	return viperInstance, nil
}

// configureViperFileSettings sets up file-related configuration for Viper.
func configureViperFileSettings(v *viper.Viper) {
	v.SetConfigName(DefaultConfigFileName)
	v.SetConfigType("yaml")
}

// configureViperPaths adds default configuration search paths to Viper.
func configureViperPaths(viperInstance *viper.Viper) {
	// Get user home directory using os/user instead of $HOME
	usr, err := user.Current()
	if err == nil {
		viperInstance.AddConfigPath(filepath.Join(usr.HomeDir, ".ksail"))
	}

	viperInstance.AddConfigPath("/etc/ksail")
}

// configureViperEnvironment sets up environment variable handling for Viper.
// Uses AutomaticEnv() for automatic environment variable binding with explicit binding for nested fields.
func configureViperEnvironment(viperInstance *viper.Viper) {
	viperInstance.SetEnvPrefix(EnvPrefix)
	viperInstance.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viperInstance.AutomaticEnv()

	// Explicitly bind nested environment variables for better compatibility
	_ = viperInstance.BindEnv("metadata.name", "KSAIL_METADATA_NAME")
	_ = viperInstance.BindEnv("spec.cluster.distribution", "KSAIL_SPEC_DISTRIBUTION")
	_ = viperInstance.BindEnv("spec.workload.sourcedirectory", "KSAIL_SPEC_SOURCEDIRECTORY")
	_ = viperInstance.BindEnv("spec.cluster.connection.context", "KSAIL_SPEC_CONNECTION_CONTEXT")
	_ = viperInstance.BindEnv(
		"spec.cluster.connection.kubeconfig",
		"KSAIL_SPEC_CONNECTION_KUBECONFIG",
	)
	_ = viperInstance.BindEnv("spec.cluster.connection.timeout", "KSAIL_SPEC_CONNECTION_TIMEOUT")
	_ = viperInstance.BindEnv(
		"spec.cluster.omni.machineclass",
		"KSAIL_SPEC_CLUSTER_OMNI_MACHINECLASS",
	)
}

// addParentDirectoriesToViperPaths adds parent directories containing ksail.yaml to Viper's search paths.
// This enables directory traversal functionality similar to how Git finds .git directories.
func addParentDirectoriesToViperPaths(viperInstance *viper.Viper) {
	currentDir, err := filepath.Abs(".")
	if err != nil {
		return
	}

	// Walk up the directory tree and add each directory to Viper's search paths
	// but only if a ksail.yaml file actually exists in that directory.
	// No duplicate-detection map is needed: upward traversal always visits unique directories.
	for dir := currentDir; ; dir = filepath.Dir(dir) {
		_, statErr := os.Stat(filepath.Join(dir, "ksail.yaml"))
		if statErr == nil {
			viperInstance.AddConfigPath(dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
}
