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

	// Pre-populate the secret so that Create will naturally return
	// AlreadyExists, simulating another installer winning the race.
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "1",
		},
		Data: map[string][]byte{"token": []byte("old-token")},
	}
	clientset := fake.NewClientset(existing)

	// Make the first Get return NotFound so ensureSecret takes the Create
	// path, which will hit AlreadyExists and fall back to Get+Update.
	var getCalled atomic.Bool
	clientset.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if getCalled.CompareAndSwap(false, true) {
			return true, nil, apierrors.NewNotFound(
				schema.GroupResource{Group: "", Resource: "secrets"}, hetzner.SecretName,
			)
		}
		return false, nil, nil
	})

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, token)
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
