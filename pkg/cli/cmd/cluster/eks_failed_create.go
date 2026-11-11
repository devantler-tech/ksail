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

	ownership, err := observeFailedEKSCreate(ctx, clusterCtx, clusterName, region)
	if errors.Is(err, eksclient.ErrClusterNotFound) ||
		errors.Is(err, eksidentity.ErrCreationUnattributed) {
		// Either the create failed before AWS accepted a control plane, or the cluster with this name
		// is not the one this attempt began building (for example it already existed). Neither is
		// this attempt's to record.
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

func observeFailedEKSCreate(
	ctx context.Context,
	clusterCtx *localregistry.Context,
	clusterName, region string,
) (*state.EKSOwnershipState, error) {
	eksIdentityClientFactoryState.RLock()
	factory := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.RUnlock()

	client, err := factory(ctx, region, *clusterCtx.AWSResolution)
	if err != nil {
		return nil, fmt.Errorf("create AWS client: %w", err)
	}

	ownership, err := eksidentity.ObserveCreation(
		ctx,
		client,
		clusterName,
		region,
		eksprovisioner.ClusterCreationTag,
		clusterCtx.EKSConfig.CreationID,
	)
	if err != nil {
		return nil, fmt.Errorf("observe partially created EKS cluster: %w", err)
	}

	return ownership, nil
}
