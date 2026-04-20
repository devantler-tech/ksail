package helm_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplateOnlyClient(t *testing.T) {
	t.Parallel()

	client, err := helm.NewTemplateOnlyClient()

	require.NoError(t, err)
	require.NotNil(t, client)

	// Verify it implements the Interface.
	var _ helm.Interface = client
	assert.Implements(t, (*helm.Interface)(nil), client)
}
