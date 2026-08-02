package clusterapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// ErrEKSOwnershipEvidenceMissing reports a local API EKS mutation for a cluster with no target
// binding on disk. The binding is written when a create completes, so its absence means the target
// this action would mutate cannot be established — and an EKS delete cannot be undone.
var ErrEKSOwnershipEvidenceMissing = errors.New("no local KSail EKS target binding")

// ErrUnguardableFactory reports a provisioner factory that cannot carry the ownership guard. It
// fails the mutation rather than proceeding unguarded: a factory that silently drops the verifier
// would leave VerifyBeforeMutation a no-op, which is the gap this guard exists to close.
var ErrUnguardableFactory = errors.New("provisioner factory cannot carry the EKS ownership guard")

// eksGuardFunc resolves the evidence-reading half of an EKS mutation guard: the region the cluster
// was bound to at create time, one frozen credential snapshot for it, and a verifier closed over
// the persisted immutable identity. It is a seam so tests exercise the wiring without AWS
// credentials or an on-disk eksctl config.
type eksGuardFunc func(
	ctx context.Context,
	name string,
) (credentials.AWSResolution, eksidentity.Verifier, error)

// eksGuardableFactory is a provisioner factory that accepts the guard. clusterprovisioner's
// DefaultFactory satisfies it; the interface keeps this package from depending on that concrete
// type and lets a test factory observe what the service handed it.
type eksGuardableFactory interface {
	WithEKSMutationGuard(
		resolution *credentials.AWSResolution,
		verifier eksidentity.Verifier,
	) clusterprovisioner.Factory
}

// eksMutationGuard pins one credential snapshot and the ownership verifier authorized by it. Both
// travel together into the factory so the provisioner re-checks the same identity at its own
// mutation boundary, rather than re-resolving an ambient one that may have changed since.
type eksMutationGuard struct {
	resolution credentials.AWSResolution
	verifier   eksidentity.Verifier
}

// resolveEKSMutationGuard returns the guard an EKS mutation must carry, or nil for actions that
// need none (every non-EKS distribution, and EKS create).
func (s *Service) resolveEKSMutationGuard(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	name string,
) (*eksMutationGuard, error) {
	if distribution != v1alpha1.DistributionEKS {
		return nil, nil //nolint:nilnil // no guard needed is a normal, non-error outcome.
	}

	resolution, verifier, err := s.resolveEKSGuard(ctx, name)
	if err != nil {
		return nil, err
	}

	// A nil verifier is refused rather than carried. eksidentity.VerifyBeforeMutation reads nil as
	// "nothing to check" — correct for creates and non-EKS callers, and a silent fail-open here,
	// because the guard would travel into the provisioner and authorize the mutation while checking
	// nothing. The absence of a check is not a passing check.
	if verifier == nil {
		return nil, fmt.Errorf(
			"%w for %q: ownership resolution returned no verifier",
			ErrEKSOwnershipEvidenceMissing, name,
		)
	}

	// Verify once here so the refusal surfaces before any provisioner work starts, and hand the
	// same verifier onward so the provisioner re-checks at the narrowest boundary. This mirrors the
	// standalone CLI path, which also verifies during its guard and reuses the verifier afterwards.
	err = verifier(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify immutable EKS ownership identity for %q: %w", name, err)
	}

	return &eksMutationGuard{resolution: resolution, verifier: verifier}, nil
}

// applyEKSMutationGuard returns the factory the action should use, carrying the guard when one was
// resolved. An unguardable factory is an error rather than a silent pass-through.
func applyEKSMutationGuard(
	factory clusterprovisioner.Factory,
	guard *eksMutationGuard,
) (clusterprovisioner.Factory, error) {
	if guard == nil {
		return factory, nil
	}

	guardable, ok := factory.(eksGuardableFactory)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrUnguardableFactory, factory)
	}

	return guardable.WithEKSMutationGuard(&guard.resolution, guard.verifier), nil
}

// defaultEKSGuard reads the target the cluster was bound to when it was created, freezes one AWS
// credential snapshot for that region, and returns a verifier closed over the persisted immutable
// identity.
//
// The region comes from that binding and never from the region selected now: resolving it from the
// current selection is precisely the redirect this guards against, and the identity check would
// then run against a same-named cluster elsewhere.
//
// Freezing matters as much as verifying: the factory refuses a verifier that is not paired with a
// frozen resolution, because a verifier proving one identity while the provisioner independently
// re-resolves another would authorize a mutation nothing checked.
func (s *Service) defaultEKSGuard(
	ctx context.Context,
	name string,
) (credentials.AWSResolution, eksidentity.Verifier, error) {
	bound, err := boundEKSConfig(name)
	if err != nil {
		return credentials.AWSResolution{}, nil, err
	}

	if bound == nil {
		return credentials.AWSResolution{}, nil, fmt.Errorf(
			"%w for %q: a mutation must resolve its target from the binding written when the"+
				" cluster was created, and there is none. Run `ksail cluster eks-bind --name %s`"+
				" to record the region it was created in",
			ErrEKSOwnershipEvidenceMissing, name, name,
		)
	}

	region := bound.Region
	selection := credentials.ResolveAWS(s.discoverer.Resolver)

	resolution, err := credentials.FreezeAWS(ctx, selection, region)
	if err != nil {
		return credentials.AWSResolution{}, nil, fmt.Errorf(
			"freeze AWS credentials for EKS ownership verification: %w", err,
		)
	}

	options := credentials.OptionsForFrozenAWSConfig(
		resolution,
		eksclient.WithAWSConfig,
		eksclient.WithCredentialValues,
		eksclient.RequireCredentialValues,
	)

	client, err := eksclient.NewClient(ctx, region, options...)
	if err != nil {
		return credentials.AWSResolution{}, nil, fmt.Errorf(
			"create EKS identity client for ownership verification: %w", err,
		)
	}

	verifier, err := eksidentity.NewVerifier(client, name, region)
	if err != nil {
		return credentials.AWSResolution{}, nil, fmt.Errorf(
			"load immutable EKS ownership identity: %w", err,
		)
	}

	return resolution, verifier, nil
}
