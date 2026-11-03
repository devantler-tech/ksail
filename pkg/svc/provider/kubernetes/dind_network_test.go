package kubernetes_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDinDRefusesExistingPodWithDifferentAllocationPolicy(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, pool string
		wantError  bool
	}{
		{name: "same policy", pool: "base=10.16.0.0/12,size=16"},
		{name: "old daemon without policy", wantError: true},
		{name: "different policy", pool: "base=172.16.0.0/12,size=16", wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			client := fake.NewClientset()
			provider, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
			require.NoError(t, err)

			var pools []string
			if testCase.pool != "" {
				pools = append(pools, testCase.pool)
			}

			require.NoError(t, provider.CreateDinDPodWithPoolsForTest(ctx, "existing", "Vanilla",
				v1alpha1.KubernetesPersistence{}, pools...))

			namespace := kubeprovider.NamespaceName("existing")
			before, err := client.CoreV1().
				Pods(namespace).
				Get(ctx, kubeprovider.DinDPodName, metav1.GetOptions{})
			require.NoError(t, err)

			err = provider.CreateDinDPodWithPoolsForTest(
				ctx,
				"existing",
				"Vanilla",
				v1alpha1.KubernetesPersistence{},
				"base=10.16.0.0/12,size=16",
			)
			if testCase.wantError {
				require.ErrorIs(t, err, kubeprovider.ErrDinDNetworkPolicyMismatch)
			} else {
				require.NoError(t, err)
			}

			after, err := client.CoreV1().
				Pods(namespace).
				Get(ctx, kubeprovider.DinDPodName, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(
				t,
				before,
				after,
				"reuse must never replace or migrate the existing daemon",
			)
		})
	}
}
