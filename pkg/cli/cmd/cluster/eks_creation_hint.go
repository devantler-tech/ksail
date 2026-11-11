package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

// adoptEKSCreationHint records ownership of a cluster that a failed `ksail cluster create` began
// building but that AWS reported only after KSail exited (ksail#8324). It runs only when no ownership
// record exists for the target and a failed-create hint does. The hint is untrusted: ownership is
// recorded only when the live cluster, read in the caller's account, carries exactly one of the
// creation tags kept for that account. Any other same-named cluster leaves the hint in place and the
// record missing, so the normal ownership check still refuses it. When AWS confirms that no such
// cluster exists, the caller account's attempts are forgotten. Persisting the record also forgets them.
func adoptEKSCreationHint(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
	client eksidentity.Client,
) error {
	clusterName := resolved.ClusterName
	region := strings.TrimSpace(resolved.AWSRegion)

	_, err := state.LoadEKSOwnershipState(clusterName, region)
	if !errors.Is(err, state.ErrEKSOwnershipStateNotFound) {
		// A record exists, or one is present but unusable: the ownership verifier owns that outcome.
		return nil
	}

	accountID, creationIDs, err := loadCallerEKSCreationAttempts(ctx, client, clusterName, region)
	if err != nil || len(creationIDs) == 0 {
		return err
	}

	ownership, err := eksidentity.ObserveCreationInAccount(
		ctx,
		client,
		accountID,
		clusterName,
		region,
		eksprovisioner.ClusterCreationTag,
		creationIDs,
	)

	switch {
	case errors.Is(err, eksclient.ErrClusterNotFound):
		return forgetEKSCreationAttempts(clusterName, region, accountID)
	case errors.Is(err, eksidentity.ErrCreationUnattributed):
		return nil
	case err != nil:
		return fmt.Errorf("check the failed create's attempt identifier against AWS: %w", err)
	}

	ownership.AWSOptions = credentials.AWSOptionsWithDefaults(resolved.AWSOpts)

	err = eksidentity.Persist(clusterName, ownership.Region, ownership)
	if err != nil {
		return fmt.Errorf("record ownership of the failed create's EKS cluster: %w", err)
	}

	return nil
}

// discardStaleEKSCreationHint forgets the caller account's kept attempts once the exact eksctl query
// has reported that the cluster does not exist and the EKS API confirms it in that account. Attempts
// kept for other accounts stay.
func discardStaleEKSCreationHint(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
	auth credentials.AWSResolution,
) error {
	clusterName := resolved.ClusterName
	region := strings.TrimSpace(resolved.AWSRegion)

	// Only a present hint is worth an AWS client; a damaged one fails in the caller-scoped load below.
	_, err := state.LoadEKSCreationHint(clusterName, region)
	if errors.Is(err, state.ErrEKSCreationHintNotFound) {
		return nil
	}

	eksIdentityClientFactoryState.RLock()
	factory := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.RUnlock()

	client, err := factory(ctx, region, auth)
	if err != nil {
		return fmt.Errorf(
			"create AWS client to check the failed create's attempt identifier: %w",
			err,
		)
	}

	accountID, creationIDs, err := loadCallerEKSCreationAttempts(ctx, client, clusterName, region)
	if err != nil || len(creationIDs) == 0 {
		return err
	}

	_, err = client.DescribeCluster(ctx, clusterName)
	if errors.Is(err, eksclient.ErrClusterNotFound) {
		return forgetEKSCreationAttempts(clusterName, region, accountID)
	}

	if err != nil {
		return fmt.Errorf("check the failed create's attempt identifier against AWS: %w", err)
	}

	return nil
}

// loadCallerEKSCreationAttempts resolves the caller's AWS account and returns the attempt identifiers
// the target's failed-create hint kept for that account. A missing hint, or one with no attempts for
// the caller's account, returns no identifiers and no error.
func loadCallerEKSCreationAttempts(
	ctx context.Context,
	client eksidentity.Client,
	clusterName, region string,
) (string, []string, error) {
	hint, err := state.LoadEKSCreationHint(clusterName, region)
	if errors.Is(err, state.ErrEKSCreationHintNotFound) {
		return "", nil, nil
	}

	if err != nil {
		return "", nil, fmt.Errorf("load the failed create's attempt identifier: %w", err)
	}

	accountID, err := client.CallerAccountID(ctx)
	if err != nil {
		return "", nil, fmt.Errorf(
			"resolve current AWS account for the failed create's attempt identifier: %w",
			err,
		)
	}

	accountID = strings.TrimSpace(accountID)

	return accountID, hint.CreationIDsFor(accountID), nil
}

func forgetEKSCreationAttempts(clusterName, region, accountID string) error {
	err := state.ForgetEKSCreationAttempts(clusterName, region, accountID)
	if err != nil {
		return fmt.Errorf("forget the failed create's attempt identifiers: %w", err)
	}

	return nil
}
