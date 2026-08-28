package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSystemContextConfig(t *testing.T) {
	t.Parallel()

	cfg := chat.DefaultSystemContextConfig(newTestRootCmd())

	assert.Contains(t, cfg.Identity, "KSail Assistant")
	assert.True(t, cfg.IncludeWorkingDirContext)
	assert.Equal(t, "ksail.yaml", cfg.ConfigFileName)
	assert.NotEmpty(t, cfg.Instructions)
	assert.Contains(t, cfg.Instructions, "<instructions>")
}

func TestBuildSystemSections(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(newTestRootCmd())

	// BuildSystemSections should return a non-nil map
	require.NotNil(t, sections)
}

func TestBuildSystemSectionsForAPIProviderUsesCompactContext(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSectionsForProvider(
		newTestRootCmd(),
		chat.ResolvedProvider{Name: v1alpha1.AIProviderOllama},
	)
	custom := sections[copilot.SectionCustomInstructions].Content

	assert.NotContains(t, custom, "<documentation>")
	assert.Contains(t, custom, "<cli_help>")
	assert.Contains(t, custom, "<instructions>")
}

func TestBuildSystemSectionsForCopilotRetainsDocumentation(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSectionsForProvider(
		newTestRootCmd(),
		chat.ResolvedProvider{Name: v1alpha1.AIProviderCopilot},
	)

	assert.Contains(t, sections[copilot.SectionCustomInstructions].Content, "<documentation>")
}

func TestBuildSystemContext_ContainsIdentity(t *testing.T) {
	t.Parallel()

	ctx, err := chat.BuildSystemContext(chat.DefaultSystemContextConfig(newTestRootCmd()))
	require.NoError(t, err)

	assert.Contains(t, ctx, "KSail Assistant")
	assert.Contains(t, ctx, "<identity>")
	assert.Contains(t, ctx, "</identity>")
}

func TestBuildSystemContext_ContainsInstructions(t *testing.T) {
	t.Parallel()

	ctx, err := chat.BuildSystemContext(chat.DefaultSystemContextConfig(newTestRootCmd()))
	require.NoError(t, err)

	assert.Contains(t, ctx, "<instructions>")
	assert.Contains(t, ctx, "</instructions>")
}
