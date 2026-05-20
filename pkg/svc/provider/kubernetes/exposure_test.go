package kubernetes_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newTestProvider(t *testing.T, objects ...runtime.Object) *kubeprovider.Provider {
	t.Helper()

	prov, err := kubeprovider.NewProvider(fake.NewClientset(objects...), v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	return prov
}

func TestExposureResultServerURL(t *testing.T) {
	t.Parallel()

	result := &kubeprovider.ExposureResult{Address: "1.2.3.4", Port: 6443, Kind: kubeprovider.ExposureNodePort}
	assert.Equal(t, "https://1.2.3.4:6443", result.ServerURL())
}

func TestHostnameOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"url_with_port", "https://1.2.3.4:6443", "1.2.3.4"},
		{"url_hostname", "https://api.example.com:6443", "api.example.com"},
		{"host_port", "1.2.3.4:6443", "1.2.3.4"},
		{"bare_ip", "1.2.3.4", "1.2.3.4"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, kubeprovider.HostnameOnlyForTest(testCase.input))
		})
	}
}

func nodeWithAddresses(name string, addrs ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     corev1.NodeStatus{Addresses: addrs},
	}
}

func TestPickNodeAddress(t *testing.T) {
	t.Parallel()

	t.Run("prefers_external_ip", func(t *testing.T) {
		t.Parallel()

		prov := newTestProvider(t, nodeWithAddresses("n1",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
			corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "5.6.7.8"},
		))

		addr, err := kubeprovider.PickNodeAddressForTest(prov, context.Background(), "https://10.0.0.99:6443")
		require.NoError(t, err)
		assert.Equal(t, "5.6.7.8", addr)
	})

	t.Run("falls_back_to_host_address", func(t *testing.T) {
		t.Parallel()

		prov := newTestProvider(t, nodeWithAddresses("n1",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
		))

		addr, err := kubeprovider.PickNodeAddressForTest(prov, context.Background(), "https://127.0.0.1:6443")
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", addr)
	})

	t.Run("falls_back_to_internal_ip", func(t *testing.T) {
		t.Parallel()

		prov := newTestProvider(t, nodeWithAddresses("n1",
			corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
		))

		addr, err := kubeprovider.PickNodeAddressForTest(prov, context.Background(), "")
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.1", addr)
	})
}

func nodePortSpec() kubeprovider.APIExposureSpec {
	return kubeprovider.APIExposureSpec{
		ClusterName:     "demo",
		Namespace:       "ksail-demo",
		BackendSelector: map[string]string{kubeprovider.LabelApp: kubeprovider.DinDPodName},
		APIPort:         kubeprovider.DinDAPIServerPort,
	}
}

func TestExposeViaNodePort(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset(nodeWithAddresses("n1",
		corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "5.6.7.8"},
	))
	// The fake client does not run the NodePort allocator, so simulate it.
	client.PrependReactor("create", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		svc, ok := action.(k8stesting.CreateAction).GetObject().(*corev1.Service)
		if ok && len(svc.Spec.Ports) > 0 {
			svc.Spec.Ports[0].NodePort = 31234
		}

		// Fall through so the (mutated) Service is stored by the default tracker.
		return false, nil, nil
	})

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	result, err := kubeprovider.ExposeViaNodePortForTest(prov, context.Background(), nodePortSpec())
	require.NoError(t, err)
	assert.Equal(t, kubeprovider.ExposureNodePort, result.Kind)
	assert.Equal(t, "5.6.7.8", result.Address)
	assert.Equal(t, int32(31234), result.Port)

	svc, err := client.CoreV1().Services("ksail-demo").Get(context.Background(), kubeprovider.APIServiceName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
	assert.Equal(t, kubeprovider.DinDPodName, svc.Spec.Selector[kubeprovider.LabelApp])
}

func TestExposeViaLoadBalancer(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	// Simulate a LoadBalancer controller having assigned an ingress address.
	client.PrependReactor("get", "services", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: kubeprovider.APIServiceName, Namespace: "ksail-demo"},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{IP: "9.9.9.9"}},
				},
			},
		}, nil
	})

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	result, err := kubeprovider.ExposeViaLoadBalancerForTest(prov, context.Background(), nodePortSpec())
	require.NoError(t, err)
	assert.Equal(t, kubeprovider.ExposureLoadBalancer, result.Kind)
	assert.Equal(t, "9.9.9.9", result.Address)
	assert.Equal(t, int32(kubeprovider.DinDAPIServerPort), result.Port)
}

func TestResolveExposureFallsBackToLoadBalancer(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	client.PrependReactor("get", "services", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: kubeprovider.APIServiceName, Namespace: "ksail-demo"},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}},
				},
			},
		}, nil
	})

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	// No GatewayClassName => Gateway tier is skipped, LoadBalancer tier succeeds.
	result, err := prov.ResolveExposure(context.Background(), nil, kubeprovider.APIExposureSpec{
		ClusterName: "demo",
		APIPort:     kubeprovider.DinDAPIServerPort,
	})
	require.NoError(t, err)
	assert.Equal(t, kubeprovider.ExposureLoadBalancer, result.Kind)
	assert.Equal(t, "lb.example.com", result.Address)
}

func TestUpdateAPIServiceTargetPort(t *testing.T) {
	t.Parallel()

	existing := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: kubeprovider.APIServiceName, Namespace: "ksail-demo"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Name: "https", Port: kubeprovider.DinDAPIServerPort}},
		},
	}

	client := fake.NewClientset(existing)
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	err = prov.UpdateAPIServiceTargetPort(context.Background(), "demo", 32456)
	require.NoError(t, err)

	svc, err := client.CoreV1().Services("ksail-demo").Get(context.Background(), kubeprovider.APIServiceName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(32456), svc.Spec.Ports[0].TargetPort.IntVal)
}
