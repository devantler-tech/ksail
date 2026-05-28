package kubernetes_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kubeprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWaitForDefaultServiceAccount_Present(t *testing.T) {
	t.Parallel()

	namespace := kubeprovider.NamespaceName("test-cluster")
	client := fake.NewClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: namespace},
	})

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	require.NoError(t, prov.WaitForDefaultServiceAccount(context.Background(), "test-cluster"))
}

func TestWaitForDefaultServiceAccount_AbsentReturnsBeforeHanging(t *testing.T) {
	t.Parallel()

	// No default ServiceAccount exists; a short context deadline must make the wait return
	// promptly with a deadline error rather than blocking for the full readiness timeout.
	client := fake.NewClientset()

	prov, err := kubeprovider.NewProvider(client, v1alpha1.OptionsKubernetes{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = prov.WaitForDefaultServiceAccount(ctx, "test-cluster")
	require.Error(t, err)
}
