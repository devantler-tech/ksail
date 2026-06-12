package chat_test

import (
	"testing"

	chatui "github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRootCmd builds a minimal root command standing in for the real
// ksail root when exercising the system context builders.
func newTestRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ksail",
		Short: "KSail CLI",
		Long:  "KSail is an SDK for Kubernetes clusters.",
	}
}

func TestBuildSystemContext(t *testing.T) {
	t.Parallel()

	ctx, err := chatui.BuildSystemContext(chat.DefaultSystemContextConfig(newTestRootCmd()))
	require.NoError(t, err)
	assert.NotEmpty(t, ctx)
	assert.Contains(t, ctx, "<identity>")
}

func TestDefaultSystemContextConfig_CLIHelpFromRootCommand(t *testing.T) {
	t.Parallel()

	cfg := chat.DefaultSystemContextConfig(newTestRootCmd())

	assert.Contains(t, cfg.CLIHelp, "KSail is an SDK for Kubernetes clusters.")
	assert.Contains(t, cfg.CLIHelp, "Usage:")
}

func TestDefaultSystemContextConfig_NilRootCommand(t *testing.T) {
	t.Parallel()

	cfg := chat.DefaultSystemContextConfig(nil)

	assert.Empty(t, cfg.CLIHelp)
}
