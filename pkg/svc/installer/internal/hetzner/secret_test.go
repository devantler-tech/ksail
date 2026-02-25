package hetzner_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	hetzner "github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/hetzner"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestEnsureSecret_TokenNotSet(t *testing.T) {
	t.Setenv(hetzner.TokenEnvVar, "")

	err := hetzner.EnsureSecret(context.Background(), "", "")
	if !errors.Is(err, hetzner.ErrTokenNotSet) {
		t.Errorf("expected ErrTokenNotSet, got %v", err)
	}
}

func TestEnsureSecret_CreateWhenNotFound(t *testing.T) {
	t.Parallel()

	token := "new-token-789"

	clientset := fake.NewClientset()

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get created secret: %v", err)
	}

	if string(got.Data["token"]) != token {
		t.Errorf("expected token %q, got %q", token, string(got.Data["token"]))
	}
}

func TestEnsureSecret_UpdatePreservesResourceVersion(t *testing.T) {
	t.Parallel()

	updatedToken := "updated-token-456"

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "12345",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	}

	clientset := fake.NewClientset(existing)

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, updatedToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get updated secret: %v", err)
	}

	if string(got.Data["token"]) != updatedToken {
		t.Errorf("expected updated token %q, got %q", updatedToken, string(got.Data["token"]))
	}
}

func TestEnsureSecret_AlreadyExistsRace(t *testing.T) {
	t.Parallel()

	token := "race-token-123"

	clientset := fake.NewClientset()

	// Simulate a race: the first Create returns AlreadyExists, as if another
	// goroutine created the secret between our Get (NotFound) and Create.
	var created atomic.Bool
	clientset.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if created.CompareAndSwap(false, true) {
			// First call: let the real Create succeed so the secret exists.
			return false, nil, nil
		}

		// Should not be reached, but guard anyway.
		return false, nil, nil
	})

	// Pre-create so the second caller's Create hits AlreadyExists.
	_, err := clientset.CoreV1().Secrets(hetzner.Namespace).Create(
		context.Background(),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hetzner.SecretName,
				Namespace: hetzner.Namespace,
			},
			Data: map[string][]byte{"token": []byte("old-token")},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("pre-create failed: %v", err)
	}

	// Now make Get return NotFound on the first call, simulating the race
	// where the secret didn't exist when we checked but was created before
	// our Create call.
	var getCalled atomic.Bool
	clientset.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if getCalled.CompareAndSwap(false, true) {
			return true, nil, apierrors.NewNotFound(
				schema.GroupResource{Group: "", Resource: "secrets"}, hetzner.SecretName,
			)
		}
		// Subsequent Gets use the default (real) handler.
		return false, nil, nil
	})

	err = hetzner.EnsureSecretForTest(context.Background(), clientset, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}

	if string(got.Data["token"]) != token {
		t.Errorf("expected token %q, got %q", token, string(got.Data["token"]))
	}
}
