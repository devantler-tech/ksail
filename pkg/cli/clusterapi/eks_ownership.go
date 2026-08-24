package clusterapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

// ErrEKSOwnershipEvidenceMissing reports a local API EKS mutation for a cluster with no target
// binding on disk. The binding is written when a create completes, so its absence means the target
// this action would mutate cannot be established — and an EKS delete cannot be undone.
var ErrEKSOwnershipEvidenceMissing = errors.New("no local KSail EKS target binding")

// ErrUnguardableFactory reports a provisioner factory that cannot carry the ownership guard. It
// fails the mutation rather than proceeding unguarded: a factory that silently drops the verifier
// would leave VerifyBeforeMutation a no-op, which is the gap this guard exists to close.
var ErrUnguardableFactory = errors.New("provisioner factory cannot carry the EKS ownership guard")

// ErrEKSOwnershipSelectorChanged reports that the AWS identity selector at capture no longer
// names the identity the create was pinned to. Create is a long async operation; a selector
// that was repointed mid-create would otherwise let capture freeze a same-named cluster in a
// different account. Capture refuses rather than bind that unrelated cluster.
var ErrEKSOwnershipSelectorChanged = errors.New(
	"AWS selector changed between EKS create and capture",
)

// ErrEKSOwnershipAccountChanged reports that the selector still has the same name at capture time,
// but now resolves to a different AWS account than it did before create began. Profile contents are
// mutable independently of the profile name, so selector equality alone cannot establish identity.
var ErrEKSOwnershipAccountChanged = errors.New(
	"AWS account changed between EKS create and capture",
)

var errEKSCreateAccountMissing = errors.New("AWS account ID is empty before EKS create")

// defaultEKSOwnershipTimeout bounds the whole ownership resolution: the AWS config load, the STS
// caller-identity query and the EKS DescribeCluster behind it.
//
// It exists because the mutation paths deliberately run on a context.WithoutCancel background
// context, so the request that triggered the action can never cancel this work. The AWS SDK applies
// no overall per-operation deadline of its own, so an unresponsive STS or EKS endpoint would
// otherwise leave the job pinned in Deleting/Updating with no way to dismiss it — the exact
// undismissable-row failure runDelete's idempotency handling already exists to avoid.
//
// 60s is generous enough for the SDK's default retries on a slow-but-working endpoint, and short
// enough that a wedged one surfaces as a failed job an operator can retry.
const defaultEKSOwnershipTimeout = 60 * time.Second

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

	// Bound every network call this guard makes. The caller's context is deliberately
	// uncancellable (see defaultEKSOwnershipTimeout), so without this a hung AWS endpoint pins the
	// job forever.
	ctx, cancel := context.WithTimeout(ctx, s.eksOwnershipTimeout)
	defer cancel()

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
		return nil, normalizeEKSVerificationError(err, name)
	}

	// Bound the carried verifier too, per invocation, and normalize its result the same way.
	//
	// This deadline ends with this function, and the provisioner re-checks identity at its own
	// mutation boundary using the context IT was given — which is the same uncancellable background
	// context. Handing the raw verifier onward would leave that second check unbounded, so a hung
	// STS or EKS endpoint could still pin the job in Deleting/Updating even though the first check
	// succeeded quickly. Wrapping is what makes the guarantee hold at both boundaries rather than
	// only at the first one.
	//
	// The normalization has to happen here as well, not only at the call above: a cluster can be
	// present when this guard verifies and gone by the time the provisioner re-checks — which is the
	// very race the second check exists to catch. Normalizing only the first boundary would leave
	// that window returning a raw eksclient error runDelete does not recognize, reaching the same
	// undismissable Failed row one boundary later.
	bounded := func(callCtx context.Context) error {
		callCtx, callCancel := context.WithTimeout(callCtx, s.eksOwnershipTimeout)
		defer callCancel()

		callErr := verifier(callCtx)
		if callErr == nil {
			return nil
		}

		return normalizeEKSVerificationError(callErr, name)
	}

	return &eksMutationGuard{resolution: resolution, verifier: bounded}, nil
}

// normalizeEKSVerificationError maps a verification failure caused by the cluster's absence onto
// clustererr.ErrClusterNotFound, and leaves every other failure exactly as it was.
//
// Absence is the one verification failure that must not fail closed. The guard exists to stop a
// mutation reaching an unrelated incarnation of the name; when no live cluster answers to it there
// is no incarnation to reach, so refusing protects nothing and costs the caller its idempotency.
// Reporting it as clustererr.ErrClusterNotFound lets runDelete's existing state-cleanup and
// idempotent-success branches run, instead of pinning a Failed row that cannot be dismissed and
// retaining completed-create state that then blocks recreating the name — the trap runDelete's own
// idempotency handling was written to avoid.
//
// A mismatch is the opposite case and stays fail-closed: a different live cluster answering to this
// name is exactly what must not be mutated.
//
// This is shared by both verification boundaries deliberately. Applying it at only one of them
// leaves the other reporting an error runDelete cannot recognize, which is the same failure the
// mapping exists to prevent.
func normalizeEKSVerificationError(err error, name string) error {
	if errors.Is(err, eksclient.ErrClusterNotFound) {
		return fmt.Errorf(
			"%w: %q has no live EKS cluster to verify ownership against: %w",
			clustererr.ErrClusterNotFound, name, err,
		)
	}

	return fmt.Errorf("verify immutable EKS ownership identity for %q: %w", name, err)
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

// eksCreateIdentity is the exact AWS identity an EKS create runs under: the credential-selection
// snapshot and variable names handed to the provisioner, plus the AWS account they resolved to
// immediately before provisioning began.
//
// It exists because the create provisioner and the capture that follows it used to resolve AWS
// independently. The provisioner resolves from the cluster spec's own variable names
// (resolveEKSCredentialOptions, over spec.Provider.AWS); the capture resolved from this backend's
// ambient resolver, which reads the canonical AWS_* names. Those are different sources whenever a
// spec names custom variables, so they can select different accounts with no Settings change at
// all — and the eksctl create is asynchronous, so the ambient selection can also change underneath
// a create that is still running.
//
// Getting this wrong does not fail closed. A capture resolved against the wrong account finds
// whatever same-named cluster lives there and records it as this cluster's immutable identity, so
// every later guarded delete, start or stop verifies successfully against an unrelated incarnation
// and mutates it. Pinning one snapshot before the create and reusing it for the capture is what
// makes the recorded identity the identity the create actually produced.
type eksCreateIdentity struct {
	selection  credentials.AWSResolution
	awsOptions v1alpha1.OptionsAWS
	accountID  string
}

// eksCreateAccountFunc resolves the AWS account selected by one credential snapshot. The default
// performs exactly one STS GetCallerIdentity call; the seam keeps local lifecycle tests hermetic.
type eksCreateAccountFunc func(
	ctx context.Context,
	selection credentials.AWSResolution,
) (string, error)

// newEKSCreateIdentity snapshots the AWS selection a create is about to run under. Account
// resolution is a separate, fallible step in resolveEKSCreateIdentity so selector-only refusal tests
// can construct the immutable value without AWS credentials.
func newEKSCreateIdentity(awsOptions v1alpha1.OptionsAWS) *eksCreateIdentity {
	return &eksCreateIdentity{
		selection:  credentials.ResolveAWS(credentials.NewAWSOptionsResolver(awsOptions)),
		awsOptions: awsOptions,
	}
}

// resolveEKSCreateIdentity snapshots the AWS selection and resolves its account before create begins.
//
// It resolves through the cluster spec's own variable names — the exact source
// resolveEKSCredentialOptions uses inside the provisioner — so the capture that follows records
// under the credentials the create actually used rather than under an independently resolved set.
//
// Taking both observations BEFORE the create closes the timing gap for mutable profile contents:
// selector names alone stay equal when a profile is rewritten to point at another account. The
// selection remains unfrozen so eksctl keeps its normal credential-chain behavior; the read-only STS
// lookup records only the account identifier needed to verify capture later.
func (s *Service) resolveEKSCreateIdentity(
	ctx context.Context,
	awsOptions v1alpha1.OptionsAWS,
) (*eksCreateIdentity, error) {
	identity := newEKSCreateIdentity(awsOptions)

	ctx, cancel := context.WithTimeout(ctx, s.eksOwnershipTimeout)
	defer cancel()

	accountID, err := s.resolveEKSCreateAccount(ctx, identity.selection)
	if err != nil {
		return nil, fmt.Errorf("resolve AWS account before EKS create: %w", err)
	}

	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, errEKSCreateAccountMissing
	}

	identity.accountID = accountID

	return identity, nil
}

func (s *Service) defaultEKSCreateAccount(
	ctx context.Context,
	selection credentials.AWSResolution,
) (string, error) {
	client, err := s.eksIdentityClientFor(ctx, selection.Region, selection)
	if err != nil {
		return "", err
	}

	accountID, err := client.CallerAccountID(ctx)
	if err != nil {
		return "", fmt.Errorf("get AWS caller account for EKS create: %w", err)
	}

	return accountID, nil
}

// eksCaptureFunc records the immutable AWS identity of a just-created EKS cluster, under the
// identity that create was pinned to. It is a seam so tests exercise the create wiring without AWS.
type eksCaptureFunc func(ctx context.Context, name string, identity *eksCreateIdentity) error

// captureEKSOwnership records the immutable identity of a cluster this backend just created.
//
// Without it the guard is unusable on the very clusters this API creates: runCreate persists only
// spec.json, and the EKS provisioner just runs eksctl, so nothing writes the ownership record
// NewVerifier requires. Every later delete/start/stop would then fail with the rebind error and the
// operator would have to run `ksail cluster eks-bind` by hand after every single create — a guard
// that blocks the path it is meant to protect.
//
// Capture performs read-only AWS queries and writes local state only; it mutates nothing in AWS.
func (s *Service) captureEKSOwnership(
	ctx context.Context,
	name string,
	identity *eksCreateIdentity,
) error {
	ctx, cancel := context.WithTimeout(ctx, s.eksOwnershipTimeout)
	defer cancel()

	return s.captureEKSIdentity(ctx, name, identity)
}

// defaultEKSCapture resolves the just-written binding and records the live identity under the exact
// credential snapshot the create was pinned to. It verifies both the selector and the AWS account:
// a same-named profile can be rewritten mid-create without changing selector equality.
func (s *Service) defaultEKSCapture(
	ctx context.Context,
	name string,
	identity *eksCreateIdentity,
) error {
	bound, err := boundEKSConfig(name)
	if err != nil {
		return err
	}

	if bound == nil {
		return fmt.Errorf(
			"%w for %q: the create completed but wrote no target binding to record an identity"+
				" against",
			ErrEKSOwnershipEvidenceMissing, name,
		)
	}

	// Refuse rather than fall back to an ambient resolution. Re-resolving here is the divergence
	// eksCreateIdentity exists to remove, and a capture recorded under credentials the create never
	// used is precisely the confident-but-wrong record that makes later guards authorize a mutation
	// against an unrelated cluster.
	if identity == nil {
		return fmt.Errorf(
			"%w for %q: the create pinned no AWS identity to record this cluster under",
			ErrEKSOwnershipEvidenceMissing, name,
		)
	}

	// Re-resolve the live selector through the same option-alias resolver the create used, then
	// refuse if it now names a different identity. Create is 15–20 minutes of async work; a
	// mid-create profile or access-key change would otherwise let FreezeAWS record a same-named
	// cluster in a different account as this cluster's immutable identity. Matching selectors
	// still freeze the pinned snapshot, not the live resolution, so capture stays bound to the
	// identity the create actually used.
	live := credentials.ResolveAWS(credentials.NewAWSOptionsResolver(identity.awsOptions))
	if !identity.selection.SelectorEquals(live) {
		return fmt.Errorf("%w for %q", ErrEKSOwnershipSelectorChanged, name)
	}

	// Freeze the create's own selection, for the region the create actually bound to. This is the
	// single resolution the recorded identity is read under, and it runs only after the live
	// selector still names that same identity.
	resolution, err := credentials.FreezeAWS(ctx, identity.selection, bound.Region)
	if err != nil {
		return fmt.Errorf("freeze AWS credentials for EKS ownership: %w", err)
	}

	client, err := s.eksIdentityClientFor(ctx, bound.Region, resolution)
	if err != nil {
		return err
	}

	err = captureEKSIdentityForCreate(
		ctx,
		client,
		name,
		bound.Region,
		// Record the variable names the create actually resolved through, so a later verification
		// resolves the same identity this capture observed. Recording the canonical defaults instead
		// would misdescribe every cluster whose spec names custom variables.
		credentials.AWSOptionsWithDefaults(identity.awsOptions),
		identity.accountID,
	)
	if err != nil {
		return fmt.Errorf("capture immutable EKS ownership identity: %w", err)
	}

	return nil
}

// captureEKSIdentityForCreate observes the capture-time account and cluster, refuses an account
// change before any state write, then persists the validated ownership record. Comparing accounts
// rather than credential bytes permits normal same-account key rotation.
func captureEKSIdentityForCreate(
	ctx context.Context,
	client eksidentity.Client,
	name, region string,
	awsOptions v1alpha1.OptionsAWS,
	createAccountID string,
) error {
	ownership, err := eksidentity.Observe(ctx, client, name, region)
	if err != nil {
		return fmt.Errorf("observe EKS identity after create: %w", err)
	}

	if ownership.AccountID != strings.TrimSpace(createAccountID) {
		return fmt.Errorf(
			"%w for %q: create account %q, capture account %q",
			ErrEKSOwnershipAccountChanged,
			name,
			strings.TrimSpace(createAccountID),
			ownership.AccountID,
		)
	}

	ownership.AWSOptions = awsOptions

	err = eksidentity.Persist(name, ownership.Region, ownership)
	if err != nil {
		return fmt.Errorf("persist EKS identity after create: %w", err)
	}

	return nil
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

	client, resolution, err := s.eksIdentityClient(ctx, name, region)
	if err != nil {
		return credentials.AWSResolution{}, nil, err
	}

	verifier, err := eksidentity.NewVerifier(client, name, region)
	if err != nil {
		return credentials.AWSResolution{}, nil, fmt.Errorf(
			"load immutable EKS ownership identity: %w", err,
		)
	}

	return resolution, verifier, nil
}

// eksIdentityClient freezes one AWS credential snapshot for a region and builds the read-only
// identity client against it. Capture and verification share this so both run under exactly the same
// resolved identity — a capture recorded under one and verified under another would fail forever.
//
// The snapshot resolves through the cluster's own ownership record where one exists, because capture
// persisted the variable names the create resolved through. Resolving the current selection instead
// would read a different identity — or no credentials at all — whenever the credentials that made
// the cluster are reachable only under those recorded aliases.
func (s *Service) eksIdentityClient(
	ctx context.Context,
	name, region string,
) (eksidentity.Client, credentials.AWSResolution, error) {
	selection := credentials.ResolveAWS(s.eksOwnershipResolver(name, region))

	resolution, err := credentials.FreezeAWS(ctx, selection, region)
	if err != nil {
		return nil, credentials.AWSResolution{}, fmt.Errorf(
			"freeze AWS credentials for EKS ownership: %w", err,
		)
	}

	client, err := s.eksIdentityClientFor(ctx, region, resolution)
	if err != nil {
		return nil, credentials.AWSResolution{}, err
	}

	return client, resolution, nil
}

// eksOwnershipResolver returns the resolver one cluster's lifecycle credentials must resolve through:
// the injected resolver layered under the variable names its ownership record captured.
//
// A record that is missing or predates the mapping schema falls back to the injected resolver
// untouched, deliberately. That keeps the injection seam intact for every unrecorded cluster, and it
// leaves eksidentity.NewVerifier's migration error as the authoritative, actionable failure for a
// legacy record rather than shadowing it with an opaque credential error here.
func (s *Service) eksOwnershipResolver(name, region string) credentials.Resolver {
	ownership, err := state.LoadEKSOwnershipState(name, region)
	if err != nil {
		return s.discoverer.Resolver
	}

	return credentials.NewRecordedAWSResolver(s.discoverer.Resolver, ownership.AWSOptions)
}

// eksIdentityClientFor builds the read-only identity client against an already-frozen snapshot.
//
// Capture uses this directly with the snapshot its create was pinned to, so the recorded identity
// is read under the credentials that made the cluster rather than under a second, independently
// resolved selection.
func (s *Service) eksIdentityClientFor(
	ctx context.Context,
	region string,
	resolution credentials.AWSResolution,
) (eksidentity.Client, error) {
	client, err := eksclient.NewClient(ctx, region, credentials.OptionsForFrozenAWSConfig(
		resolution,
		eksclient.WithAWSConfig,
		eksclient.WithCredentialValues,
		eksclient.RequireCredentialValues,
	)...)
	if err != nil {
		return nil, fmt.Errorf("create EKS identity client for ownership: %w", err)
	}

	return client, nil
}
