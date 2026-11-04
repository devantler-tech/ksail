package kubernetes_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

type kindPoolCase struct {
	name      string
	address   string
	podCIDRs  []string
	wantPool  string
	readError bool
}

func kindPoolCases() []kindPoolCase {
	return []kindPoolCase{
		{
			name: "observed outer Kind network", address: "172.18.0.8",
			podCIDRs: []string{"10.244.0.0/16"}, wantPool: "198.18.0.0/16",
		},
		{
			// Every RFC 1918 range is in use on the host; the allow-list is outside all of them.
			name: "host uses all private ranges", address: "172.18.0.8",
			podCIDRs: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			wantPool: "198.18.0.0/16",
		},
		{name: "host occupies first candidate", address: "198.18.0.8", wantPool: "198.19.0.0/16"},
		{
			name:     "IPv6 host",
			address:  "2001:db8::8",
			podCIDRs: []string{"fd01::/64"},
			wantPool: "198.18.0.0/16",
		},
		{name: "unknown address", address: ""},
		{name: "malformed address", address: "not-an-address"},
		{name: "malformed pod range", address: "172.18.0.8", podCIDRs: []string{"not-a-prefix"}},
		{
			// Never fall back to a range outside the allow-list.
			name: "host occupies every candidate", address: "172.18.0.8",
			podCIDRs: []string{"198.18.0.0/15"},
		},
		{name: "failed host inventory", address: "172.18.0.8", readError: true},
	}
}

func TestKindDinDPoolAvoidsHostNetworks(t *testing.T) {
	t.Parallel()

	for _, testCase := range kindPoolCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			node := &corev1.Node{
				Name: "fixture-host",
				Spec: corev1.NodeSpec{PodCIDRs: testCase.podCIDRs},
				Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: testCase.address},
				}},
			}

			client := fake.NewClientset(node)
			if testCase.readError {
				client.PrependReactor(
					"list",
					"nodes",
					func(clienttesting.Action) (bool, runtime.Object, error) {
						return true, nil, assert.AnError
					},
				)
			}

			err := createKindNetworkFixture(t, client, true)
			if testCase.wantPool == "" {
				require.Error(t, err)

				if testCase.readError {
					require.ErrorIs(t, err, assert.AnError)
				}

				assertNoNetworkFixtureCreates(t, client)

				return
			}

			require.NoError(t, err)
			pod := kindNetworkFixturePod(t, client)
			assert.Contains(
				t,
				pod.Spec.Containers[0].Args,
				"--default-address-pool=base="+testCase.wantPool+",size=24",
			)
			assert.Contains(t, strings.Join(pod.Spec.Containers[0].Args, " "), "--mtu=")
		})
	}
}

func TestOtherDinDDistributionsKeepDaemonArguments(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)
	require.NoError(
		t,
		prov.CreateDinDPod(t.Context(), "fixture", "Talos", v1alpha1.KubernetesPersistence{}),
	)
	pod := kindNetworkFixturePod(t, client)
	assert.Len(t, pod.Spec.Containers[0].Args, 1)
	assert.True(t, strings.HasPrefix(pod.Spec.Containers[0].Args[0], "--mtu="))
}

func TestKindDinDPoolRequiresCompleteHostInventory(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"complete", "later-page-error", "repeated-page", "empty-inventory"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			client := fake.NewClientset()
			client.PrependReactor(
				"list",
				"nodes",
				func(action clienttesting.Action) (bool, runtime.Object, error) {
					options := kindNetworkListOptions(t, action)

					if mode == "empty-inventory" {
						return true, &corev1.NodeList{}, nil
					}

					address := "172.17.0.8"

					page := &corev1.NodeList{Continue: "next"}
					if options.Continue != "" {
						if mode == "later-page-error" {
							return true, nil, assert.AnError
						}

						address = "198.18.0.8"

						if mode != "repeated-page" {
							page.Continue = ""
						}
					}

					page.Items = []corev1.Node{{Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: address},
						},
					}}}

					return true, page, nil
				},
			)

			err := createKindNetworkFixture(t, client, false)
			if mode != "complete" {
				require.Error(t, err)

				assertNoNetworkFixtureCreates(t, client)

				return
			}

			require.NoError(t, err)
			pod := kindNetworkFixturePod(t, client)
			assert.Contains(
				t,
				pod.Spec.Containers[0].Args,
				"--default-address-pool=base=198.19.0.0/16,size=24",
			)
		})
	}
}

func kindNetworkFixturePod(t *testing.T, client *fake.Clientset) *corev1.Pod {
	t.Helper()
	pod, err := client.CoreV1().
		Pods(kubeprovider.NamespaceName("fixture")).
		Get(t.Context(), kubeprovider.DinDPodName, metav1.GetOptions{})
	require.NoError(t, err)

	return pod
}

func createKindNetworkFixture(t *testing.T, client *fake.Clientset, persistent bool) error {
	t.Helper()

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	err = prov.CreateDinDPod(
		t.Context(),
		"fixture",
		"Vanilla",
		v1alpha1.KubernetesPersistence{Enabled: persistent},
	)
	if err != nil {
		return fmt.Errorf("create nested fixture: %w", err)
	}

	return nil
}

func kindNetworkListOptions(t *testing.T, action clienttesting.Action) metav1.ListOptions {
	t.Helper()

	listAction, ok := action.(interface{ GetListOptions() metav1.ListOptions })
	require.True(t, ok)

	return listAction.GetListOptions()
}

func assertNoNetworkFixtureCreates(t *testing.T, client *fake.Clientset) {
	t.Helper()

	for _, action := range client.Actions() {
		assert.NotEqual(
			t,
			"create",
			action.GetVerb(),
			"unknown inventory must not create any resources",
		)
	}
}

func TestKindDinDRejectsReusedPodWithDifferentPool(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		distribution string
		args         []string
		wantErr      bool
	}{
		{
			name: "matching pool", distribution: "Vanilla",
			args: []string{"--mtu=1400", "--default-address-pool=base=198.18.0.0/16,size=24"},
		},
		{name: "pre-pool pod", distribution: "Vanilla", args: []string{"--mtu=1400"}, wantErr: true},
		{
			name: "different pool", distribution: "Vanilla",
			args:    []string{"--mtu=1400", "--default-address-pool=base=198.19.0.0/16,size=24"},
			wantErr: true,
		},
		{name: "other distribution", distribution: "Talos", args: []string{"--mtu=1400"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := fake.NewClientset(
				&corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "172.16.0.8"},
				}}},
				&corev1.Pod{
					Name:      kubeprovider.DinDPodName,
					Namespace: kubeprovider.NamespaceName("fixture"),
					Spec: corev1.PodSpec{Containers: []corev1.Container{
						{Name: kubeprovider.DinDContainerName, Args: testCase.args},
					}},
				},
			)
			prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
			require.NoError(t, err)

			err = prov.CreateDinDPod(
				t.Context(), "fixture", testCase.distribution, v1alpha1.KubernetesPersistence{},
			)
			if testCase.wantErr {
				require.ErrorIs(t, err, kubeprovider.ErrDinDAddressPool)

				return
			}

			require.NoError(t, err)

			if testCase.distribution != "Vanilla" {
				for _, action := range client.Actions() {
					assert.False(
						t,
						action.Matches("get", "pods"),
						"no pool comparison outside Vanilla",
					)
				}
			}
		})
	}
}
