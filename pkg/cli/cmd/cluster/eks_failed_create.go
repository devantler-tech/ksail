package cluster

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
)

// failedCreateOwnershipTimeout bounds the AWS reads that record ownership after a failed create.
const failedCreateOwnershipTimeout = time.Minute

// newEKSCreationID returns a fresh, unguessable create-attempt identifier.
//
//nolint:gochecknoglobals // injectable identifier source for offline lifecycle tests.
var newEKSCreationID = rand.Text

// assignEKSCreationID gives this create attempt a fresh random identifier. The EKS provisioner tags
// the cluster with it, which is the only evidence a failed create later accepts that a cluster with
// the configured name is the one this attempt began building.
func assignEKSCreationID(clusterCtx *localregistry.Context) {
	if clusterCtx == nil || clusterCtx.ClusterCfg == nil || clusterCtx.EKSConfig == nil ||
		clusterCtx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return
	}

	clusterCtx.EKSConfig.CreationID = newEKSCreationID()
}

// retainFailedEKSCreateOwnership keeps a cluster that this create attempt brought into existence
// deletable after the create failed part-way (ksail#7591). The ownership record is written only when
// AWS reports a cluster in the caller's own account and region that carries this attempt's creation
// tag. The original create error is always returned; a failure to record ownership is joined to it.
func retainFailedEKSCreateOwnership(
	cmd *cobra.Command,
	clusterCtx *localregistry.Context,
	createErr error,
) error {
	if !hasEKSCreationAttempt(clusterCtx) {
		return createErr
	}

	clusterName := strings.TrimSpace(clusterCtx.EKSConfig.Name)
	region := strings.TrimSpace(clusterCtx.EKSConfig.Region)

	// An interrupted create (Ctrl-C, deadline) is exactly when a half-built cluster is most likely, so
	// the read-only ownership observation runs detached from the cancelled command context, bounded
	// by its own deadline.
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(cmd.Context()),
		failedCreateOwnershipTimeout,
	)
	defer cancel()

	client, err := newFailedCreateIdentityClient(ctx, clusterCtx, region)

	var accountID string
	if err == nil {
		// The account is read once and reused, so a later failing account lookup can never cost
		// the attempt its hint.
		accountID, err = client.CallerAccountID(ctx)
	}

	if err != nil {
		return errors.Join(
			createErr,
			fmt.Errorf("record ownership of partially created EKS cluster: %w", err),
		)
	}

	accountID = strings.TrimSpace(accountID)
	creationID := clusterCtx.EKSConfig.CreationID

	ownership, err := observeFailedEKSCreate(
		ctx, client, accountID, clusterName, region,
		failedCreateCandidateIDs(clusterName, region, accountID, creationID),
	)
	if errors.Is(err, eksclient.ErrClusterNotFound) {
		// AWS does not report the cluster yet. An interrupted create can still bring it into
		// existence after KSail exits, so this attempt's identifier is kept as a local hint that a
		// later lifecycle command checks against the live cluster's creation tag (ksail#8324).
		return keepFailedEKSCreateHint(cmd, clusterName, region, accountID, creationID, createErr)
	}

	if errors.Is(err, eksidentity.ErrCreationUnattributed) {
		// The cluster with this name is not one this attempt or an earlier kept attempt began
		// building (for example it already existed), so it is not this attempt's to record.
		return createErr
	}

	if err == nil {
		ownership.AWSOptions = credentials.AWSOptionsWithDefaults(
			clusterCtx.ClusterCfg.Spec.Provider.AWS,
		)
		err = eksidentity.Persist(clusterName, ownership.Region, ownership)
	}

	if err != nil {
		return errors.Join(
			createErr,
			fmt.Errorf("record ownership of partially created EKS cluster: %w", err),
		)
	}

	notify.Infof(
		cmd.ErrOrStderr(),
		"recorded ownership of partially created EKS cluster %q; run `ksail cluster delete` to remove it",
		clusterName,
	)

	return createErr
}

// hasEKSCreationAttempt reports whether clusterCtx is an EKS create carrying an attempt identifier and
// the frozen credentials needed to read the cluster back.
func hasEKSCreationAttempt(clusterCtx *localregistry.Context) bool {
	return clusterCtx != nil && clusterCtx.ClusterCfg != nil &&
		clusterCtx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS &&
		clusterCtx.EKSConfig != nil && clusterCtx.AWSResolution != nil &&
		strings.TrimSpace(clusterCtx.EKSConfig.CreationID) != ""
}

// failedCreateCandidateIDs returns this attempt's identifier followed by every earlier unresolved
// attempt kept for the same account. A retry whose create fails because an earlier attempt's cluster
// appeared in the meantime can then still record that cluster, but only when its live creation tag
// matches one of these identifiers (ksail#8324).
func failedCreateCandidateIDs(clusterName, region, accountID, creationID string) []string {
	candidates := []string{creationID}

	hint, err := state.LoadEKSCreationHint(clusterName, region)
	if err != nil || hint.AccountID != accountID {
		return candidates
	}

	return append(candidates, hint.CreationIDs...)
}

// keepFailedEKSCreateHint adds this attempt's creation identifier to the hint for a cluster AWS did
// not report yet, keyed to the caller's account and the cluster's name and region. Earlier unresolved
// attempts stay in the hint. The hint never authorizes anything by itself. The original create error
// is always returned; a failure to keep the hint is joined to it.
func keepFailedEKSCreateHint(
	cmd *cobra.Command,
	clusterName, region, accountID, creationID string,
	createErr error,
) error {
	err := state.RecordEKSCreationAttempt(clusterName, region, accountID, creationID)
	if err != nil {
		return errors.Join(
			createErr,
			fmt.Errorf("keep the failed EKS create's attempt identifier: %w", err),
		)
	}

	notify.Infof(
		cmd.ErrOrStderr(),
		"kept this create attempt's identifier for EKS cluster %q; if AWS finishes creating it, "+
			"`ksail cluster delete` can remove it",
		clusterName,
	)

	return createErr
}

// newFailedCreateIdentityClient builds the read-only AWS client for the create's frozen credentials.
func newFailedCreateIdentityClient(
	ctx context.Context,
	clusterCtx *localregistry.Context,
	region string,
) (eksidentity.Client, error) {
	eksIdentityClientFactoryState.RLock()
	factory := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.RUnlock()

	client, err := factory(ctx, region, *clusterCtx.AWSResolution)
	if err != nil {
		return nil, fmt.Errorf("create AWS client: %w", err)
	}

	return client, nil
}

// observeFailedEKSCreate reads the cluster back in the caller's account and returns its identity only
// when it carries one of the candidate creation tags.
func observeFailedEKSCreate(
	ctx context.Context,
	client eksidentity.Client,
	accountID, clusterName, region string,
	creationIDs []string,
) (*state.EKSOwnershipState, error) {
	ownership, err := eksidentity.ObserveCreationInAccount(
		ctx,
		client,
		accountID,
		clusterName,
		region,
		eksprovisioner.ClusterCreationTag,
		creationIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("observe partially created EKS cluster: %w", err)
	}

	return ownership, nil
}
