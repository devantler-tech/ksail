package eksprovisioner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
)

func clusterUpdateID(update *ekstypes.Update) *string {
	if update == nil {
		return nil
	}

	return update.Id
}

func validateVersionUpdate(update *ekstypes.Update, updateID, target string) error {
	if update == nil || updateID == "" || aws.ToString(update.Id) != updateID ||
		update.Type != ekstypes.UpdateTypeVersionUpdate {
		return eksclient.ErrInvalidClusterUpdate
	}

	err := validateUpdateTarget(update.Params, target)
	if err != nil {
		return err
	}

	switch update.Status {
	case ekstypes.UpdateStatusInProgress, ekstypes.UpdateStatusSuccessful:
		if len(update.Errors) != 0 {
			return eksclient.ErrInvalidClusterUpdate
		}

		return nil
	case ekstypes.UpdateStatusFailed, ekstypes.UpdateStatusCancelled:
		return fmt.Errorf("%w: update %s status %s: %s", eksclient.ErrInvalidClusterUpdate,
			updateID, update.Status, formatUpdateErrors(update.Errors))
	default:
		return fmt.Errorf(
			"%w: update %s has unknown status %q",
			eksclient.ErrInvalidClusterUpdate,
			updateID,
			update.Status,
		)
	}
}

func validateUpdateTarget(params []ekstypes.UpdateParam, target string) error {
	versions := 0

	for _, param := range params {
		if param.Type != ekstypes.UpdateParamTypeVersion {
			continue
		}

		versions++

		version, err := eksclient.NormalizeControlPlaneVersion(aws.ToString(param.Value))
		if err != nil || version != target {
			return eksclient.ErrInvalidClusterUpdate
		}
	}

	if versions != 1 {
		return eksclient.ErrInvalidClusterUpdate
	}

	return nil
}

func formatUpdateErrors(details []ekstypes.ErrorDetail) string {
	messages := make([]string, 0, len(details))
	for _, detail := range details {
		messages = append(messages, fmt.Sprintf(
			"%s: %s (resources: %s)",
			detail.ErrorCode,
			aws.ToString(detail.ErrorMessage),
			strings.Join(detail.ResourceIds, ", "),
		))
	}

	return strings.Join(messages, "; ")
}

func (p *UpgradableProvisioner) waitForControlPlaneUpgrade(
	ctx context.Context,
	api AWSClusterVersionAPI,
	expected controlPlaneSnapshot,
	target, updateID string,
) error {
	ticker := time.NewTicker(controlPlaneUpgradePollInterval)
	defer ticker.Stop()

	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("wait for EKS update %s: %w", updateID, err)
		}

		done, err := p.controlPlaneUpgradeComplete(ctx, api, expected, target, updateID)
		if err != nil {
			return err
		}

		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for EKS update %s: %w", updateID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *UpgradableProvisioner) controlPlaneUpgradeComplete(
	ctx context.Context,
	api AWSClusterVersionAPI,
	expected controlPlaneSnapshot,
	target, updateID string,
) (bool, error) {
	update, err := api.DescribeClusterUpdate(ctx, p.name, updateID)
	if err != nil {
		return false, fmt.Errorf("poll EKS version update: %w", err)
	}

	err = validateVersionUpdate(update, updateID, target)
	if err != nil {
		return false, err
	}

	if update.Status != ekstypes.UpdateStatusSuccessful {
		return false, nil
	}

	actual, err := p.readControlPlane(ctx, api, p.name)
	if err != nil {
		return false, err
	}

	done, err := controlPlaneAtTarget(expected, actual, target)
	if err != nil || !done {
		return false, err
	}

	err = eksidentity.VerifyBeforeMutation(ctx, p.ownershipVerifier)
	if err != nil {
		return false, fmt.Errorf("verify upgraded EKS ownership: %w", err)
	}

	return true, nil
}

func controlPlaneAtTarget(expected, actual controlPlaneSnapshot, target string) (bool, error) {
	if actual.arn != expected.arn || !actual.createdAt.Equal(expected.createdAt) {
		return false, eksidentity.ErrIdentityMismatch
	}

	if actual.status != ekstypes.ClusterStatusActive &&
		actual.status != ekstypes.ClusterStatusUpdating {
		return false, errControlPlaneNotActive
	}

	if actual.version != expected.version && actual.version != target {
		return false, errStaleControlPlaneVersion
	}

	return actual.status == ekstypes.ClusterStatusActive && actual.version == target, nil
}
