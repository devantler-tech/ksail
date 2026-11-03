package kindprovisioner_test

import (
	"context"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Missing startup pools let the inner Docker bridge claim the selected outer
// endpoint. Exercise Create up to the real pod-create boundary, without Docker.
func TestKubernetesCreateProtectsExposureBeforeStartingDinD(t *testing.T) {
	t.Parallel()
	checkDinDPlanAtPodCreation(t, false)
}

func TestKubernetesCreateRetainsForbiddenNodeListFallback(t *testing.T) {
	t.Parallel()
	checkDinDPlanAtPodCreation(t, true)
}

func checkDinDPlanAtPodCreation(t *testing.T, forbidden bool) {
	t.Helper()

	serviceIP := "10.32.0.10"
	if forbidden {
		serviceIP = "10.0.0.10"
	}

	client := nestedKindHostClient(forbidden, serviceIP)
	exposureResolved := false

	client.PrependReactor(
		"update",
		"services",
		func(ktesting.Action) (bool, runtime.Object, error) {
			exposureResolved = true

			return false, nil, nil
		},
	)

	stop := io.EOF

	var pod *corev1.Pod

	client.PrependReactor(
		"create",
		"pods",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			createAction, ok := action.(ktesting.CreateAction)
			require.True(t, ok)
			pod, ok = createAction.GetObject().(*corev1.Pod)
			require.True(t, ok)

			assert.True(
				t,
				exposureResolved,
				"the chosen endpoint must precede daemon configuration",
			)

			return true, nil, stop
		},
	)
	provisioner := newNestedKindFixture(t, client)
	require.ErrorIs(t, provisioner.Create(context.Background(), ""), stop)
	require.NotNil(t, pod)

	assertDinDPools(t, pod.Spec.Containers[0].Args, forbidden, serviceIP)

	nodeLists := 0

	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "nodes" {
			nodeLists++
		}
	}

	assert.Equal(t, 1, nodeLists, "reuse the existing exposure read")
}

func assertDinDPools(t *testing.T, args []string, forbidden bool, serviceIP string) {
	t.Helper()

	poolCount := 0

	for _, arg := range args {
		if !strings.HasPrefix(arg, "--default-address-pool=") {
			continue
		}

		poolCount++
		parts := strings.Split(strings.TrimPrefix(arg, "--default-address-pool="), ",")
		require.Len(t, parts, 2)
		prefix, parseErr := netip.ParsePrefix(strings.TrimPrefix(parts[0], "base="))
		require.NoError(t, parseErr)

		if !forbidden {
			assert.False(t, prefix.Contains(netip.MustParseAddr("10.0.1.2")))
			assert.False(
				t,
				prefix.Overlaps(netip.MustParsePrefix("10.16.0.0/16")),
				"retain already-read host pod prefixes",
			)
			assert.False(
				t,
				prefix.Overlaps(netip.MustParsePrefix("10.64.0.0/16")),
				"retain singular PodCIDR as returned",
			)
		}

		assert.False(
			t,
			prefix.Contains(netip.MustParseAddr(serviceIP)),
			"protect the known service IP without guessing its CIDR",
		)
		assert.False(
			t,
			prefix.Contains(netip.MustParseAddr("10.48.0.10")),
			"retain additional ClusterIPs",
		)
		assert.False(t, prefix.Contains(netip.MustParseAddr("172.18.0.2")))
		assert.Equal(t, "size=16", parts[1], "retain /16 capacity for each Docker network")
	}

	assert.Positive(t, poolCount, "a fresh daemon needs an explicit protected allocation policy")
}

func nestedKindHostClient(forbidden bool, serviceIP string) *fake.Clientset {
	client := fake.NewClientset(
		&corev1.ServiceAccount{Name: "default", Namespace: kubeprovider.NamespaceName("isolated")},
		&corev1.Node{
			Spec: corev1.NodeSpec{
				PodCIDR:  "10.64.0.0/16",
				PodCIDRs: []string{"10.16.0.0/16", "fd00:1::/56"},
			},
			Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "10.0.1.2"},
			}},
		},
		&corev1.Service{
			Name: kubeprovider.APIServiceName, Namespace: kubeprovider.NamespaceName("isolated"),
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeNodePort,
				ClusterIP: serviceIP, ClusterIPs: []string{serviceIP, "10.48.0.10", "fd00:3::10"},
				Ports: []corev1.ServicePort{{Name: "https", Port: 6443, NodePort: 31403}},
			},
		},
	)
	if forbidden {
		client.PrependReactor("list", "nodes", func(ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Resource: "nodes"},
				"",
				os.ErrPermission,
			)
		})
	}

	return client
}

func newNestedKindFixture(
	t *testing.T,
	client *fake.Clientset,
) *kindprovisioner.KubernetesProvisioner {
	t.Helper()

	provider, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)
	provisioner, err := kindprovisioner.NewKubernetesProvisioner(
		kindprovisioner.KubernetesProvisionerConfig{
			KubeconfigPath: filepath.Join(t.TempDir(), "config"),
			HostClientset:  client, K8sProvider: provider,
			RestConfig: &rest.Config{Host: "https://172.18.0.2:6443"},
			KindConfig: &v1alpha4.Cluster{Name: "isolated"}, APIServerPort: 6443,
		},
	)
	require.NoError(t, err)

	return provisioner
}
