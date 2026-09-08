package eksprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
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

// isRetryablePollFailure classifies a poll failure with the SDK's own retry
// rules, so the wait loop retries exactly the transport and throttling classes
// the standard retryer would have retried had its attempt budget not run out.
func isRetryablePollFailure(err error) bool {
	return retry.IsErrorRetryables(retry.DefaultRetryables).
		IsErrorRetryable(err) == aws.TrueTernary
}

// transientPollError marks a DescribeClusterUpdate failure that says nothing
// about the upgrade itself, only about reaching the API.
type transientPollError struct{ err error }

func (e *transientPollError) Error() string { return e.err.Error() }

func (e *transientPollError) Unwrap() error { return e.err }

func isTransientPollError(err error) bool {
	var transient *transientPollError

	return errors.As(err, &transient)
}

// waitDeadlineError keeps the context error in the chain, because callers match
// on context.DeadlineExceeded, while still naming the poll failure that was
// recurring when time ran out.
func waitDeadlineError(updateID string, ctxErr, lastPollErr error) error {
	if lastPollErr != nil {
		return fmt.Errorf(
			"wait for EKS update %s: %w (last poll error: %w)",
			updateID, ctxErr, lastPollErr,
		)
	}

	return fmt.Errorf("wait for EKS update %s: %w", updateID, ctxErr)
}

func (p *UpgradableProvisioner) waitForControlPlaneUpgrade(
	ctx context.Context,
	api AWSClusterVersionAPI,
	expected controlPlaneSnapshot,
	target, updateID string,
) error {
	ticker := time.NewTicker(controlPlaneUpgradePollInterval)
	defer ticker.Stop()

	var lastPollErr error

	for {
		err := ctx.Err()
		if err != nil {
			return waitDeadlineError(updateID, err, lastPollErr)
		}

		done, err := p.controlPlaneUpgradeComplete(ctx, api, expected, target, updateID)

		switch {
		case err == nil:
			if done {
				return nil
			}

			lastPollErr = nil
		case isTransientPollError(err):
			// The SDK retryer has already spent its attempts on this poll. The
			// upgrade itself is unaffected, so keep polling until the deadline
			// rather than abandoning a control plane that is still converging.
			//
			// Keep the UNDERLYING error, not the marker. waitDeadlineError wraps
			// this with %w, so retaining the marker would make the returned
			// deadline error satisfy isTransientPollError — a wait that ran out of
			// time reporting itself as a retryable poll blip.
			lastPollErr = errors.Unwrap(err)
		default:
			return err
		}

		select {
		case <-ctx.Done():
			return waitDeadlineError(updateID, ctx.Err(), lastPollErr)
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
		wrapped := fmt.Errorf("poll EKS version update: %w", err)
		if isRetryablePollFailure(err) {
			return false, &transientPollError{err: wrapped}
		}

		return false, wrapped
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
