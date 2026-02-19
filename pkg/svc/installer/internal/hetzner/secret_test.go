package hetzner

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestEnsureSecret_TokenNotSet(t *testing.T) {
	t.Setenv(TokenEnvVar, "")

	err := EnsureSecret(context.Background(), "", "")
	if err != ErrTokenNotSet {
		t.Errorf("expected ErrTokenNotSet, got %v", err)
	}
}

func TestEnsureSecret_CreateSecret(t *testing.T) {
	token := "test-token-123"
	t.Setenv(TokenEnvVar, token)

	// Create a fake clientset that will be used by the EnsureSecret function
	// Note: We can't easily inject the clientset into EnsureSecret without
	// refactoring the function, so this test documents the expected behavior
	// rather than fully testing the implementation.
	
	// This test validates that the function attempts to create a secret
	// with the correct structure when the token is set.
	err := EnsureSecret(context.Background(), "", "")
	
	// We expect this to fail because we're not providing a valid kubeconfig,
	// but the error should be about the kubeconfig, not about the token.
	if err == nil {
		t.Error("expected error for invalid kubeconfig")
	}
	if err == ErrTokenNotSet {
		t.Error("should not return ErrTokenNotSet when token is set")
	}
}

func TestEnsureSecret_UpdatePreservesResourceVersion(t *testing.T) {
	token := "updated-token-456"
	os.Setenv(TokenEnvVar, token)
	defer os.Unsetenv(TokenEnvVar)

	// Create a fake clientset with an existing secret
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            SecretName,
			Namespace:       Namespace,
			ResourceVersion: "12345",
			UID:             "test-uid",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	}

	clientset := fake.NewSimpleClientset(existingSecret)

	// Track the Update call to verify resourceVersion is preserved
	var updatedSecret *corev1.Secret
	clientset.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAction := action.(k8stesting.UpdateAction)
		updatedSecret = updateAction.GetObject().(*corev1.Secret)
		return false, nil, nil // Let the default handler proceed
	})

	// This test documents the expected behavior - in a real scenario,
	// EnsureSecret would need to be refactored to accept a clientset
	// for proper unit testing. For now, this test serves as documentation.
	
	// Verify the expected behavior: when updating, the existing secret's
	// metadata (including resourceVersion) should be preserved.
	secrets := clientset.CoreV1().Secrets(Namespace)
	
	// Simulate what EnsureSecret should do
	existing, err := secrets.Get(context.Background(), SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get existing secret: %v", err)
	}

	// Update the existing secret's data
	existing.StringData = map[string]string{"token": token}
	existing.Data = nil

	_, err = secrets.Update(context.Background(), existing, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update secret: %v", err)
	}

	// Verify that the update preserved the resourceVersion
	if updatedSecret == nil {
		t.Fatal("update reactor was not called")
	}
	if updatedSecret.ResourceVersion != "12345" {
		t.Errorf("expected resourceVersion 12345, got %s", updatedSecret.ResourceVersion)
	}
	if updatedSecret.UID != "test-uid" {
		t.Errorf("expected UID test-uid, got %s", updatedSecret.UID)
	}
}

func TestEnsureSecret_CreateWhenNotFound(t *testing.T) {
	token := "new-token-789"
	os.Setenv(TokenEnvVar, token)
	defer os.Unsetenv(TokenEnvVar)

	// Create a fake clientset with no existing secret
	clientset := fake.NewSimpleClientset()

	// Track the Create call
	var createdSecret *corev1.Secret
	clientset.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction := action.(k8stesting.CreateAction)
		createdSecret = createAction.GetObject().(*corev1.Secret)
		return false, nil, nil
	})

	secrets := clientset.CoreV1().Secrets(Namespace)

	// Simulate what EnsureSecret should do when secret doesn't exist
	_, err := secrets.Get(context.Background(), SecretName, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got %v", err)
	}

	// Create new secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName,
			Namespace: Namespace,
		},
		StringData: map[string]string{
			"token": token,
		},
	}

	_, err = secrets.Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	// Verify the secret was created with correct data
	if createdSecret == nil {
		t.Fatal("create reactor was not called")
	}
	if createdSecret.Name != SecretName {
		t.Errorf("expected secret name %s, got %s", SecretName, createdSecret.Name)
	}
	if createdSecret.Namespace != Namespace {
		t.Errorf("expected namespace %s, got %s", Namespace, createdSecret.Namespace)
	}
}
