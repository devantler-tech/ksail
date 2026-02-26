package hetzner_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	hetzner "github.com/devantler-tech/ksail/v5/pkg/svc/installer/internal/hetzner"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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

func TestEnsureSecret_ConcurrentCreation(t *testing.T) {
	t.Parallel()

	token := "concurrent-token"
	clientset := fake.NewClientset()

	const goroutines = 10

	var waitGroup sync.WaitGroup

	errs := make([]error, goroutines)

	// startCh is a barrier to ensure all goroutines begin EnsureSecretForTest together.
	startCh := make(chan struct{})

	for goroutineIdx := range goroutines {
		waitGroup.Go(func() {
			<-startCh

			errs[goroutineIdx] = hetzner.EnsureSecretForTest(context.Background(), clientset, token)
		})
	}

	close(startCh)

	waitGroup.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d returned unexpected error: %v", i, err)
		}
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
