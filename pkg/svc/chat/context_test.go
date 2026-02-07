package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemContext(t *testing.T) {
	t.Parallel()

	ctx, err := chat.BuildSystemContext()
	require.NoError(t, err)
	assert.NotEmpty(t, ctx)
	assert.Contains(t, ctx, "<identity>")
}

func TestFindKSailExecutable(t *testing.T) {
	t.Parallel()

	result := chat.FindKSailExecutable()
	assert.IsType(t, "", result)
}
