package clustererr_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			err:      clustererr.ErrClusterNotFound,
			contains: "cluster not found",
		},
		{
			name:     "ErrProviderNotSet",
			err:      clustererr.ErrProviderNotSet,
			contains: "infrastructure provider not set",
		},
		{
			name:     "ErrNoNodesFound",
			err:      clustererr.ErrNoNodesFound,
			contains: "no nodes found for cluster",
		},
		{
			name:     "ErrNotHetznerProvider",
			err:      clustererr.ErrNotHetznerProvider,
			contains: "infrastructure provider is not a Hetzner provider",
		},
		{
			name:     "ErrNoControlPlaneNodes",
			err:      clustererr.ErrNoControlPlaneNodes,
			contains: "no control-plane nodes found for cluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, tc.err)
			assert.Contains(t, tc.err.Error(), tc.contains)
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	errs := []error{
		clustererr.ErrClusterNotFound,
		clustererr.ErrProviderNotSet,
		clustererr.ErrNoNodesFound,
		clustererr.ErrNotHetznerProvider,
		clustererr.ErrNoControlPlaneNodes,
	}

	// Verify all errors are distinct
	for i, err1 := range errs {
		for j, err2 := range errs {
			if i != j {
				assert.NotErrorIs(t, err1, err2,
					"error %q should not match %q", err1, err2)
			}
		}
	}
}
