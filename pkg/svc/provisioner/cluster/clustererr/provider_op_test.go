package clustererr_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errProviderOpConnectionRefused = errors.New("connection refused")
	errProviderOpDeadlineExceeded  = errors.New("deadline exceeded")
	errProviderOpUnexpectedName    = errors.New("unexpected cluster name")
)

// allErrors returns every sentinel error in the clustererr package.
//
//nolint:funlen // Sentinel coverage list is easiest to scan one item per line.
func allErrors() []struct {
	name     string
	err      error
	contains string
} {
	return []struct {
		name     string
		err      error
		contains string
	}{
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
		{
			name:     "ErrUnsupportedDistribution",
			err:      clustererr.ErrUnsupportedDistribution,
			contains: "unsupported distribution",
		},
		{
			name:     "ErrUnsupportedProvider",
			err:      clustererr.ErrUnsupportedProvider,
			contains: "unsupported provider",
		},
		{
			name:     "ErrMissingDistributionConfig",
			err:      clustererr.ErrMissingDistributionConfig,
			contains: "missing distribution config",
		},
		{
			name:     "ErrRecreationRequired",
			err:      clustererr.ErrRecreationRequired,
			contains: "cluster recreation required",
		},
		{name: "ErrConfigNil", err: clustererr.ErrConfigNil, contains: "config is nil"},
		{
			name:     "ErrNoProviderConfigured",
			err:      clustererr.ErrNoProviderConfigured,
			contains: "no provider configured to get node IPs",
		},
		{
			name:     "ErrDockerClientNotConfigured",
			err:      clustererr.ErrDockerClientNotConfigured,
			contains: "docker client not configured",
		},
		{
			name:     "ErrClusterDoesNotExist",
			err:      clustererr.ErrClusterDoesNotExist,
			contains: "cluster does not exist",
		},
		{
			name:     "ErrTalosConfigRequired",
			err:      clustererr.ErrTalosConfigRequired,
			contains: "TalosConfig required",
		},
		{
			name:     "ErrClusterNotFoundInDistributions",
			err:      clustererr.ErrClusterNotFoundInDistributions,
			contains: "cluster not found in any distribution",
		},
		{
			name:     "ErrCreateNotSupported",
			err:      clustererr.ErrCreateNotSupported,
			contains: "create not supported",
		},
		{name: "ErrUpgradeSkipped", err: clustererr.ErrUpgradeSkipped, contains: "upgrade skipped"},
		{
			name:     "ErrUpgraderNotSupported",
			err:      clustererr.ErrUpgraderNotSupported,
			contains: "provisioner does not support version upgrades",
		},
		{
			name:     "ErrVersionUndetermined",
			err:      clustererr.ErrVersionUndetermined,
			contains: "cannot determine running version",
		},
	}
}

func TestAllErrorVariables(t *testing.T) {
	t.Parallel()

	for _, tc := range allErrors() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, tc.err)
			assert.Contains(t, tc.err.Error(), tc.contains)
		})
	}
}

func TestAllErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	entries := allErrors()
	errs := make([]error, len(entries))

	for i, e := range entries {
		errs[i] = e.err
	}

	for i, err1 := range errs {
		for j, err2 := range errs {
			if i != j {
				assert.NotErrorIs(t, err1, err2,
					"error %q should not match %q", err1, err2)
			}
		}
	}
}

//nolint:funlen // Table-driven provider-op scenarios are clearer inline.
func TestRunProviderOp(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name          string
		provider      provider.Provider
		resolvedName  string
		operationName string
		providerFunc  func(ctx context.Context, clusterName string) error
		wantErr       bool
		wantSentinel  error
		wantContains  string
	}

	tests := []testCase{
		{
			name:          "nil provider returns ErrProviderNotSet",
			provider:      nil,
			resolvedName:  "my-cluster",
			operationName: "start",
			providerFunc: func(_ context.Context, _ string) error {
				return nil
			},
			wantErr:      true,
			wantSentinel: clustererr.ErrProviderNotSet,
			wantContains: "my-cluster",
		},
		{
			name:          "successful provider operation returns nil",
			provider:      provider.NewMockProvider(),
			resolvedName:  "test-cluster",
			operationName: "create",
			providerFunc: func(_ context.Context, _ string) error {
				return nil
			},
			wantErr: false,
		},
		{
			name:          "failed provider operation returns wrapped error",
			provider:      provider.NewMockProvider(),
			resolvedName:  "fail-cluster",
			operationName: "delete",
			providerFunc: func(_ context.Context, _ string) error {
				return errProviderOpConnectionRefused
			},
			wantErr:      true,
			wantContains: "failed to delete cluster 'fail-cluster'",
		},
		{
			name:          "nil provider error message includes cluster name",
			provider:      nil,
			resolvedName:  "special-cluster",
			operationName: "stop",
			providerFunc: func(_ context.Context, _ string) error {
				return nil
			},
			wantErr:      true,
			wantSentinel: clustererr.ErrProviderNotSet,
			wantContains: "special-cluster",
		},
		{
			name:          "error wrapping preserves original error",
			provider:      provider.NewMockProvider(),
			resolvedName:  "wrap-cluster",
			operationName: "upgrade",
			providerFunc: func(_ context.Context, _ string) error {
				return fmt.Errorf("timeout: %w", errProviderOpDeadlineExceeded)
			},
			wantErr:      true,
			wantContains: "failed to upgrade cluster 'wrap-cluster'",
		},
		{
			name:          "providerFunc receives resolved name",
			provider:      provider.NewMockProvider(),
			resolvedName:  "name-check",
			operationName: "verify",
			providerFunc: func(_ context.Context, clusterName string) error {
				if clusterName != "name-check" {
					return fmt.Errorf("%w: %s", errProviderOpUnexpectedName, clusterName)
				}

				return nil
			},
			wantErr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			err := clustererr.RunProviderOp(
				ctx,
				testCase.provider,
				testCase.resolvedName,
				testCase.operationName,
				testCase.providerFunc,
			)

			if !testCase.wantErr {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)

			if testCase.wantSentinel != nil {
				require.ErrorIs(t, err, testCase.wantSentinel)
			}

			if testCase.wantContains != "" {
				assert.Contains(t, err.Error(), testCase.wantContains)
			}
		})
	}
}

func TestRunProviderOpContextPropagation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prov := provider.NewMockProvider()

	err := clustererr.RunProviderOp(ctx, prov, "ctx-cluster", "start",
		func(ctx context.Context, _ string) error {
			return ctx.Err()
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start cluster 'ctx-cluster'")
}
