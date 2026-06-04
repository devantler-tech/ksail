package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
)

func (f DefaultFactory) createKWOKProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.KWOK == nil {
		return nil, nil, fmt.Errorf(
			"kwok config is required for KWOK distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	kwokConfig := f.DistributionConfig.KWOK

	// Kubernetes provider: run KWOK inside a DinD pod on the host cluster
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createKWOKKubernetesProvisioner(cluster, kwokConfig)
	}

	provisioner := kwokprovisioner.NewProvisioner(
		kwokConfig.Name,
		kwokConfig.ConfigPath,
		nil,
	)

	return provisioner, kwokConfig, nil
}

// createKWOKKubernetesProvisioner creates a KWOK provisioner that runs inside
// a DinD pod on a host Kubernetes cluster.
func (f DefaultFactory) createKWOKKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
	kwokConfig *KWOKConfig,
) (Provisioner, any, error) {
	opts := cluster.Spec.Provider.Kubernetes

	_, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	// Use kwokConfig.Name as the cluster name — it's always set correctly
	// by applyClusterNameOverride, while cluster.Name may be empty
	// when using --name flag without a ksail.yaml file.
	clusterName := kwokConfig.Name

	provisioner, err := kwokprovisioner.NewKubernetesProvisioner(
		kwokprovisioner.KubernetesProvisionerConfig{
			Name:             kwokConfig.Name,
			ConfigPath:       kwokConfig.ConfigPath,
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			RestConfig:       restConfig,
			ClusterName:      clusterName,
			Distribution:     string(cluster.Spec.Cluster.Distribution),
			GatewayClassName: opts.GatewayClassName,
			Persistence:      opts.Persistence,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create KWOK Kubernetes provisioner: %w", err)
	}

	return provisioner, kwokConfig, nil
}
