//nolint:testpackage // White-box tests verify the unexported secret-merge boundary.
package registryresolver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/registryauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const (
	testGHCRHost        = "ghcr.io"
	testPrivateHost     = "registry.example.com"
	testGHCRTokenEnvVar = "GHCR_TOKEN"
	testUsername        = "user"
)

func TestTryFluxSecretDoesNotReusePullOnlyCredentialsForPush(t *testing.T) {
	t.Parallel()

	secret := buildFluxCredentialSecret(t, testGHCRHost, "pull-token")
	secret.Annotations = map[string]string{
		registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: testGHCRHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.False(t, found)
	assert.Empty(t, info.Username)
	assert.Empty(t, info.Password)
}

// A pull-only secret stays ineligible for push even when a push token happens to sit in
// the process environment: push credentials are resolved from configuration
// (LocalRegistry.Credentials), never recovered from ambient environment state here.
func TestTryFluxSecretIgnoresAmbientPushTokenForPullOnlyCredentials(t *testing.T) {
	t.Setenv(testGHCRTokenEnvVar, "push-token")

	secret := buildFluxCredentialSecret(t, testGHCRHost, "pull-token")
	secret.Annotations = map[string]string{
		registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: testGHCRHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.False(t, found)
	assert.Empty(t, info.Username)
	assert.Empty(t, info.Password)
}

// The pull-only refusal is registry-agnostic: no host carries special meaning.
func TestTryFluxSecretDoesNotReusePullOnlyCredentialsOnNonGHCRHost(t *testing.T) {
	t.Parallel()

	secret := buildFluxCredentialSecret(t, testPrivateHost, "pull-token")
	secret.Annotations = map[string]string{
		registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: testPrivateHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.False(t, found)
	assert.Empty(t, info.Username)
	assert.Empty(t, info.Password)
}

func TestTryFluxSecretRetainsUnmarkedPushCredentials(t *testing.T) {
	t.Parallel()

	secret := buildFluxCredentialSecret(t, testGHCRHost, "push-token")
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: testGHCRHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.True(t, found)
	assert.Equal(t, testUsername, info.Username)
	assert.Equal(t, "push-token", info.Password)
}

func TestTryArgoCDSecretDoesNotReusePullOnlyCredentialsForPush(t *testing.T) {
	t.Parallel()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argoCDSecretName,
			Namespace: argoCDSecretNamespace,
			Annotations: map[string]string{
				registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
			},
		},
		Data: map[string][]byte{
			"username": []byte(testUsername),
			"password": []byte("pull-token"),
		},
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: testGHCRHost, IsExternal: true}

	found := tryArgoCDSecret(context.Background(), clientset, info)

	assert.False(t, found)
	assert.Empty(t, info.Username)
	assert.Empty(t, info.Password)
}

// buildFluxCredentialSecret builds a Flux docker-config Secret for testUsername on the
// given host, holding the supplied password.
func buildFluxCredentialSecret(t *testing.T, host, password string) *corev1.Secret {
	t.Helper()

	auth := base64.StdEncoding.EncodeToString([]byte(testUsername + ":" + password))
	dockerConfig, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			host: map[string]string{"auth": auth},
		},
	})
	require.NoError(t, err)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fluxSecretName,
			Namespace: fluxSecretNamespace,
		},
		Data: map[string][]byte{corev1.DockerConfigJsonKey: dockerConfig},
	}
}
