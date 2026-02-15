package clustererr

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
)

// RunProviderOp executes a provider operation with a nil check and standardised error wrapping.
// The resolvedName must already be resolved by the caller (e.g. via distribution-specific
// name resolution logic).
func RunProviderOp(
	ctx context.Context,
	infraProvider provider.Provider,
	resolvedName string,
	operationName string,
	providerFunc func(ctx context.Context, clusterName string) error,
) error {
	if infraProvider == nil {
		return fmt.Errorf("%w for cluster '%s'", ErrProviderNotSet, resolvedName)
	}

	err := providerFunc(ctx, resolvedName)
	if err != nil {
		return fmt.Errorf("failed to %s cluster '%s': %w", operationName, resolvedName, err)
	}

	return nil
}
