package eks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

var (
	// ErrInvalidControlPlaneVersion reports a target outside EKS minor-version syntax.
	ErrInvalidControlPlaneVersion = errors.New(
		"EKS requires a Kubernetes minor version such as 1.35",
	)
	// ErrUnsupportedControlPlaneStep reports a skipped minor or a rollback request.
	ErrUnsupportedControlPlaneStep = errors.New(
		"EKS control-plane upgrades require one forward minor step",
	)
	// ErrInvalidClusterUpdate reports incomplete or inconsistent asynchronous update evidence.
	ErrInvalidClusterUpdate    = errors.New("invalid EKS cluster version update")
	errVersionAPIUnavailable   = errors.New("EKS version update API unavailable")
	controlPlaneVersionPattern = regexp.MustCompile(`^v?1\.(0|[1-9][0-9]*)(?:\.0)?$`)
)

// NormalizeControlPlaneVersion returns the semver equivalent of an EKS minor version.
// EKS selects a managed minor release, so nonzero patch numbers and suffixes are invalid.
func NormalizeControlPlaneVersion(raw string) (string, error) {
	match := controlPlaneVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidControlPlaneVersion, raw)
	}

	_, err := strconv.Atoi(match[1])
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidControlPlaneVersion, raw)
	}

	return "v1." + match[1] + ".0", nil
}

// PlanControlPlaneUpgrade validates an explicit forward minor upgrade or same-version no-op.
func PlanControlPlaneUpgrade(current, target string) (string, error) {
	currentVersion, err := NormalizeControlPlaneVersion(current)
	if err != nil {
		return "", err
	}

	targetVersion, err := NormalizeControlPlaneVersion(target)
	if err != nil {
		return "", err
	}

	fromMinor, _ := strconv.Atoi(strings.Split(currentVersion, ".")[1])

	toMinor, _ := strconv.Atoi(strings.Split(targetVersion, ".")[1])
	if toMinor != fromMinor && toMinor-fromMinor != 1 {
		return "", fmt.Errorf("%w: %s to %s", ErrUnsupportedControlPlaneStep, current, target)
	}

	return targetVersion, nil
}

type clusterVersionAPI interface {
	UpdateClusterVersion(
		ctx context.Context,
		params *awseks.UpdateClusterVersionInput,
		optFns ...func(*awseks.Options),
	) (*awseks.UpdateClusterVersionOutput, error)
	DescribeUpdate(
		ctx context.Context,
		params *awseks.DescribeUpdateInput,
		optFns ...func(*awseks.Options),
	) (*awseks.DescribeUpdateOutput, error)
}

// UpdateClusterVersion submits only a version update, retaining AWS readiness checks.
// The token binds SDK retries to this request. Ownership and step checks belong to the caller.
func (c *Client) UpdateClusterVersion(
	ctx context.Context, name, version, token string,
) (*ekstypes.Update, error) {
	api, ok := c.describer.(clusterVersionAPI)
	if !ok {
		return nil, errVersionAPIUnavailable
	}

	out, err := api.UpdateClusterVersion(ctx, &awseks.UpdateClusterVersionInput{
		Name: aws.String(name), Version: aws.String(version), ClientRequestToken: aws.String(token),
	})
	if err != nil {
		return nil, fmt.Errorf("request EKS control-plane version update: %w", err)
	}

	if out == nil || out.Update == nil {
		return nil, ErrInvalidClusterUpdate
	}

	return out.Update, nil
}

// DescribeClusterUpdate observes the exact asynchronous update submitted for the named cluster.
func (c *Client) DescribeClusterUpdate(
	ctx context.Context,
	name, updateID string,
) (*ekstypes.Update, error) {
	api, ok := c.describer.(clusterVersionAPI)
	if !ok {
		return nil, errVersionAPIUnavailable
	}

	out, err := api.DescribeUpdate(
		ctx,
		&awseks.DescribeUpdateInput{Name: aws.String(name), UpdateId: aws.String(updateID)},
	)
	if err != nil {
		return nil, fmt.Errorf("describe EKS control-plane update %s: %w", updateID, err)
	}

	if out == nil || out.Update == nil {
		return nil, ErrInvalidClusterUpdate
	}

	return out.Update, nil
}
