package hetzner_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	cfg := hetzner.ChartConfig{
		Name:        "hcloud-ccm",
		ReleaseName: "hcloud-ccm",
		ChartName:   "hcloud/hcloud-cloud-controller-manager",
		Version:     "1.0.0",
	}

	installer := hetzner.NewInstaller(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		cfg,
	)

	require.NotNil(t, installer, "expected installer to be created")
}

func TestChartConfig(t *testing.T) {
	t.Parallel()

	cfg := hetzner.ChartConfig{
		Name:        "hetzner-csi",
		ReleaseName: "hetzner-csi",
		ChartName:   "hcloud/hcloud-csi",
		Version:     "2.5.0",
	}

	assert.Equal(t, "hetzner-csi", cfg.Name)
	assert.Equal(t, "hetzner-csi", cfg.ReleaseName)
	assert.Equal(t, "hcloud/hcloud-csi", cfg.ChartName)
	assert.Equal(t, "2.5.0", cfg.Version)
}

func TestEnsureSecret_UpdateWithSameToken(t *testing.T) {
	t.Parallel()

	token := "same-token"

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "100",
		},
		Data: map[string][]byte{
			"token": []byte(token),
		},
	}

	clientset := fake.NewClientset(existing)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, token)
	require.NoError(t, err)

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, token, string(got.Data["token"]))
	assert.Equal(t, "100", got.ResourceVersion, "resource version should be unchanged")
}

func TestEnsureSecret_EmptyToken(t *testing.T) {
	t.Setenv(hetzner.TokenEnvVar, "")

	err := hetzner.EnsureSecret(context.Background(), "", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, hetzner.ErrTokenNotSet)
}

func TestEnsureSecret_GetError(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	clientset.PrependReactor(
		"get",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, assert.AnError
		},
	)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "some-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get secret")
}

func TestConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kube-system", hetzner.Namespace)
	assert.Equal(t, "hcloud", hetzner.SecretName)
	assert.Equal(t, "HCLOUD_TOKEN", hetzner.TokenEnvVar)
}
