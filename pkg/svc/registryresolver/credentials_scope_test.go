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

func TestTryFluxSecretDoesNotReusePullOnlyCredentialsForPush(t *testing.T) {
	t.Setenv(registryauth.GHCRTokenEnvVar, "")

	secret := buildFluxCredentialSecret(t, "user", "pull-token")
	secret.Annotations = map[string]string{
		registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: registryauth.GHCRHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.False(t, found)
	assert.Empty(t, info.Username)
	assert.Empty(t, info.Password)
}

func TestTryFluxSecretUsesAmbientPushTokenForPullOnlyCredentials(t *testing.T) {
	t.Setenv(registryauth.GHCRTokenEnvVar, "push-token")

	secret := buildFluxCredentialSecret(t, "user", "pull-token")
	secret.Annotations = map[string]string{
		registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: registryauth.GHCRHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.True(t, found)
	assert.Equal(t, "user", info.Username)
	assert.Equal(t, "push-token", info.Password)
}

func TestTryFluxSecretRetainsLegacyUnmarkedPushCredentials(t *testing.T) {
	t.Setenv(registryauth.GHCRTokenEnvVar, "")

	secret := buildFluxCredentialSecret(t, "user", "legacy-push-token")
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: registryauth.GHCRHost, IsExternal: true}

	found := tryFluxSecret(context.Background(), clientset, info)

	assert.True(t, found)
	assert.Equal(t, "user", info.Username)
	assert.Equal(t, "legacy-push-token", info.Password)
}

func TestTryArgoCDSecretDoesNotReusePullOnlyCredentialsForPush(t *testing.T) {
	t.Setenv(registryauth.GHCRTokenEnvVar, "")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      argoCDSecretName,
			Namespace: argoCDSecretNamespace,
			Annotations: map[string]string{
				registryauth.CredentialPurposeAnnotation: registryauth.PullCredentialPurpose,
			},
		},
		Data: map[string][]byte{
			"username": []byte("user"),
			"password": []byte("pull-token"),
		},
	}
	clientset := k8sfake.NewClientset(secret)
	info := &Info{Host: registryauth.GHCRHost, IsExternal: true}

	found := tryArgoCDSecret(context.Background(), clientset, info)

	assert.False(t, found)
	assert.Empty(t, info.Username)
	assert.Empty(t, info.Password)
}

func buildFluxCredentialSecret(t *testing.T, username, password string) *corev1.Secret {
	t.Helper()

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	dockerConfig, err := json.Marshal(map[string]any{
		"auths": map[string]any{
			registryauth.GHCRHost: map[string]string{"auth": auth},
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
