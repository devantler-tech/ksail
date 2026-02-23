package hetzner

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureSecret_TokenNotSet(t *testing.T) {
	t.Setenv(TokenEnvVar, "")

	err := EnsureSecret(context.Background(), "", "")
	if err != ErrTokenNotSet {
		t.Errorf("expected ErrTokenNotSet, got %v", err)
	}
}

func TestEnsureSecret_CreateWhenNotFound(t *testing.T) {
	t.Parallel()
	token := "new-token-789"

	clientset := fake.NewSimpleClientset()
	err := ensureSecret(context.Background(), clientset, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(Namespace).Get(context.Background(), SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created secret: %v", err)
	}
	if got.StringData["token"] != token {
		t.Errorf("expected token %q, got %q", token, got.StringData["token"])
	}
}

func TestEnsureSecret_UpdatePreservesResourceVersion(t *testing.T) {
	t.Parallel()
	updatedToken := "updated-token-456"

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            SecretName,
			Namespace:       Namespace,
			ResourceVersion: "12345",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	}
	clientset := fake.NewSimpleClientset(existing)

	err := ensureSecret(context.Background(), clientset, updatedToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := clientset.CoreV1().Secrets(Namespace).Get(context.Background(), SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated secret: %v", err)
	}
	if got.StringData["token"] != updatedToken {
		t.Errorf("expected updated token %q, got %q", updatedToken, got.StringData["token"])
	}
}
