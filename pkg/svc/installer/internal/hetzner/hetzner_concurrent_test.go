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

// TestEnsureSecret_CreateOnNotFound verifies that a secret is created when
// it does not exist.
func TestEnsureSecret_CreateOnNotFound(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "new-token")
	require.NoError(t, err)

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "new-token", string(got.Data["token"]))
}

// TestEnsureSecret_UpdateWithDifferentToken verifies that an existing secret
// is updated when the token differs.
func TestEnsureSecret_UpdateWithDifferentToken(t *testing.T) {
	t.Parallel()

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "42",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	}

	clientset := fake.NewClientset(existing)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "new-token")
	require.NoError(t, err)

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "new-token", string(got.Data["token"]))
}

// TestEnsureSecret_CreateConflictThenUpdate verifies the createOrUpdateOnConflict
// path when Create returns AlreadyExists and the subsequent Get + Update succeeds.
func TestEnsureSecret_CreateConflictThenUpdate(t *testing.T) {
	t.Parallel()

	// Pre-create the secret so that after AlreadyExists, the Get succeeds.
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

	getCallCount := 0

	clientset.PrependReactor(
		"get",
		"secrets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			getCallCount++
			if getCallCount == 1 {
				// First Get: return NotFound to enter the create path
				return true, nil, apierrors.NewNotFound(
					schema.GroupResource{Group: "", Resource: "secrets"},
					hetzner.SecretName,
				)
			}
			// Subsequent Gets: pass through to the fake clientset
			return false, nil, nil
		},
	)

	// Create returns AlreadyExists to simulate concurrent creation.
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

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "updated-token")
	require.NoError(t, err)
}
