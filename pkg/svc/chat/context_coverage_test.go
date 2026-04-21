package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSystemContextConfig(t *testing.T) {
	t.Parallel()

	cfg := chat.DefaultSystemContextConfig()

	assert.Contains(t, cfg.Identity, "KSail Assistant")
	assert.True(t, cfg.IncludeWorkingDirContext)
	assert.Equal(t, "ksail.yaml", cfg.ConfigFileName)
	assert.NotEmpty(t, cfg.Instructions)
	assert.Contains(t, cfg.Instructions, "<instructions>")
}

func TestBuildSystemSections(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections()

	// BuildSystemSections should return a non-nil map
	require.NotNil(t, sections)
}

func TestBuildSystemContext_ContainsIdentity(t *testing.T) {
	t.Parallel()

	ctx, err := chat.BuildSystemContext()
	require.NoError(t, err)

	assert.Contains(t, ctx, "KSail Assistant")
	assert.Contains(t, ctx, "<identity>")
	assert.Contains(t, ctx, "</identity>")
}

func TestBuildSystemContext_ContainsInstructions(t *testing.T) {
	t.Parallel()

	ctx, err := chat.BuildSystemContext()
	require.NoError(t, err)

	assert.Contains(t, ctx, "<instructions>")
	assert.Contains(t, ctx, "</instructions>")
}
