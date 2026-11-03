package kindprovisioner

import (
	"context"
	"net/netip"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/docker/docker/api/types/network"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/log"
)

// NewKubernetesProvisionerForConnectorTest builds a minimal KubernetesProvisioner exercising only
// the operator Connector paths (publish + read): the host clientset, the nested cluster name
// (resolved through the embedded kindConfig), and the on-disk kubeconfig path. It skips the DinD
// infra the full NewKubernetesProvisioner wires, which a Connector unit test does not need.
func NewKubernetesProvisionerForConnectorTest(
	clientset kubernetes.Interface, clusterName, kubeconfigPath string,
) *KubernetesProvisioner {
	return &KubernetesProvisioner{
		Provisioner: &Provisioner{
			kindConfig: &v1alpha4.Cluster{Name: clusterName}, waitForReady: k8s.WaitForClusterReady,
		},
		hostClientset:  clientset,
		kubeconfigPath: kubeconfigPath,
	}
}

// PlanDinDNetworkForTest exposes the allocation result, without exposing internal state.
func PlanDinDNetworkForTest(
	ctx context.Context,
	cfg *v1alpha4.Cluster,
	endpoints ...string,
) (string, error) {
	plan, err := planDinDNetwork(ctx, cfg, nil, endpoints...)

	return plan.pool, err
}

// SelectDinDNetworkForTest exercises bounded pool exhaustion with supplied known prefixes.
func SelectDinDNetworkForTest(protected []netip.Prefix) (string, error) {
	plan, err := selectDinDNetwork(protected)

	return plan.pool, err
}

// EndpointPrefixesForTest permits deterministic DNS results without external lookups.
var EndpointPrefixesForTest = endpointPrefixes //nolint:gochecknoglobals

// ValidateDinDNetworksForTest exposes real network validation at the Docker API boundary.
func ValidateDinDNetworksForTest(
	ctx context.Context,
	client interface {
		NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	},
	requireKind bool,
) error {
	return validateDinDNetworks(ctx, client, dindNetworkPlan{
		base:      netip.MustParsePrefix("10.16.0.0/12"),
		protected: []netip.Prefix{netip.MustParsePrefix("10.0.1.2/32")},
	}, requireKind)
}

// FinalizeKubeconfigForTest exercises endpoint rewrite, readiness and publication together.
func (k *KubernetesProvisioner) FinalizeKubeconfigForTest(
	ctx context.Context, target, serverURL string,
) error {
	return k.finalizeKubeconfig(ctx, target, serverURL)
}

// PublishConnectorKubeconfigForTest exposes publishConnectorKubeconfig for unit testing.
func (k *KubernetesProvisioner) PublishConnectorKubeconfigForTest(
	ctx context.Context, target string,
) error {
	return k.publishConnectorKubeconfig(ctx, target)
}

// KubeConfigForTest returns the kubeConfig field for testing purposes.
func (k *Provisioner) KubeConfigForTest() string {
	return k.kubeConfig
}

// WithWaitForReadyForTest injects a stub readiness waiter so Start can be
// exercised without a live cluster.
func (k *Provisioner) WithWaitForReadyForTest(
	f func(ctx context.Context, kubeconfigPath, contextName string) error,
) *Provisioner {
	k.waitForReady = f

	return k
}

// NewStreamLoggerForTest creates a streamLogger for testing.
func NewStreamLoggerForTest(w interface {
	Write(p []byte) (n int, err error)
},
) log.Logger {
	return &streamLogger{writer: w}
}

// SetNameForTest exposes setName for unit testing.
func SetNameForTest(name, kindConfigName string) string {
	return setName(name, kindConfigName)
}
