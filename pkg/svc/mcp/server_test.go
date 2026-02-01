package mcp_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/mcp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	cfg := mcp.DefaultConfig(rootCmd, "1.0.0")

	assert.Equal(t, "ksail-mcp", cfg.Name)
	assert.Equal(t, "1.0.0", cfg.Version)
	assert.Same(t, rootCmd, cfg.RootCmd)
	assert.NotNil(t, cfg.Logger)
	assert.Empty(t, cfg.WorkingDirectory)
}

func TestNewServer(t *testing.T) {
	t.Parallel()

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

	cfg := mcp.ServerConfig{
		Name:    "test-server",
		Version: "0.1.0",
		RootCmd: rootCmd,
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestNewServer_WithWorkingDirectory(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
	}

	cfg := mcp.ServerConfig{
		Name:             "test-server",
		Version:          "0.1.0",
		RootCmd:          rootCmd,
		WorkingDirectory: "/tmp/test",
	}

	server, err := mcp.NewServer(cfg)

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestServerConfig_Fields(t *testing.T) {
	t.Parallel()

	rootCmd := &cobra.Command{Use: "test"}

	cfg := mcp.ServerConfig{
		Name:             "my-server",
		Version:          "2.0.0",
		RootCmd:          rootCmd,
		WorkingDirectory: "/custom/dir",
	}

	assert.Equal(t, "my-server", cfg.Name)
	assert.Equal(t, "2.0.0", cfg.Version)
	assert.Same(t, rootCmd, cfg.RootCmd)
	assert.Equal(t, "/custom/dir", cfg.WorkingDirectory)
}
