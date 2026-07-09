package clustererr

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
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

// ResolveTargetName resolves name via resolveName and, when the result is empty, wraps
// notFoundErr as "no cluster name configured" — the target-name preamble shared by every cloud
// connector's Kubeconfig method (AKS, GKE, EKS) before they diverge on how they fetch the cluster.
func ResolveTargetName(
	resolveName func(string) string,
	name string,
	notFoundErr error,
) (string, error) {
	target := resolveName(name)
	if target == "" {
		return "", fmt.Errorf("%w: no cluster name configured", notFoundErr)
	}

	return target, nil
}

// WithResolvedTarget resolves name via ResolveTargetName and, on success, calls next with ctx and
// the resolved target, returning its result directly. next takes (ctx, target) — matching a
// connector's own "<Op>ForTarget(ctx, target)" method's signature exactly — so a Kubeconfig method
// can pass that method value straight through with no wrapping closure, and the "resolve name,
// check err, return the zero value" early-return that ResolveTargetName's own error would
// otherwise require at every call site is handled once here instead of being copy-pasted into
// each cloud connector (AKS, GKE, …).
func WithResolvedTarget[T any](
	ctx context.Context,
	resolveName func(string) string,
	name string,
	notFoundErr error,
	next func(ctx context.Context, target string) (T, error),
) (T, error) {
	target, err := ResolveTargetName(resolveName, name, notFoundErr)
	if err != nil {
		var zero T

		return zero, err
	}

	return next(ctx, target)
}
