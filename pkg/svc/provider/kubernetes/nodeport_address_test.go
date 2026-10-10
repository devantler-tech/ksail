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
	"k8s.io/client-go/kubernetes/fake"
)

func TestNodePortRejectsUnusableNodeIPs(t *testing.T) {
	t.Parallel()

	for _, address := range []string{
		"127.0.0.1", "127.0.0.2", "::1", "::ffff:127.0.0.1",
		"0.0.0.0", "::", "224.0.0.1", "ff02::1", "255.255.255.255",
		"169.254.1.2", "fe80::1", "node.example.com", "10.0.0.1:6443", "bad-ip",
	} {
		for _, addressType := range []corev1.NodeAddressType{
			corev1.NodeExternalIP, corev1.NodeInternalIP,
		} {
			t.Run(string(addressType)+"/"+address, func(t *testing.T) {
				t.Parallel()

				prov := newTestProvider(t, nodeWithAddresses(corev1.NodeAddress{
					Type: addressType, Address: address,
				}))
				addr, err := kubeprovider.PickNodeAddressForTest(
					prov, context.Background(), "https://127.0.0.1:6443",
				)
				require.ErrorIs(t, err, kubeprovider.ErrNoNodeAddress)
				assert.Empty(t, addr, "an unusable node IP cannot become a saved endpoint")
			})
		}
	}
}

func TestResolveExposureRejectsUnusableNodePortEndpoints(t *testing.T) {
	t.Parallel()

	for _, validIP := range []string{"", "10.0.0.1", "fd00::9"} {
		t.Run(validIP, func(t *testing.T) {
			t.Parallel()

			client := fake.NewClientset(nodeWithAddresses(
				corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "127.0.0.1"},
				corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "0.0.0.0"},
				corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: validIP},
			), &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: kubeprovider.APIServiceName, Namespace: testExposureNS,
				},
				Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
					Name: "https", Port: 6443, NodePort: 31234,
				}}},
			})
			prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
			require.NoError(t, err)

			spec := nodePortSpec()
			spec.SkipLoadBalancer = true
			spec.HostAddress = "https://127.0.0.1:6443"

			result, err := prov.ResolveExposure(context.Background(), nil, spec)
			if validIP == "" {
				require.ErrorIs(t, err, kubeprovider.ErrNoNodeAddress)
				assert.Nil(t, result)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, validIP, result.Address)
			assert.Equal(t, kubeprovider.ExposureNodePort, result.Kind)
			assert.Equal(t, int32(31234), result.Port)
		})
	}
}

func TestNodePortSkipsUnusableNodesAndPreservesAddressPriority(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		addresses []corev1.NodeAddress
		host      string
		expected  string
	}{
		{
			name: "later external IP wins",
			addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "127.0.0.1"},
				{Type: corev1.NodeExternalIP, Address: "198.51.100.9"},
				{Type: corev1.NodeInternalIP, Address: testNodeInternalIP},
			},
			host: "https://10.0.0.99:6443", expected: "198.51.100.9",
		},
		{
			name: "host precedes internal IP",
			addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "0.0.0.0"},
				{Type: corev1.NodeInternalIP, Address: testNodeInternalIP},
			},
			host: "https://api.example.com:6443", expected: "api.example.com",
		},
		{
			name: "private IPv6 remains usable",
			addresses: []corev1.NodeAddress{
				{Type: corev1.NodeExternalIP, Address: "ff02::1"},
				{Type: corev1.NodeInternalIP, Address: "::1"},
				{Type: corev1.NodeInternalIP, Address: "fd00::9"},
			},
			host: "https://localhost:6443", expected: "fd00::9",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := newTestProvider(t, nodeWithAddresses(testCase.addresses...))
			addr, err := kubeprovider.PickNodeAddressForTest(
				prov, context.Background(), testCase.host,
			)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, addr)
		})
	}
}
