package hetzner_test

import (
	"context"
	"testing"

	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestEnsureSecret_CreateError(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	// Make Get return NotFound (default for empty clientset), but Create fails.
	clientset.PrependReactor(
		"create",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, assert.AnError
		},
	)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "some-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create secret")
}

func TestEnsureSecret_UpdateError(t *testing.T) {
	t.Parallel()

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "1",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	}

	clientset := fake.NewClientset(existing)

	// Make Update fail permanently.
	clientset.PrependReactor(
		"update",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, assert.AnError
		},
	)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "new-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update secret")
}

func TestEnsureSecret_DifferentToken_SuccessfulUpdate(t *testing.T) {
	t.Parallel()

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "5",
		},
		Data: map[string][]byte{
			"token": []byte("outdated-token"),
		},
	}

	clientset := fake.NewClientset(existing)

	newToken := "brand-new-token"
	err := hetzner.EnsureSecretForTest(context.Background(), clientset, newToken)

	require.NoError(t, err)

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, newToken, string(got.Data["token"]))
}

func TestEnsureSecret_AlreadyExists_GetFails(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	callCount := 0

	clientset.PrependReactor(
		"get",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			callCount++
			if callCount == 1 {
				// First Get: NotFound, triggers Create path.
				return false, nil, nil
			}
			// Second Get (after AlreadyExists): return error.
			return true, nil, assert.AnError
		},
	)

	// Create returns AlreadyExists to trigger the race-handling path.
	clientset.PrependReactor(
		"create",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewAlreadyExists(
				schema.GroupResource{Group: "", Resource: "secrets"},
				hetzner.SecretName,
			)
		},
	)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "some-token")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get existing secret")
}

func TestErrTokenNotSet(t *testing.T) {
	t.Parallel()

	assert.Contains(t, hetzner.ErrTokenNotSet.Error(), "HCLOUD_TOKEN")
	assert.Contains(t, hetzner.ErrTokenNotSet.Error(), "not set")
}
