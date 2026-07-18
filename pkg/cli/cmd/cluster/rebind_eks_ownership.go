package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
)

var (
	errEKSOwnershipRebindConfirmationRequired = errors.New(
		"EKS ownership rebind requires --yes after reviewing the displayed identity",
	)
	errEKSOwnershipRebindProvider = errors.New(
		"EKS ownership rebind requires provider AWS",
	)
)

// NewRebindEKSOwnershipCmd creates the explicit, non-destructive legacy-state migration path. The
// command performs only read-only AWS/eksctl queries, displays the selected immutable identity, and
// writes local state only after --yes. It ships hidden and default-off while the migration UX is
// evaluated; the long-lived mutation guard itself is always on.
//
//nolint:funlen // command construction keeps guard and action visibly paired.
func NewRebindEKSOwnershipCmd() *cobra.Command {
	var (
		confirmed bool
		observed  *state.EKSOwnershipState
	)

	var cmd *cobra.Command

	cmd = lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:   "eks-bind",
		Short: "Bind legacy EKS state to the current immutable AWS identity",
		Long: `Bind legacy local EKS state to the exact cluster selected by the current AWS credentials.

The command performs read-only AWS and eksctl queries, prints the account, ARN, region, and creation
time for review, and writes only local KSail state. Run once without --yes to review the target, then
repeat with --yes to confirm. It never deletes or scales AWS resources.`,
		TitleEmoji:   "🔐",
		TitleContent: "Rebind EKS ownership...",
		Activity:     "binding immutable ownership for",
		Success:      "EKS ownership identity rebound",
		Guard: func(ctx context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			if resolved.Provider != v1alpha1.ProviderAWS {
				return fmt.Errorf(
					"%w: got %s",
					errEKSOwnershipRebindProvider,
					resolved.Provider,
				)
			}

			identityClient, err := resolveAWSOwnershipTarget(ctx, resolved)
			if err != nil {
				return err
			}

			observed, err = eksidentity.Observe(
				ctx,
				identityClient,
				resolved.ClusterName,
				resolved.AWSRegion,
			)
			if err != nil {
				return fmt.Errorf("observe EKS ownership identity: %w", err)
			}

			notify.Infof(cmd.OutOrStdout(), "AWS account: %s", observed.AccountID)
			notify.Infof(cmd.OutOrStdout(), "cluster ARN: %s", observed.ClusterARN)
			notify.Infof(cmd.OutOrStdout(), "region: %s", observed.Region)
			notify.Infof(
				cmd.OutOrStdout(),
				"created at: %s",
				observed.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			)

			if !confirmed {
				return errEKSOwnershipRebindConfirmationRequired
			}

			return nil
		},
		Action: func(
			_ context.Context,
			_ clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			if observed == nil {
				return fmt.Errorf(
					"persist EKS ownership identity: %w",
					eksidentity.ErrInvalidLiveIdentity,
				)
			}

			return eksidentity.Persist(clusterName, observed.Region, observed)
		},
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}
	cmd.Flags().
		BoolVar(&confirmed, "yes", false, "confirm the displayed EKS identity and write local ownership state")
	_ = cmd.Flags().SetAnnotation(
		"yes", annotations.AnnotationConfirmFlag, []string{annotations.AnnotationValueTrue},
	)

	return experimental.Guard(cmd)
}
