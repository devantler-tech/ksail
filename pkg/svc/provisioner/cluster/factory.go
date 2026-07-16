package clusterprovisioner

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/container/apiv1/containerpb"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Re-export errors for backward compatibility.
var (
	// ErrUnsupportedDistribution is returned when an unsupported distribution is specified.
	ErrUnsupportedDistribution = clustererr.ErrUnsupportedDistribution
	// ErrUnsupportedProvider is returned when an unsupported provider is specified.
	ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider
	// ErrMissingDistributionConfig is returned when no pre-loaded distribution config is provided.
	ErrMissingDistributionConfig = clustererr.ErrMissingDistributionConfig
	// ErrImageVerificationTemplateNotRegularFile is returned when the image verification
	// template path exists but is not a regular file (e.g. it is a directory).
	ErrImageVerificationTemplateNotRegularFile = errors.New(
		"image verification template is not a regular file",
	)
)

// DistributionConfig holds pre-loaded distribution-specific configuration.
// This config is used directly by the factory, preserving any in-memory modifications
// (e.g., mirror registries, metrics-server flags).
type DistributionConfig struct {
	// Kind holds the pre-loaded Kind cluster configuration.
	Kind *v1alpha4.Cluster
	// K3d holds the pre-loaded K3d cluster configuration.
	K3d *k3dv1alpha5.SimpleConfig
	// Talos holds the pre-loaded Talos machine configurations.
	Talos *talosconfigmanager.Configs
	// VCluster holds the pre-loaded vCluster configuration.
	VCluster *VClusterConfig
	// KWOK holds the pre-loaded KWOK configuration.
	KWOK *KWOKConfig
	// EKS holds the pre-loaded EKS configuration.
	EKS *EKSConfig
	// GKE holds the pre-loaded GKE configuration.
	GKE *GKEConfig
	// AKS holds the pre-loaded AKS configuration.
	AKS *AKSConfig
	// MirrorSpecs holds resolved registry mirror specifications. For the Kubernetes
	// provider these are applied inside the DinD environment so nested clusters pull
	// through authenticated, caching mirrors (the host-level CLI mirror stage cannot
	// reach the DinD daemon). Empty disables nested mirroring.
	MirrorSpecs []registry.MirrorSpec
}

// EKSConfig holds EKS-specific configuration.
type EKSConfig struct {
	// Name is the cluster name (mirrors eksctl.yaml metadata.name).
	Name string
	// Region is the AWS region.
	Region string
	// ConfigPath is the path to the declarative eksctl.yaml.
	ConfigPath string
	// KubeconfigPath is the path where eksctl writes the created cluster context.
	KubeconfigPath string
}

// GetClusterName returns the EKS cluster name.
// This implements the ClusterNameProvider interface used by
// configmanager.GetClusterName.
func (c *EKSConfig) GetClusterName() string {
	return c.Name
}

// GKEConfig holds GKE-specific configuration.
type GKEConfig struct {
	// Name is the cluster name (mirrors gke.yaml name when present).
	Name string
	// Project is the Google Cloud project ID every GKE call is scoped to.
	Project string
	// Location is the GKE location (zone or region). Empty means "not pinned":
	// reads resolve the cluster's own location, while create requires a value.
	Location string
	// ConfigPath is the path to the declarative gke.yaml cluster spec, when present.
	ConfigPath string
	// ClusterSpec is the pre-loaded declarative cluster specification parsed
	// from ConfigPath. Required for create; nil is fine for inspection-only use.
	ClusterSpec *containerpb.Cluster
}

// GetClusterName returns the GKE cluster name.
// This implements the ClusterNameProvider interface used by
// configmanager.GetClusterName.
func (c *GKEConfig) GetClusterName() string {
	return c.Name
}

// AKSConfig holds AKS-specific configuration.
type AKSConfig struct {
	// Name is the cluster name (mirrors aks.yaml name when present).
	Name string
	// SubscriptionID is the Azure subscription every AKS call is scoped to.
	SubscriptionID string
	// ResourceGroup is the Azure resource group hosting the cluster. Empty
	// means "not pinned": reads resolve the cluster's own resource group via
	// a subscription-wide list, while create requires a value.
	ResourceGroup string
	// ConfigPath is the path to the declarative aks.yaml cluster spec, when present.
	ConfigPath string
	// ClusterSpec is the pre-loaded declarative cluster specification parsed
	// from ConfigPath. Required for create; nil is fine for inspection-only use.
	ClusterSpec *armcontainerservice.ManagedCluster
}

// GetClusterName returns the AKS cluster name.
// This implements the ClusterNameProvider interface used by
// configmanager.GetClusterName.
func (c *AKSConfig) GetClusterName() string {
	return c.Name
}

// KWOKConfig holds KWOK-specific configuration.
type KWOKConfig struct {
	// Name is the cluster name.
	Name string
	// ConfigPath is the optional path to a kwok.yaml configuration file.
	ConfigPath string
}

// GetClusterName returns the KWOK cluster name.
// This implements the ClusterNameProvider interface used by configmanager.GetClusterName.
func (c *KWOKConfig) GetClusterName() string {
	return c.Name
}

// VClusterConfig holds vCluster-specific configuration.
type VClusterConfig struct {
	// Name is the cluster name.
	Name string
	// ValuesPath is the optional path to a vcluster.yaml values file.
	ValuesPath string
	// DisableFlannel disables the built-in flannel CNI in the vCluster.
	// Set to true when a custom CNI (Cilium, Calico) is being installed.
	DisableFlannel bool
}

// GetClusterName returns the vCluster cluster name.
// This implements the ClusterNameProvider interface used by configmanager.GetClusterName.
func (c *VClusterConfig) GetClusterName() string {
	return c.Name
}

// Factory creates distribution-specific cluster provisioners based on the KSail cluster configuration.
type Factory interface {
	Create(ctx context.Context, cluster *v1alpha1.Cluster) (Provisioner, any, error)
}

// DefaultFactory implements Factory for creating cluster provisioners.
// It requires pre-loaded distribution configs via DistributionConfig to preserve
// any in-memory modifications made before cluster creation.
type DefaultFactory struct {
	// DistributionConfig holds pre-loaded distribution-specific configuration.
	// This is required and must contain the appropriate config for the cluster's distribution.
	DistributionConfig *DistributionConfig

	// ComponentDetector is an optional detector used to probe running clusters
	// for installed components. When non-nil it is injected into provisioners
	// so that GetCurrentConfig returns accurate live state instead of defaults.
	ComponentDetector *detector.ComponentDetector
}

// Create selects the correct distribution provisioner for the KSail cluster configuration.
// It requires DistributionConfig to be set with the appropriate pre-loaded config.
func (f DefaultFactory) Create(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	err := f.validateCreateInputs(cluster)
	if err != nil {
		return nil, nil, err
	}

	switch cluster.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return f.createKindProvisioner(cluster)
	case v1alpha1.DistributionK3s:
		return f.createK3dProvisioner(cluster)
	case v1alpha1.DistributionTalos:
		return f.createTalosProvisioner(cluster)
	case v1alpha1.DistributionVCluster:
		return f.createVClusterProvisioner(cluster)
	case v1alpha1.DistributionKWOK:
		return f.createKWOKProvisioner(cluster)
	case v1alpha1.DistributionEKS, v1alpha1.DistributionGKE, v1alpha1.DistributionAKS:
		return f.createManagedProvisioner(ctx, cluster)
	default:
		return nil, "", fmt.Errorf(
			"%w: %s",
			ErrUnsupportedDistribution,
			cluster.Spec.Cluster.Distribution,
		)
	}
}

// createManagedProvisioner routes the managed cloud distributions (EKS, GKE,
// AKS) to their provisioner constructors — split from Create to keep its
// distribution switch within the complexity budget.
func (f DefaultFactory) createManagedProvisioner(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	//nolint:exhaustive // Create routes only the managed cloud distributions here.
	switch cluster.Spec.Cluster.Distribution {
	case v1alpha1.DistributionEKS:
		return f.createEKSProvisioner(cluster)
	case v1alpha1.DistributionGKE:
		return f.createGKEProvisioner(ctx, cluster)
	case v1alpha1.DistributionAKS:
		return f.createAKSProvisioner(cluster)
	default:
		return nil, "", fmt.Errorf(
			"%w: %s",
			ErrUnsupportedDistribution,
			cluster.Spec.Cluster.Distribution,
		)
	}
}

// validateCreateInputs guards Create against a nil cluster and a missing
// pre-loaded distribution config.
func (f DefaultFactory) validateCreateInputs(cluster *v1alpha1.Cluster) error {
	if cluster == nil {
		return fmt.Errorf(
			"cluster configuration is required: %w",
			ErrUnsupportedDistribution,
		)
	}

	if f.DistributionConfig == nil {
		return fmt.Errorf(
			"distribution config is required: %w",
			ErrMissingDistributionConfig,
		)
	}

	return nil
}
