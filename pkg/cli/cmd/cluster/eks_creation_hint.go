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
// recorded only when the caller's account is the one the hint was kept for and the live cluster
// carries exactly one of the hint's creation tags. Any other same-named cluster leaves the hint in place and
// the record missing, so the normal ownership check still refuses it. When AWS confirms that no such
// cluster exists, the hint is removed. Persisting the record also removes the hint.
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

	hint, matches, err := loadCallerEKSCreationHint(ctx, client, clusterName, region)
	if err != nil || !matches {
		return err
	}

	ownership, err := eksidentity.ObserveCreationInAccount(
		ctx,
		client,
		hint.AccountID,
		clusterName,
		region,
		eksprovisioner.ClusterCreationTag,
		hint.CreationIDs,
	)

	switch {
	case errors.Is(err, eksclient.ErrClusterNotFound):
		return discardEKSCreationHint(clusterName, region)
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

// discardStaleEKSCreationHint removes the failed-create hint once the exact eksctl query has reported
// that the cluster does not exist and the EKS API confirms it in the account the hint was kept for.
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

	_, matches, err := loadCallerEKSCreationHint(ctx, client, clusterName, region)
	if err != nil || !matches {
		return err
	}

	_, err = client.DescribeCluster(ctx, clusterName)
	if errors.Is(err, eksclient.ErrClusterNotFound) {
		return discardEKSCreationHint(clusterName, region)
	}

	if err != nil {
		return fmt.Errorf("check the failed create's attempt identifier against AWS: %w", err)
	}

	return nil
}

// loadCallerEKSCreationHint loads the failed-create hint for the target and reports whether it was
// kept for the caller's AWS account. A missing hint is not an error.
func loadCallerEKSCreationHint(
	ctx context.Context,
	client eksidentity.Client,
	clusterName, region string,
) (*state.EKSCreationHint, bool, error) {
	hint, err := state.LoadEKSCreationHint(clusterName, region)
	if errors.Is(err, state.ErrEKSCreationHintNotFound) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("load the failed create's attempt identifier: %w", err)
	}

	accountID, err := client.CallerAccountID(ctx)
	if err != nil {
		return nil, false, fmt.Errorf(
			"resolve current AWS account for the failed create's attempt identifier: %w",
			err,
		)
	}

	return hint, strings.TrimSpace(accountID) == strings.TrimSpace(hint.AccountID), nil
}

func discardEKSCreationHint(clusterName, region string) error {
	err := state.DeleteEKSCreationHint(clusterName, region)
	if err != nil {
		return fmt.Errorf("remove the failed create's attempt identifier: %w", err)
	}

	return nil
}
