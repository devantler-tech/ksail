package cluster

import (
	"context"
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
	"github.com/spf13/cobra"
)

// eksCreateBaseline records what AWS reported for the target cluster name immediately before
// eksctl started creating it. Only a definite "no such cluster" observation makes a cluster found
// after a failed create attributable to this attempt; an unknown answer or a pre-existing cluster
// never does.
// failedCreateOwnershipTimeout bounds the AWS reads that record ownership after a failed create.
const failedCreateOwnershipTimeout = time.Minute

type eksCreateBaseline struct {
	absent bool
}

// observeEKSCreateBaseline asks AWS, under the credential snapshot frozen for this create, whether
// the target cluster already exists. It never fails the create: any answer other than a definite
// "not found" leaves the baseline unknown, which keeps the previous behaviour of retaining nothing
// after a failed create.
func observeEKSCreateBaseline(
	ctx context.Context,
	clusterCtx *localregistry.Context,
) eksCreateBaseline {
	client, clusterName, region, applicable, err := eksCreateIdentityClient(ctx, clusterCtx)
	if !applicable || err != nil {
		return eksCreateBaseline{absent: false}
	}

	_, err = eksidentity.Observe(ctx, client, clusterName, region)

	return eksCreateBaseline{absent: errors.Is(err, eksclient.ErrClusterNotFound)}
}

// retainFailedEKSCreateOwnership keeps a cluster that this create attempt brought into existence
// deletable after the create failed part-way (ksail#7591). The ownership record is written only
// when AWS reported no such cluster before eksctl started and now reports one in the caller's own
// account and region: that cluster can only have been created during this attempt. The original
// create error is always returned unchanged; a failure to record ownership is joined to it.
func retainFailedEKSCreateOwnership(
	cmd *cobra.Command,
	clusterCtx *localregistry.Context,
	baseline eksCreateBaseline,
	createErr error,
) error {
	if !baseline.absent {
		return createErr
	}

	// An interrupted create (Ctrl-C, deadline) is exactly when a half-built cluster is most likely, so
	// the read-only ownership observation runs detached from the cancelled command context, bounded
	// by its own deadline.
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(cmd.Context()),
		failedCreateOwnershipTimeout,
	)
	defer cancel()

	client, clusterName, region, applicable, err := eksCreateIdentityClient(ctx, clusterCtx)
	if !applicable {
		return createErr
	}

	if err != nil {
		return errors.Join(
			createErr,
			fmt.Errorf("record ownership of partially created EKS cluster: %w", err),
		)
	}

	ownership, err := eksidentity.Observe(ctx, client, clusterName, region)
	if errors.Is(err, eksclient.ErrClusterNotFound) {
		// The create failed before AWS accepted a control plane: there is nothing to clean up.
		return createErr
	}

	if err != nil {
		return errors.Join(
			createErr,
			fmt.Errorf("record ownership of partially created EKS cluster: %w", err),
		)
	}

	ownership.AWSOptions = credentials.AWSOptionsWithDefaults(
		clusterCtx.ClusterCfg.Spec.Provider.AWS,
	)

	err = eksidentity.Persist(clusterName, ownership.Region, ownership)
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

// eksCreateIdentityClient builds the read-only AWS identity client for the cluster being created,
// using the same credential snapshot the provisioner received. It reports applicable=false for
// non-EKS distributions and whenever the inputs needed to bind an identity are missing.
func eksCreateIdentityClient(
	ctx context.Context,
	clusterCtx *localregistry.Context,
) (eksidentity.Client, string, string, bool, error) {
	if clusterCtx == nil || clusterCtx.ClusterCfg == nil ||
		clusterCtx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS ||
		clusterCtx.EKSConfig == nil || clusterCtx.AWSResolution == nil {
		return nil, "", "", false, nil
	}

	clusterName := strings.TrimSpace(clusterCtx.EKSConfig.Name)
	if clusterName == "" {
		return nil, "", "", false, nil
	}

	eksIdentityClientFactoryState.RLock()
	factory := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.RUnlock()

	region := strings.TrimSpace(clusterCtx.EKSConfig.Region)

	client, err := factory(ctx, region, *clusterCtx.AWSResolution)
	if err != nil {
		return nil, "", "", true, fmt.Errorf("create AWS client: %w", err)
	}

	return client, clusterName, region, true, nil
}
