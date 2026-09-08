package eksprovisioner

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

var (
	errUpgradeOwnershipRequired = errors.New(
		"EKS control-plane upgrade requires verified ownership",
	)
	errUpgradeAPIRequired       = errors.New("EKS control-plane upgrade API unavailable")
	errControlPlaneNotActive    = errors.New("EKS control plane must be ACTIVE")
	errStaleControlPlaneVersion = errors.New("EKS control-plane version changed; inspect and retry")
	errControlPlaneRecreation   = errors.New("EKS control-plane upgrades do not support recreation")
)

const (
	controlPlaneUpgradeTimeout      = 65 * time.Minute
	controlPlaneUpgradePollInterval = 10 * time.Second
)

// AWSClusterVersionAPI adds the asynchronous version-update surface to the connector client.
type AWSClusterVersionAPI interface {
	AWSClusterAPI
	UpdateClusterVersion(ctx context.Context, name, version, token string) (*ekstypes.Update, error)
	DescribeClusterUpdate(ctx context.Context, name, updateID string) (*ekstypes.Update, error)
}

// UpgradableProvisioner adds explicit control-plane upgrades while preserving node-group updates.
// The factory exposes this capability only when the experimental config option is enabled.
type UpgradableProvisioner struct {
	*UpdatableProvisioner
}

var _ clusterupdate.Upgrader = (*UpgradableProvisioner)(nil)

// NewUpgradableProvisioner opts an EKS provisioner into explicit control-plane upgrades.
func NewUpgradableProvisioner(p *UpdatableProvisioner) *UpgradableProvisioner {
	return &UpgradableProvisioner{UpdatableProvisioner: p}
}

// ValidateKubernetesUpgrade validates the plan before dry-run or same-version handling.
func (p *UpgradableProvisioner) ValidateKubernetesUpgrade(current, target string) (string, error) {
	version, err := eksclient.PlanControlPlaneUpgrade(current, target)
	if err != nil {
		return "", fmt.Errorf("plan EKS control-plane upgrade: %w", err)
	}

	return version, nil
}

// KubernetesUpgradeCategory describes the control-plane operation without implying recreation.
func (p *UpgradableProvisioner) KubernetesUpgradeCategory() clusterupdate.ChangeCategory {
	return clusterupdate.ChangeCategoryInPlace
}

// GetCurrentVersions reads the ACTIVE managed control-plane version.
func (p *UpgradableProvisioner) GetCurrentVersions(
	ctx context.Context, name string,
) (*clusterupdate.VersionInfo, error) {
	api, err := p.resolveAWSClient(ctx)
	if err != nil {
		return nil, err
	}

	snapshot, err := p.readControlPlane(ctx, api, name)
	if err != nil {
		return nil, err
	}

	if snapshot.status != ekstypes.ClusterStatusActive {
		return nil, errControlPlaneNotActive
	}

	return &clusterupdate.VersionInfo{KubernetesVersion: snapshot.version}, nil
}

// UpgradeKubernetes advances one managed minor version and waits for the exact AWS update.
// It does not change worker nodes, add-ons, cluster stacks or network configuration.
func (p *UpgradableProvisioner) UpgradeKubernetes(
	ctx context.Context,
	name, from, to string,
) error {
	target, err := p.ValidateKubernetesUpgrade(from, to)
	if err != nil {
		return err
	}

	if p.ownershipVerifier == nil {
		return errUpgradeOwnershipRequired
	}

	ctx, cancel := context.WithTimeout(ctx, controlPlaneUpgradeTimeout)
	defer cancel()

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("start EKS upgrade: %w", err)
	}

	api, expected, err := p.prepareControlPlaneUpgrade(ctx, name, from)
	if err != nil {
		return err
	}

	if expected.version == target {
		return nil
	}

	token := rand.Text()

	err = p.verifyUpgradeSubmission(ctx, api, expected)
	if err != nil {
		return err
	}

	err = p.validateUpgradeCredentialLifetime(ctx)
	if err != nil {
		return err
	}

	update, err := api.UpdateClusterVersion(
		ctx, p.name, strings.TrimSuffix(strings.TrimPrefix(target, "v"), ".0"), token,
	)
	if err != nil {
		return fmt.Errorf("submit EKS version update: %w", err)
	}

	updateID := aws.ToString(clusterUpdateID(update))

	err = validateVersionUpdate(update, updateID, target)
	if err != nil {
		return err
	}

	return p.waitForControlPlaneUpgrade(ctx, api, expected, target, updateID)
}

// UpgradeDistribution refuses a separate OS upgrade; AWS manages that dimension.
func (p *UpgradableProvisioner) UpgradeDistribution(context.Context, string, string, string) error {
	return fmt.Errorf("AWS manages the EKS distribution: %w", clustererr.ErrUpgradeSkipped)
}

// KubernetesImageRef is empty because EKS upgrades require an explicit target.
func (p *UpgradableProvisioner) KubernetesImageRef() string { return "" }

// DistributionImageRef is empty because AWS manages the distribution.
func (p *UpgradableProvisioner) DistributionImageRef() string { return "" }

// PinnedDistributionVersion is empty because there is no separate distribution target.
func (p *UpgradableProvisioner) PinnedDistributionVersion() string { return "" }

// PinnedKubernetesVersion is empty because only the user's explicit target is accepted.
func (p *UpgradableProvisioner) PinnedKubernetesVersion() string { return "" }

// VersionSuffix is empty because the internal version representation is plain semver.
func (p *UpgradableProvisioner) VersionSuffix() string { return "" }

// PrepareConfigForVersion refuses recreation; EKS upgrades never recreate a cluster.
func (p *UpgradableProvisioner) PrepareConfigForVersion(string, string) error {
	return errControlPlaneRecreation
}

func (p *UpgradableProvisioner) verifyUpgradeSubmission(
	ctx context.Context, api AWSClusterAPI, expected controlPlaneSnapshot,
) error {
	err := eksidentity.VerifyBeforeMutation(ctx, p.ownershipVerifier)
	if err != nil {
		return fmt.Errorf("verify ownership before EKS upgrade: %w", err)
	}

	actual, err := p.readControlPlane(ctx, api, p.name)
	if err != nil {
		return err
	}

	if actual.arn != expected.arn || !actual.createdAt.Equal(expected.createdAt) {
		return eksidentity.ErrIdentityMismatch
	}

	if actual.status != ekstypes.ClusterStatusActive || actual.version != expected.version {
		return errStaleControlPlaneVersion
	}

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("submit EKS upgrade: %w", err)
	}

	return nil
}

func (p *UpgradableProvisioner) prepareControlPlaneUpgrade(
	ctx context.Context, name, from string,
) (AWSClusterVersionAPI, controlPlaneSnapshot, error) {
	client, err := p.resolveAWSClient(ctx)
	if err != nil {
		return nil, controlPlaneSnapshot{}, err
	}

	api, ok := client.(AWSClusterVersionAPI)
	if !ok {
		return nil, controlPlaneSnapshot{}, errUpgradeAPIRequired
	}

	snapshot, err := p.readControlPlane(ctx, api, name)
	if err != nil {
		return nil, snapshot, err
	}

	if snapshot.status != ekstypes.ClusterStatusActive {
		return nil, snapshot, errControlPlaneNotActive
	}

	expected, err := eksclient.NormalizeControlPlaneVersion(from)
	if err != nil {
		return nil, snapshot, fmt.Errorf("validate starting EKS version: %w", err)
	}

	if snapshot.version != expected {
		return nil, snapshot, errStaleControlPlaneVersion
	}

	err = eksidentity.VerifyBeforeMutation(ctx, p.ownershipVerifier)
	if err != nil {
		return nil, snapshot, fmt.Errorf("verify EKS upgrade ownership: %w", err)
	}

	return api, snapshot, nil
}

type controlPlaneSnapshot struct {
	arn       string
	createdAt time.Time
	version   string
	status    ekstypes.ClusterStatus
}

func (p *UpgradableProvisioner) readControlPlane(
	ctx context.Context, api AWSClusterAPI, name string,
) (controlPlaneSnapshot, error) {
	if p.name == "" || p.resolveName(name) != p.name {
		return controlPlaneSnapshot{}, eksidentity.ErrIdentityMismatch
	}

	cluster, err := api.DescribeCluster(ctx, p.name)
	if err != nil {
		return controlPlaneSnapshot{}, fmt.Errorf("read EKS control plane: %w", err)
	}

	err = p.validateControlPlaneIdentity(cluster)
	if err != nil {
		return controlPlaneSnapshot{}, err
	}

	version, err := eksclient.NormalizeControlPlaneVersion(aws.ToString(cluster.Version))
	if err != nil {
		return controlPlaneSnapshot{}, fmt.Errorf("read EKS control-plane version: %w", err)
	}

	return controlPlaneSnapshot{
		arn: aws.ToString(
			cluster.Arn,
		),
		createdAt: *cluster.CreatedAt,
		version:   version,
		status:    cluster.Status,
	}, nil
}

func (p *UpgradableProvisioner) validateControlPlaneIdentity(cluster *ekstypes.Cluster) error {
	if cluster == nil || aws.ToString(cluster.Name) != p.name || cluster.CreatedAt == nil ||
		cluster.CreatedAt.IsZero() {
		return eksidentity.ErrInvalidLiveIdentity
	}

	arn, err := awsarn.Parse(aws.ToString(cluster.Arn))
	if err != nil || arn.Service != "eks" || arn.Region != p.region ||
		arn.Resource != "cluster/"+p.name ||
		arn.AccountID == "" {
		return eksidentity.ErrInvalidLiveIdentity
	}

	return nil
}

var errUpgradeCredentialLifetime = errors.New(
	"EKS upgrade credentials must remain valid through the bounded wait plus one minute; " +
		"use a credential provider with a sufficient known session expiry",
)

// validateUpgradeCredentialLifetime rejects credentials that could expire after AWS accepts
// the upgrade. The selected credential generation stays fixed for ownership and mutation.
func (p *UpgradableProvisioner) validateUpgradeCredentialLifetime(ctx context.Context) error {
	if p.upgradeCredentials == nil {
		return errUpgradeCredentialLifetime
	}

	values, err := p.upgradeCredentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("read frozen EKS upgrade credentials: %w", err)
	}

	if values.AccessKeyID == "" || values.SecretAccessKey == "" {
		return errUpgradeCredentialLifetime
	}

	if !values.CanExpire {
		if values.SessionToken != "" {
			return errUpgradeCredentialLifetime
		}

		return nil
	}

	deadline, ok := ctx.Deadline()
	if !ok || !values.Expires.After(deadline.Add(time.Minute)) {
		return errUpgradeCredentialLifetime
	}

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("validate EKS upgrade credential lifetime: %w", err)
	}

	return nil
}
