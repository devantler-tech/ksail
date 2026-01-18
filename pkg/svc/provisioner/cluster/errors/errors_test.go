package clustererrors_test

import (
	"errors"
	"testing"

	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/stretchr/testify/assert"
)

func TestErrorVariables(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		err      error
		contains string
	}

	tests := []testCase{
		{
			name:     "ErrClusterNotFound",
			err:      clustererrors.ErrClusterNotFound,
			contains: "cluster not found",
		},
		{
			name:     "ErrProviderNotSet",
			err:      clustererrors.ErrProviderNotSet,
			contains: "infrastructure provider not set",
		},
		{
			name:     "ErrNoNodesFound",
			err:      clustererrors.ErrNoNodesFound,
			contains: "no nodes found for cluster",
		},
		{
			name:     "ErrNotHetznerProvider",
			err:      clustererrors.ErrNotHetznerProvider,
			contains: "infrastructure provider is not a Hetzner provider",
		},
		{
			name:     "ErrNoControlPlaneNodes",
			err:      clustererrors.ErrNoControlPlaneNodes,
			contains: "no control-plane nodes found for cluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.NotNil(t, tc.err)
			assert.Contains(t, tc.err.Error(), tc.contains)
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	errs := []error{
		clustererrors.ErrClusterNotFound,
		clustererrors.ErrProviderNotSet,
		clustererrors.ErrNoNodesFound,
		clustererrors.ErrNotHetznerProvider,
		clustererrors.ErrNoControlPlaneNodes,
	}

	// Verify all errors are distinct
	for i, err1 := range errs {
		for j, err2 := range errs {
			if i != j {
				assert.False(t, errors.Is(err1, err2),
					"error %q should not match %q", err1, err2)
			}
		}
	}
}
