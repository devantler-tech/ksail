package eksprovisioner_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errUpgradeFixtureRequest = errors.New("wrong upgrade request")
	errUpgradeFixturePoll    = errors.New("wrong poll identity")
)

func TestControlPlaneUpgradeWaitsThroughUpdateAndClusterConvergence(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
		api.afterPoll = func() {
			switch api.polls {
			case 1:
				api.result.Status = ekstypes.UpdateStatusInProgress
			case 2:
				api.result.Status = ekstypes.UpdateStatusSuccessful
				api.cluster.Status = ekstypes.ClusterStatusUpdating
			default:
				api.cluster.Status = ekstypes.ClusterStatusActive
				api.cluster.Version = aws.String("1.35")
			}
		}
		provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
		require.NoError(t, provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35"))
		assert.Equal(t, 3, api.polls)
		assert.Equal(t, 1, api.submitted)
	})
}

func TestControlPlaneUpgradeRejectsFinalOwnershipChange(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	api.afterPoll = func() { api.cluster.Version = aws.String("1.35") }
	checks := 0
	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error {
		checks++
		if checks == 3 {
			return eksidentity.ErrIdentityMismatch
		}

		return nil
	})
	require.ErrorIs(
		t,
		provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35"),
		eksidentity.ErrIdentityMismatch,
	)
}

func TestControlPlaneUpgradeWaitHonorsDeadline(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	api.result.Status = ekstypes.UpdateStatusInProgress

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
	require.ErrorIs(
		t,
		provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35"),
		context.DeadlineExceeded,
	)
}

func TestControlPlaneUpgradeRejectsVersionMovementAtSubmit(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	checks := 0
	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error {
		checks++
		if checks == 2 {
			api.cluster.Version = aws.String("1.36")
		}

		return nil
	})
	require.Error(t, provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35"))
	assert.Zero(t, api.submitted)
}

func TestControlPlaneUpgradeReportsAWSFailureMessage(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	api.result.Status = ekstypes.UpdateStatusFailed
	api.result.Errors = []ekstypes.ErrorDetail{
		{
			ErrorCode:    "InsufficientFreeAddresses",
			ErrorMessage: aws.String("Free IP addresses before retrying"),
		},
	}
	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
	err := provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35")
	require.ErrorContains(t, err, "Free IP addresses before retrying")
}

func TestControlPlaneUpgradeCancelledBeforeSubmission(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
	require.ErrorIs(t, provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35"), context.Canceled)
	assert.Zero(t, api.submitted)
}

type upgradeAPI struct {
	cluster          *ekstypes.Cluster
	result           *ekstypes.Update
	submitted, polls int
	afterSubmit      func()
	afterPoll        func()
	submission       func() *ekstypes.Update
}

func upgradeCluster() *ekstypes.Cluster {
	return &ekstypes.Cluster{
		Name: aws.String(
			"demo",
		),
		Arn: aws.String("arn:aws:eks:us-east-1:123456789012:cluster/demo"),
		CreatedAt: aws.Time(
			time.Unix(1700000000, 0),
		),
		Status:  ekstypes.ClusterStatusActive,
		Version: aws.String("1.34"),
	}
}

func upgradeResult() *ekstypes.Update {
	return &ekstypes.Update{
		Id:     aws.String("id"),
		Type:   ekstypes.UpdateTypeVersionUpdate,
		Status: ekstypes.UpdateStatusSuccessful,
		Params: []ekstypes.UpdateParam{
			{Type: ekstypes.UpdateParamTypeVersion, Value: aws.String("1.35")},
		},
	}
}

func (api *upgradeAPI) DescribeCluster(context.Context, string) (*ekstypes.Cluster, error) {
	return api.cluster, nil
}
func (api *upgradeAPI) MintToken(context.Context, string) (string, error) { return "", nil }

func (api *upgradeAPI) UpdateClusterVersion(
	_ context.Context,
	name, version, token string,
) (*ekstypes.Update, error) {
	if name != "demo" || version != "1.35" || token == "" {
		return nil, errUpgradeFixtureRequest
	}

	api.submitted++
	if api.afterSubmit != nil {
		api.afterSubmit()
	}

	if api.submission != nil {
		return api.submission(), nil
	}

	return upgradeResult(), nil
}

func (api *upgradeAPI) DescribeClusterUpdate(
	_ context.Context,
	name, updateID string,
) (*ekstypes.Update, error) {
	if name != "demo" || updateID != "id" {
		return nil, errUpgradeFixturePoll
	}

	api.polls++
	if api.afterPoll != nil {
		api.afterPoll()
	}

	return api.result, nil
}

func newUpgradeProvisioner(
	t *testing.T,
	api *upgradeAPI,
	verifier eksidentity.Verifier,
	options ...eksprovisioner.Option,
) *eksprovisioner.UpgradableProvisioner {
	t.Helper()

	options = append([]eksprovisioner.Option{
		eksprovisioner.WithAWSClusterAPI(api),
		eksprovisioner.WithOwnershipVerifier(verifier),
		eksprovisioner.WithAWSConfig(
			aws.Config{
				Credentials: aws.CredentialsProviderFunc(
					func(context.Context) (aws.Credentials, error) {
						return aws.Credentials{
							AccessKeyID:     "permanent",
							SecretAccessKey: "secret",
						}, nil
					},
				),
			},
		),
	}, options...)
	provisioner, err := eksprovisioner.NewProvisioner(
		"demo",
		"us-east-1",
		"",
		eksctl.NewClient(),
		nil,
		options...,
	)
	require.NoError(t, err)

	return eksprovisioner.NewUpgradableProvisioner(
		eksprovisioner.NewUpdatableProvisioner(provisioner),
	)
}

func TestControlPlaneUpgradeWaitsForVerifiedTarget(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	api.afterPoll = func() { api.cluster.Version = aws.String("1.35") }
	verified := 0
	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error {
		verified++

		return nil
	})
	current, err := provisioner.GetCurrentVersions(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "v1.34.0", current.KubernetesVersion)
	require.NoError(t, provisioner.UpgradeKubernetes(t.Context(), "demo", "v1.34.0", "v1.35.0"))
	assert.Equal(t, 1, api.submitted)
	assert.Equal(t, 1, api.polls)
	assert.GreaterOrEqual(t, verified, 2)
	require.NoError(t, provisioner.UpgradeKubernetes(t.Context(), "demo", "v1.35.0", "1.35"))
	assert.Equal(t, 1, api.submitted)
}

func TestControlPlaneUpgradeRefusesUnsafeStart(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name                   string
		change                 func(*upgradeAPI)
		target, currentVersion string
		verifier               eksidentity.Verifier
	}{
		{name: "busy", change: func(api *upgradeAPI) { api.cluster.Status = ekstypes.ClusterStatusUpdating }},
		{name: "wrong_name", change: func(api *upgradeAPI) { api.cluster.Name = aws.String("other") }},
		{name: "wrong_region", change: func(api *upgradeAPI) {
			api.cluster.Arn = aws.String("arn:aws:eks:eu-west-1:123456789012:cluster/demo")
		}},
		{name: "missing_creation", change: func(api *upgradeAPI) { api.cluster.CreatedAt = nil }},
		{name: "stale_from", currentVersion: "1.33"},
		{name: "skipped_minor", target: "1.36"},
		{name: "downgrade", target: "1.33"},
		{name: "ownership_changed", verifier: func(context.Context) error { return eksidentity.ErrIdentityMismatch }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
			if testCase.change != nil {
				testCase.change(api)
			}

			verify := testCase.verifier
			if verify == nil {
				verify = func(context.Context) error { return nil }
			}

			provisioner := newUpgradeProvisioner(t, api, verify)

			currentVersion, target := testCase.currentVersion, testCase.target
			if currentVersion == "" {
				currentVersion = "1.34"
			}

			if target == "" {
				target = "1.35"
			}

			require.Error(
				t,
				provisioner.UpgradeKubernetes(t.Context(), "demo", currentVersion, target),
			)
			assert.Zero(t, api.submitted)
		})
	}

	api := &upgradeAPI{cluster: upgradeCluster()}
	provisioner := newUpgradeProvisioner(t, api, nil)
	require.Error(t, provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35"))
	assert.Zero(t, api.submitted)
}

func TestControlPlaneUpgradeRejectsInvalidCompletion(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		change func(*upgradeAPI)
	}{
		{"failed", func(api *upgradeAPI) { api.result.Status = ekstypes.UpdateStatusFailed }},
		{"cancelled", func(api *upgradeAPI) { api.result.Status = ekstypes.UpdateStatusCancelled }},
		{"missing", func(api *upgradeAPI) { api.result = nil }},
		{"wrong_id", func(api *upgradeAPI) { api.result.Id = aws.String("other") }},
		{"wrong_type", func(api *upgradeAPI) { api.result.Type = ekstypes.UpdateTypeConfigUpdate }},
		{"wrong_target", func(api *upgradeAPI) { api.result.Params[0].Value = aws.String("1.36") }},
		{"missing_target", func(api *upgradeAPI) { api.result.Params = nil }},
		{"duplicate_target", func(api *upgradeAPI) {
			api.result.Params = append(api.result.Params, api.result.Params[0])
		}},
		{"unknown_status", func(api *upgradeAPI) { api.result.Status = "Unknown" }},
		{"replacement", func(api *upgradeAPI) {
			api.afterPoll = func() { api.cluster.Version = aws.String("1.35"); api.cluster.CreatedAt = aws.Time(time.Now()) }
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
			testCase.change(api)
			provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
			require.Error(t, provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35"))
		})
	}
}

func TestControlPlaneUpgradeHonorsCancellation(t *testing.T) {
	t.Parallel()

	for _, atTarget := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "old_version", true: "in_progress"}[atTarget],
			func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
				if atTarget {
					api.cluster.Version = aws.String("1.34")
					api.result.Status = ekstypes.UpdateStatusInProgress
				}

				api.afterPoll = cancel
				provisioner := newUpgradeProvisioner(
					t,
					api,
					func(context.Context) error { return nil },
				)
				err := provisioner.UpgradeKubernetes(ctx, "demo", "1.34", "1.35")
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, api.submitted)
			},
		)
	}
}

func TestControlPlaneUpgradeRejectsMalformedSubmission(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		submission func() *ekstypes.Update
	}{
		{"nil_update", func() *ekstypes.Update { return nil }},
		{"missing_id", func() *ekstypes.Update {
			update := upgradeResult()
			update.Id = nil

			return update
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			api := &upgradeAPI{cluster: upgradeCluster(), submission: testCase.submission}
			provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil })
			require.Error(t, provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35"))
			assert.Equal(t, 1, api.submitted)
			assert.Zero(t, api.polls)
		})
	}
}

func TestControlPlaneUpgradeRequiresCredentialsThroughWait(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		canExpire bool
		lifetime  time.Duration
		token     string
		wantErr   bool
	}{
		{name: "permanent"},
		{name: "sufficient_session", canExpire: true, lifetime: 2 * time.Hour, token: "session"},
		{name: "short_session", canExpire: true, lifetime: 30 * time.Minute, token: "session", wantErr: true},
		{name: "expired_session", canExpire: true, lifetime: -time.Minute, token: "session", wantErr: true},
		{name: "unknown_session", token: "session", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
			api.afterPoll = func() { api.cluster.Version = aws.String("1.35") }
			values := aws.Credentials{
				AccessKeyID: "test", SecretAccessKey: "secret", SessionToken: testCase.token,
				CanExpire: testCase.canExpire, Expires: time.Now().Add(testCase.lifetime),
			}
			provisioner := newUpgradeProvisioner(
				t,
				api,
				func(context.Context) error { return nil },
				eksprovisioner.WithAWSConfig(
					aws.Config{
						Credentials: aws.CredentialsProviderFunc(
							func(context.Context) (aws.Credentials, error) { return values, nil },
						),
					},
				),
			)

			err := provisioner.UpgradeKubernetes(t.Context(), "demo", "1.34", "1.35")
			if testCase.wantErr {
				require.ErrorContains(t, err, "credential")
				assert.Zero(t, api.submitted)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 1, api.submitted)
			}
		})
	}
}
