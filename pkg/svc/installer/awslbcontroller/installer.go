package awslbcontrollerinstaller

import (
	"errors"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
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

// Installer installs or upgrades the AWS Load Balancer Controller.
//
// It embeds helmutil.Base for the whole Helm lifecycle; no extra Kubernetes
// resources are created outside the chart.
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new AWS Load Balancer Controller installer instance.
//
// clusterName is required (see ErrClusterNameRequired). region is optional:
// when set it is passed to the chart so the controller does not depend on
// IMDS/environment discovery; when empty the chart's own discovery applies.
// When haEnabled is true the chart runs with two replicas for fast failover
// via leader election.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
	clusterName, region string,
	haEnabled bool,
) (*Installer, error) {
	if strings.TrimSpace(clusterName) == "" {
		return nil, ErrClusterNameRequired
	}

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
				ValuesYaml:  buildValuesYaml(clusterName, region, haEnabled),
			},
		),
	}, nil
}

// buildValuesYaml generates the Helm values YAML for the chart. clusterName is
// the chart's one required value; region is included only when known; a second
// replica is configured only for HA clusters (single-node clusters cannot
// schedule two replicas past the chart's default anti-affinity).
//
// The chart's Service mutator webhook (default-on, failurePolicy: Fail) is
// disabled: it makes this controller the default for every new LoadBalancer
// Service, and during install its admitted-but-not-ready window rejects
// Services created by concurrently-installing components.
func buildValuesYaml(clusterName, region string, haEnabled bool) string {
	parts := []string{
		"clusterName: " + clusterName,
		"enableServiceMutatorWebhook: false",
	}

	if region != "" {
		parts = append(parts, "region: "+region)
	}

	if haEnabled {
		parts = append(parts, "replicaCount: 2")
	} else {
		parts = append(parts, "replicaCount: 1")
	}

	return strings.Join(parts, "\n")
}
