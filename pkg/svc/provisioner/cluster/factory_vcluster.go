package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
)

func (f DefaultFactory) createVClusterProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createVClusterKubernetesProvisioner(cluster)
	}

	if f.DistributionConfig.VCluster == nil {
		return nil, nil, fmt.Errorf(
			"vcluster config is required for VCluster distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	vclusterConfig := f.DistributionConfig.VCluster

	provisioner, err := vclusterprovisioner.CreateProvisioner(
		vclusterConfig.Name,
		vclusterConfig.ValuesPath,
		vclusterConfig.DisableFlannel,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create VCluster provisioner: %w", err)
	}

	return provisioner, vclusterConfig, nil
}

// createVClusterKubernetesProvisioner creates a vCluster provisioner that
// deploys vCluster as a Helm release on a host Kubernetes cluster.
func (f DefaultFactory) createVClusterKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	// Use VCluster config name (set by applyClusterNameOverride).
	vclusterConfig := f.DistributionConfig.VCluster

	clusterName := ""
	if vclusterConfig != nil {
		clusterName = vclusterConfig.Name
	}

	if clusterName == "" {
		clusterName = cluster.Name
	}

	hostClient, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	var (
		valuesPath     string
		disableFlannel bool
	)

	if vclusterConfig != nil {
		valuesPath = vclusterConfig.ValuesPath
		disableFlannel = vclusterConfig.DisableFlannel
	}

	provisioner, err := vclusterprovisioner.NewKubernetesProvisioner(
		vclusterprovisioner.KubernetesProvisionerConfig{
			ClusterName:      clusterName,
			HostContext:      resolveKubernetesOption(opts.Context, opts.ContextEnvVar),
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			HostClientset:    hostClient,
			RestConfig:       restConfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			GatewayClassName: opts.GatewayClassName,
			ValuesPath:       valuesPath,
			DisableFlannel:   disableFlannel,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create vCluster Kubernetes provisioner: %w", err)
	}

	return provisioner, vclusterConfig, nil
}
