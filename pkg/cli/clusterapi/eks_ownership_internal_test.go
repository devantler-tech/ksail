package clusterapi

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheProductionFactoryCanCarryTheGuard closes the gap every other test in this package leaves
// open: they all inject a fake factory, and a fake satisfies eksGuardableFactory because it was
// written to. Nothing in them touches the factory the shipped binary builds.
//
// That matters because applyEKSMutationGuard fails closed on a factory it cannot guard. If
// DefaultFactory ever stopped satisfying the interface, every real EKS mutation would be refused
// with ErrUnguardableFactory while this package's tests stayed green — the failure would surface
// only to an operator, on a cluster.
func TestTheProductionFactoryCanCarryTheGuard(t *testing.T) {
	t.Parallel()

	production := defaultProductionFactory(t)

	guarded, err := applyEKSMutationGuard(production, &eksMutationGuard{
		resolution: credentials.AWSResolution{},
		verifier:   func(context.Context) error { return nil },
	})
	require.NoError(t, err, "the production factory could not carry the EKS mutation guard")

	typed, ok := guarded.(clusterprovisioner.DefaultFactory)
	require.True(t, ok, "guarding the production factory changed its concrete type")
	assert.NotNil(t, typed.AWSOwnershipVerifier, "the verifier did not reach the factory")
	assert.NotNil(t, typed.AWSResolution, "the frozen resolution did not reach the factory")
}

// TestGuardingTheFactoryDoesNotMutateTheOriginal pins the value receiver on
// WithEKSMutationGuard. A pointer receiver would leave one action's identity attached to a shared
// factory, so the next, unrelated action would be authorized by an identity nobody verified for it.
func TestGuardingTheFactoryDoesNotMutateTheOriginal(t *testing.T) {
	t.Parallel()

	production := defaultProductionFactory(t)

	_, err := applyEKSMutationGuard(production, &eksMutationGuard{
		resolution: credentials.AWSResolution{},
		verifier:   func(context.Context) error { return nil },
	})
	require.NoError(t, err)

	original, ok := production.(clusterprovisioner.DefaultFactory)
	require.True(t, ok)
	assert.Nil(t, original.AWSOwnershipVerifier,
		"guarding one action left the verifier on the shared factory")
	assert.Nil(t, original.AWSResolution,
		"guarding one action left the credential snapshot on the shared factory")
}

// TestAnUnguardableFactoryIsRefused pins the fail-closed direction: a factory that cannot carry the
// guard must fail the mutation rather than silently run it unguarded.
func TestAnUnguardableFactoryIsRefused(t *testing.T) {
	t.Parallel()

	_, err := applyEKSMutationGuard(unguardableFactory{}, &eksMutationGuard{
		resolution: credentials.AWSResolution{},
		verifier:   func(context.Context) error { return nil },
	})

	require.ErrorIs(t, err, ErrUnguardableFactory)
}

// TestNoGuardLeavesTheFactoryUntouched is the scope control for the two tests above: a create, and
// every non-EKS action, must reach the same factory they always did.
func TestNoGuardLeavesTheFactoryUntouched(t *testing.T) {
	t.Parallel()

	original := unguardableFactory{}

	same, err := applyEKSMutationGuard(original, nil)
	require.NoError(t, err, "an unguarded action was refused")
	assert.Equal(t, original, same)
}

// defaultProductionFactory builds the factory the shipped binary uses for a distribution whose
// config needs no on-disk state, so the assertion is about the real type rather than a stand-in.
func defaultProductionFactory(t *testing.T) clusterprovisioner.Factory {
	t.Helper()

	factory, err := defaultFactory(v1alpha1.DistributionVanilla, "guard-shape-probe")
	require.NoError(t, err)

	return factory
}

// unguardableFactory is a factory that deliberately does NOT implement the guard interface.
type unguardableFactory struct{}

func (unguardableFactory) Create(
	context.Context,
	*v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return nil, nil, nil
}

var _ eksidentity.Verifier = func(context.Context) error { return nil }
