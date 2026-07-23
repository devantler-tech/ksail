package awslbcontrollerinstaller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	awslbcRepoName = "eks"
	awslbcRepoURL  = "https://aws.github.io/eks-charts"
	awslbcRelease  = "aws-load-balancer-controller"
	// The controller is conventionally installed into kube-system (the chart's
	// documented default target), which always exists — no namespace creation.
	awslbcNamespace = "kube-system"
	awslbcChartName = "eks/aws-load-balancer-controller"
)

// ErrClusterNameRequired is returned when no EKS cluster name is available for
// the chart's required clusterName value. The controller uses it to discover
// the cluster's VPC and to tag/filter the AWS resources it manages, so there
// is no safe default.
var ErrClusterNameRequired = errors.New(
	"aws-load-balancer-controller requires the EKS cluster name (chart value clusterName)",
)

// ErrInvalidServiceAccountName is returned when a pre-created service account
// name is not a valid Kubernetes object name. Validating here keeps arbitrary
// config strings out of the interpolated Helm values YAML.
var ErrInvalidServiceAccountName = errors.New(
	"aws-load-balancer-controller service account name must be a valid DNS-1123 subdomain",
)

// ErrReleaseIdentityEmpty reports Helm storage without a Kubernetes UID. A
// caller cannot safely bind uninstall ownership to such a release.
var ErrReleaseIdentityEmpty = errors.New(
	"aws-load-balancer-controller Helm storage UID is empty",
)

// Installer installs or upgrades the AWS Load Balancer Controller.
//
// It embeds helmutil.Base for the whole Helm lifecycle; no extra Kubernetes
// resources are created outside the chart.
type Installer struct {
	*helmutil.Base

	client       helm.Interface
	ksailManaged bool
}

// NewInstaller creates a new AWS Load Balancer Controller installer instance.
//
// clusterName is required (see ErrClusterNameRequired). region is optional:
// when set it is passed to the chart so the controller does not depend on
// IMDS/environment discovery; when empty the chart's own discovery applies.
// serviceAccountName is optional: when set, the chart reuses that pre-created
// service account (AWS's documented IRSA path: serviceAccount.create=false)
// instead of creating its own; when empty the chart's default SA creation
// applies and IAM comes from node-role credentials. When haEnabled is true the
// chart runs with two replicas for fast failover via leader election.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
	clusterName, region, serviceAccountName string,
	haEnabled bool,
	ksailManaged ...bool,
) (*Installer, error) {
	err := helm.ValidateKubernetesReleaseStorageDriver(os.Getenv("HELM_DRIVER"))
	if err != nil {
		return nil, fmt.Errorf("validate release identity storage: %w", err)
	}

	if strings.TrimSpace(clusterName) == "" {
		return nil, ErrClusterNameRequired
	}

	serviceAccountName = strings.TrimSpace(serviceAccountName)
	if serviceAccountName != "" {
		if errs := validation.IsDNS1123Subdomain(serviceAccountName); len(errs) > 0 {
			return nil, fmt.Errorf("%w: %q: %s",
				ErrInvalidServiceAccountName, serviceAccountName, strings.Join(errs, "; "))
		}
	}

	managed := len(ksailManaged) > 0 && ksailManaged[0]

	return &Installer{
		Base: helmutil.NewBase(
			"aws-load-balancer-controller",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: awslbcRepoName,
				URL:  awslbcRepoURL,
			},
			&helm.ChartSpec{
				ReleaseName:     awslbcRelease,
				ChartName:       awslbcChartName,
				Namespace:       awslbcNamespace,
				Version:         chartVersion(),
				RepoURL:         awslbcRepoURL,
				CreateNamespace: false,
				Atomic:          true,
				Wait:            true,
				WaitForJobs:     true,
				// The chart owns the TargetGroupBinding CRDs; without this,
				// upgrades leave them at the previously-installed version
				// (helm v4 maps !UpgradeCRDs to SkipCRDs).
				UpgradeCRDs: true,
				Timeout:     timeout,
				ValuesYaml:  buildValuesYaml(clusterName, region, serviceAccountName, haEnabled),
			},
		),
		client:       client,
		ksailManaged: managed,
	}, nil
}

// IsGitOpsManaged reports whether the current release is owned by Flux or
// ArgoCD. Missing release storage means there is no GitOps-owned release.
func (i *Installer) IsGitOpsManaged(ctx context.Context) (bool, error) {
	labels, err := i.client.GetReleaseStorageLabels(ctx, awslbcRelease, awslbcNamespace)
	if err != nil && !errors.Is(err, helm.ErrNoReleaseStorage) {
		return false, fmt.Errorf(
			"check AWS load balancer controller release ownership: %w",
			err,
		)
	}

	_, managed := helmutil.IsGitOpsManaged(labels)

	return managed, nil
}

// ReleaseIdentity returns the Kubernetes UID of the latest Helm storage
// object. Unlike a release name or revision, this changes when a deleted
// same-name release is installed again.
func (i *Installer) ReleaseIdentity(ctx context.Context) (string, error) {
	metadata, err := i.client.GetReleaseStorageMetadata(
		ctx,
		awslbcRelease,
		awslbcNamespace,
	)
	if err != nil {
		return "", fmt.Errorf(
			"read AWS load balancer controller release identity: %w",
			err,
		)
	}

	if metadata == nil {
		return "", ErrReleaseIdentityEmpty
	}

	identity := strings.TrimSpace(metadata.Identity)
	if identity == "" {
		return "", ErrReleaseIdentityEmpty
	}

	return identity, nil
}

// OwnsReleaseIdentity reports whether the expected Kubernetes UID still
// belongs to the current Helm release history. Helm upgrades create a new
// storage object for each revision, so requiring only the latest UID would
// reject cleanup after an owned failed/pending upgrade. A deleted and
// reinstalled same-name release has a new history and therefore does not match.
func (i *Installer) OwnsReleaseIdentity(ctx context.Context, expected string) (bool, error) {
	metadata, err := i.client.GetReleaseStorageMetadata(
		ctx,
		awslbcRelease,
		awslbcNamespace,
	)
	if err != nil {
		return false, fmt.Errorf(
			"read AWS load balancer controller release identity history: %w",
			err,
		)
	}

	expected = strings.TrimSpace(expected)
	if expected == "" || metadata == nil {
		return false, nil
	}

	return metadata.Identity == expected ||
		slices.Contains(metadata.HistoryIdentities, expected), nil
}

// Uninstall removes the AWS Load Balancer Controller only when the caller has
// supplied positive KSail ownership evidence. A Flux- or ArgoCD-managed release
// is still left untouched, and an ownership lookup failure aborts before Helm
// can delete anything.
func (i *Installer) Uninstall(ctx context.Context) error {
	if !i.ksailManaged {
		return nil
	}

	skip, err := helmutil.SkipIfGitOpsManaged(
		ctx,
		i.client,
		"aws-load-balancer-controller",
		awslbcRelease,
		awslbcNamespace,
	)
	if err != nil {
		return fmt.Errorf("check AWS load balancer controller release ownership: %w", err)
	}

	if skip {
		return nil
	}

	err = i.Base.Uninstall(ctx)
	if err != nil {
		return fmt.Errorf("uninstall AWS load balancer controller: %w", err)
	}

	return nil
}

// buildValuesYaml generates the Helm values YAML for the chart. clusterName is
// the chart's one required value; region is included only when known; a
// non-empty serviceAccountName switches the chart to AWS's IRSA path
// (serviceAccount.create=false + the given name — callers validate the name
// first); a second replica is configured only for HA clusters (single-node
// clusters cannot schedule two replicas past the chart's default
// anti-affinity).
//
// The chart's Service mutator webhook (default-on, failurePolicy: Fail) is
// disabled: it makes this controller the default for every new LoadBalancer
// Service, and during install its admitted-but-not-ready window rejects
// Services created by concurrently-installing components.
func buildValuesYaml(clusterName, region, serviceAccountName string, haEnabled bool) string {
	parts := []string{
		"clusterName: " + clusterName,
		"enableServiceMutatorWebhook: false",
	}

	if region != "" {
		parts = append(parts, "region: "+region)
	}

	if serviceAccountName = strings.TrimSpace(serviceAccountName); serviceAccountName != "" {
		// Quoted: DNS-1123 names like "123", "null", "true" or "on" are
		// otherwise parsed as YAML numbers/nulls/booleans, not strings.
		// Validation guarantees the name contains no quote or backslash.
		parts = append(parts,
			"serviceAccount:\n  create: false\n  name: \""+serviceAccountName+"\"")
	}

	if haEnabled {
		parts = append(parts, "replicaCount: 2")
	} else {
		parts = append(parts, "replicaCount: 1")
	}

	return strings.Join(parts, "\n")
}
