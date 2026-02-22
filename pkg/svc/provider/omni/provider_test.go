package omni_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider(t *testing.T) {
	t.Parallel()

	t.Run("WithNilClient", func(t *testing.T) {
		t.Parallel()

		prov := omni.NewProvider(nil)

		require.NotNil(t, prov)
		assert.False(t, prov.IsAvailable())
	})
}

func TestIsAvailable(t *testing.T) {
	t.Parallel()

	t.Run("WithNilClient", func(t *testing.T) {
		t.Parallel()

		prov := omni.NewProvider(nil)

		assert.False(t, prov.IsAvailable())
	})
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrEndpointRequired", func(t *testing.T) {
		t.Parallel()
		require.Error(t, omni.ErrEndpointRequired)
		assert.Contains(t, omni.ErrEndpointRequired.Error(), "endpoint")
	})

	t.Run("ErrServiceAccountKeyRequired", func(t *testing.T) {
		t.Parallel()
		require.Error(t, omni.ErrServiceAccountKeyRequired)
		assert.Contains(t, omni.ErrServiceAccountKeyRequired.Error(), "service account key")
	})
}
