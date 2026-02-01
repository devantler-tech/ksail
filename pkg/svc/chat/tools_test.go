package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetKSailTools(t *testing.T) {
	t.Parallel()

	// Create a minimal root command for testing
	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}
	subCmd := &cobra.Command{
		Use:   "test",
		Short: "Test command",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(subCmd)

	tools := chat.GetKSailTools(rootCmd, nil)

	require.NotNil(t, tools)
	assert.NotEmpty(t, tools)
}

func TestGetKSailToolMetadata(t *testing.T) {
	t.Parallel()

	// Create a minimal root command for testing
	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}
	subCmd := &cobra.Command{
		Use:   "test",
		Short: "Test command",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	rootCmd.AddCommand(subCmd)

	tools, metadata := chat.GetKSailToolMetadata(rootCmd, nil)

	require.NotNil(t, tools)
	require.NotNil(t, metadata)
	assert.NotEmpty(t, tools)
}
