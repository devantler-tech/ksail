package clusterdiscovery_test

import (
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errProviderListFailed is a sentinel used to assert ProviderError preserves the
// wrapped cause through Unwrap / errors.Is.
var errProviderListFailed = errors.New("connection refused")

func TestProviderErrorError(t *testing.T) {
	t.Parallel()

	err := clusterdiscovery.ProviderError{
		Provider: v1alpha1.ProviderHetzner,
		Err:      errProviderListFailed,
	}

	assert.Equal(t, "list Hetzner clusters: connection refused", err.Error())
}

func TestProviderErrorUnwrap(t *testing.T) {
	t.Parallel()

	err := clusterdiscovery.ProviderError{
		Provider: v1alpha1.ProviderDocker,
		Err:      errProviderListFailed,
	}

	// Unwrap returns the wrapped cause, so errors.Is can match through it.
	require.ErrorIs(t, err, errProviderListFailed)
	assert.Equal(t, errProviderListFailed, err.Unwrap())
}
