package clusterprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
)

// talosOptsFromCluster builds the OptionsTalos passed to the Talos provisioner,
// overlaying the distribution-agnostic cluster fields (node counts, Kubernetes
// version) that the provisioner consumes. The node-count fields bridge the
// deprecated Talos-scoped aliases during the migration window; KubernetesVersion
// carries the top-level spec.cluster.kubernetesVersion pin so the provisioner can
// tell whether the user pinned a version (vs. tracking the running one on update).
func talosOptsFromCluster(cluster *v1alpha1.Cluster) v1alpha1.OptionsTalos {
	talosOpts := cluster.Spec.Cluster.Talos
	//nolint:staticcheck // intentional: bridging deprecated field
	talosOpts.ControlPlanes = cluster.Spec.Cluster.ControlPlanes
	//nolint:staticcheck // intentional: bridging deprecated field
	talosOpts.Workers = cluster.Spec.Cluster.Workers
	talosOpts.KubernetesVersion = cluster.Spec.Cluster.KubernetesVersion

	return talosOpts
}

func (f DefaultFactory) createTalosProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	// Kubernetes provider: run Talos inside a DinD pod on the host cluster
	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderKubernetes {
		return f.createTalosKubernetesProvisioner(cluster)
	}

	if f.DistributionConfig.Talos == nil {
		return nil, nil, fmt.Errorf(
			"talos config is required for Talos distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	// Always skip CNI-dependent checks (CoreDNS, kube-proxy) for Talos Docker provisioner.
	//
	// Rationale:
	// 1. Custom CNI (Cilium, Calico): Pods cannot start until CNI is installed post-bootstrap.
	// 2. Default Flannel CNI: While Flannel is bundled with Talos, it can be slow or unreliable
	//    in containerized environments (GitHub Actions, Docker-in-Docker). The checks for
	//    kube-proxy and CoreDNS can timeout even when the cluster is fundamentally healthy.
	//
	// Since we've verified that etcd, kubelet, and the Kubernetes API are healthy via
	// PreBootSequenceChecks, the cluster is functional. Application-level DNS/proxy
	// services will become ready shortly after bootstrap completes.
	skipCNIChecks := true

	// Overlay cluster-level fields (node counts, Kubernetes version) onto Talos options.
	talosOpts := talosOptsFromCluster(cluster)

	// Propagate autoscaler-enabled flag to Hetzner options so the provisioner
	// can create the cluster-autoscaler-config Secret during bootstrap.
	hetznerOpts := cluster.Spec.Provider.Hetzner

	hetznerOpts.NodeAutoscalerEnabled = cluster.Spec.Cluster.Autoscaler.Node.Enabled ||
		cluster.Spec.Cluster.NodeAutoscaling == v1alpha1.NodeAutoscalingEnabled

	// Derive pool names from the new autoscaler pools config so that the
	// delete path can clean up autoscaler-managed Hetzner servers.
	if len(hetznerOpts.AutoscalerNodePoolNames) == 0 {
		pools := cluster.Spec.Cluster.Autoscaler.Node.Pools
		if len(pools) > 0 {
			names := make([]string, len(pools))
			for i, pool := range pools {
				names[i] = pool.Name
			}

			hetznerOpts.AutoscalerNodePoolNames = names
		}
	}

	provisioner, err := talosprovisioner.CreateProvisioner(
		f.DistributionConfig.Talos,
		cluster.Spec.Cluster.Connection.Kubeconfig,
		cluster.Spec.Cluster.Connection.Context,
		cluster.Spec.Cluster.Provider,
		talosOpts,
		hetznerOpts,
		cluster.Spec.Provider.Omni,
		skipCNIChecks,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Talos provisioner: %w", err)
	}

	if f.ComponentDetector != nil {
		provisioner.WithComponentDetector(f.ComponentDetector)
	}

	return provisioner, f.DistributionConfig.Talos, nil
}

// createTalosKubernetesProvisioner creates a Talos provisioner that runs inside
// a DinD pod on a host Kubernetes cluster via the Talos SDK.
func (f DefaultFactory) createTalosKubernetesProvisioner(
	cluster *v1alpha1.Cluster,
) (Provisioner, any, error) {
	if f.DistributionConfig.Talos == nil {
		return nil, nil, fmt.Errorf(
			"talos config is required for Talos distribution: %w",
			ErrMissingDistributionConfig,
		)
	}

	opts := cluster.Spec.Provider.Kubernetes

	// Derive cluster name from Talos config (set by applyClusterNameOverride).
	clusterName := f.DistributionConfig.Talos.GetClusterName()
	if clusterName == "" {
		clusterName = cluster.Name
	}

	_, restConfig, dynClient, k8sProvider, err := buildKubernetesInfra(opts)
	if err != nil {
		return nil, nil, err
	}

	// Create a full inner Talos Provisioner (Docker provider type).
	// The Docker client will be injected at Create() time after DinD is ready.
	talosOpts := talosOptsFromCluster(cluster)

	innerProvisioner, err := talosprovisioner.CreateProvisioner(
		f.DistributionConfig.Talos,
		cluster.Spec.Cluster.Connection.Kubeconfig,
		"",
		v1alpha1.ProviderDocker,
		talosOpts,
		v1alpha1.OptionsHetzner{},
		v1alpha1.OptionsOmni{},
		true, // skipCNIChecks — same as normal Docker path
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create inner Talos provisioner: %w", err)
	}

	// jscpd:ignore-start
	provisioner, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			InnerProvisioner: innerProvisioner,
			KubeconfigPath:   cluster.Spec.Cluster.Connection.Kubeconfig,
			K8sProvider:      k8sProvider,
			DynamicClient:    dynClient,
			RestConfig:       restConfig,
			ClusterName:      clusterName,
			Distribution:     string(cluster.Spec.Cluster.Distribution),
			GatewayClassName: opts.GatewayClassName,
			HostContext:      resolveKubernetesOption(opts.Context, opts.ContextEnvVar),
			ControlPlanes:    int(cluster.Spec.Cluster.ControlPlanes),
			Workers:          int(cluster.Spec.Cluster.Workers),
			Persistence:      opts.Persistence,
			MirrorSpecs:      f.DistributionConfig.MirrorSpecs,
		},
	)
	// jscpd:ignore-end
	if err != nil {
		return nil, nil, fmt.Errorf("create Talos Kubernetes provisioner: %w", err)
	}

	return provisioner, nil, nil
}
