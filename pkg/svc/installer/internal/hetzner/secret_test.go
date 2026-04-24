package hetzner_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/hetzner"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureSecret_TokenNotSet(t *testing.T) {
	t.Setenv(hetzner.TokenEnvVar, "")

	err := hetzner.EnsureSecret(context.Background(), "", "", nil)
	if !errors.Is(err, hetzner.ErrTokenNotSet) {
		t.Errorf("expected ErrTokenNotSet, got %v", err)
	}
}

func TestEnsureSecret_CreateWhenNotFound(t *testing.T) {
	t.Parallel()

	token := "new-token-789"

	clientset := fake.NewClientset()

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, token, nil)
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

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, updatedToken, nil)
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

			errs[goroutineIdx] = hetzner.EnsureSecretForTest(
				context.Background(),
				clientset,
				token,
				nil,
			)
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

func TestEnsureSecret_MergesExtraDataIntoSecret(t *testing.T) {
	t.Parallel()

	token := "test-token"
	extraData := map[string][]byte{
		"network": []byte("dev-network"),
	}

	clientset := fake.NewClientset()

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, token, extraData)
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

	if string(got.Data["network"]) != "dev-network" {
		t.Errorf("expected network %q, got %q", "dev-network", string(got.Data["network"]))
	}
}

func TestEnsureSecret_PreservesExistingKeysOnMerge(t *testing.T) {
	t.Parallel()

	// Simulate CCM having already written "network" into the secret.
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            hetzner.SecretName,
			Namespace:       hetzner.Namespace,
			ResourceVersion: "1",
		},
		Data: map[string][]byte{
			"token":   []byte("old-token"),
			"network": []byte("dev-network"),
		},
	}

	clientset := fake.NewClientset(existing)

	// CSI installer writes only "token" (no extraData). The "network" key must survive.
	err := hetzner.EnsureSecretForTest(context.Background(), clientset, "new-token", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}

	if string(got.Data["token"]) != "new-token" {
		t.Errorf("expected token %q, got %q", "new-token", string(got.Data["token"]))
	}

	if string(got.Data["network"]) != "dev-network" {
		t.Errorf("expected network key to be preserved as %q, got %q", "dev-network", string(got.Data["network"]))
	}
}

func TestEnsureSecret_ExtraDataCannotOverrideToken(t *testing.T) {
	t.Parallel()

	realToken := "real-hcloud-token"

	// Attempt to sneak a different token value via extraData.
	extraData := map[string][]byte{
		"token":   []byte("malicious-override"),
		"network": []byte("dev-network"),
	}

	clientset := fake.NewClientset()

	err := hetzner.EnsureSecretForTest(context.Background(), clientset, realToken, extraData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(hetzner.Namespace).Get(
		context.Background(), hetzner.SecretName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}

	// The "token" key must always come from the HCLOUD_TOKEN env var, not extraData.
	if string(got.Data["token"]) != realToken {
		t.Errorf("expected token from env var %q, got %q (extraData should not override token)", realToken, string(got.Data["token"]))
	}

	if string(got.Data["network"]) != "dev-network" {
		t.Errorf("expected network %q, got %q", "dev-network", string(got.Data["network"]))
	}
}
