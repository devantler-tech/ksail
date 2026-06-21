package chat

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// errInvalidReasoningEffort is returned when a reasoning-effort value is not
// one of the accepted levels.
var errInvalidReasoningEffort = errors.New(
	"invalid reasoning effort: must be low, medium, or high",
)

// flags holds parsed flags for the chat command.
type flags struct {
	model           string
	reasoningEffort string
	streaming       bool
	timeout         time.Duration
	useTUI          bool
}

// parseChatFlags extracts and resolves chat command flags.
func parseChatFlags(cmd *cobra.Command) (flags, error) {
	modelFlag, _ := cmd.Flags().GetString("model")
	reasoningEffortFlag, _ := cmd.Flags().GetString("reasoning-effort")

	// Validate reasoning effort if provided via flag
	err := validateReasoningEffort(reasoningEffortFlag)
	if err != nil {
		return flags{}, err
	}

	streaming, _ := cmd.Flags().GetBool("streaming")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	useTUI, _ := cmd.Flags().GetBool("tui")

	// Load config values
	cfg := loadChatConfig()

	// Determine model: flag > config > "" (auto)
	model := resolveModel(modelFlag, cfg.Model)

	// Determine reasoning effort: flag > config > ""
	reasoningEffort, err := resolveReasoningEffort(reasoningEffortFlag, cfg.ReasoningEffort)
	if err != nil {
		return flags{}, err
	}

	return flags{
		model:           model,
		reasoningEffort: reasoningEffort,
		streaming:       streaming,
		timeout:         timeout,
		useTUI:          useTUI,
	}, nil
}

func validateReasoningEffort(effort string) error {
	if effort == "" {
		return nil
	}

	switch effort {
	case "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("%w: %q", errInvalidReasoningEffort, effort)
	}
}

func resolveModel(flagValue, configValue string) string {
	if flagValue != "" {
		return flagValue
	}

	if configValue != "" && configValue != "auto" {
		return configValue
	}

	return ""
}

func resolveReasoningEffort(flagValue, configValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	if configValue != "" {
		err := validateReasoningEffort(configValue)
		if err != nil {
			return "", fmt.Errorf("%w: %q (from config)", errInvalidReasoningEffort, configValue)
		}

		return configValue, nil
	}

	return "", nil
}

// chatConfig holds configuration values loaded from ksail.yaml.
type chatConfig struct {
	Model           string
	ReasoningEffort string
}

// loadChatConfig loads chat configuration from ksail.yaml.
// Returns empty strings if config doesn't exist or values are not set.
func loadChatConfig() chatConfig {
	// Try to load ksail.yaml from current directory
	configPath := "ksail.yaml"

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist or can't be read - use defaults
		return chatConfig{}
	}

	var config v1alpha1.Cluster

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		// Config exists but couldn't be parsed - ignore and use defaults
		return chatConfig{}
	}

	return chatConfig{
		Model:           config.Spec.Chat.Model,
		ReasoningEffort: config.Spec.Chat.ReasoningEffort,
	}
}
