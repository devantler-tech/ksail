package registryresolver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/registryauth"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// mergeCredentialsFromClusterSecrets retrieves credentials from GitOps engine secrets.
// It checks both Flux and ArgoCD secret locations since the registry URL was auto-discovered
// from GitOps resources but credentials may be stored in the cluster.
//
// Flux stores credentials in: flux-system/ksail-registry-credentials (Docker config JSON format).
// ArgoCD stores credentials in: argocd/ksail-local-registry-repo (plain username/password fields).
//
// The clients bundle supplies the kubeconfig/context the CLI resolved, so the
// secrets are read from the targeted cluster rather than the default kubeconfig.
func mergeCredentialsFromClusterSecrets(ctx context.Context, clients *Clients, info *Info) {
	clientset, err := clients.kubernetesClient()
	if err != nil {
		return
	}

	// Try Flux secret first (Docker config JSON format)
	if tryFluxSecret(ctx, clientset, info) {
		return
	}

	// Try ArgoCD secret (plain username/password format)
	tryArgoCDSecret(ctx, clientset, info)
}

// tryFluxSecret attempts to retrieve credentials from the Flux registry secret.
// Returns true if credentials were found and set.
func tryFluxSecret(ctx context.Context, clientset kubernetes.Interface, info *Info) bool {
	secret, err := clientset.CoreV1().Secrets(fluxSecretNamespace).Get(
		ctx,
		fluxSecretName,
		metav1.GetOptions{},
	)
	if err != nil {
		return false
	}

	// Parse Docker config JSON to extract credentials
	dockerConfigData, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return false
	}

	username, password := parseDockerConfigCredentials(dockerConfigData, info.Host)

	return applyClusterSecretCredentials(secret, info, username, password)
}

// tryArgoCDSecret attempts to retrieve credentials from the ArgoCD repository secret.
// Returns true if credentials were found and set.
func tryArgoCDSecret(
	ctx context.Context,
	clientset kubernetes.Interface,
	info *Info,
) bool {
	secret, err := clientset.CoreV1().Secrets(argoCDSecretNamespace).Get(
		ctx,
		argoCDSecretName,
		metav1.GetOptions{},
	)
	if err != nil {
		return false
	}

	// ArgoCD stores credentials as plain username/password in StringData/Data
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])

	return applyClusterSecretCredentials(secret, info, username, password)
}

// applyClusterSecretCredentials merges credentials into a push target while
// refusing to reuse cluster-resident pull-only passwords. For marked GHCR
// secrets, an ambient GHCR_TOKEN is the only eligible push credential.
func applyClusterSecretCredentials(
	secret *corev1.Secret,
	info *Info,
	username, password string,
) bool {
	if username == "" {
		return false
	}

	purpose := secret.Annotations[registryauth.CredentialPurposeAnnotation]
	if purpose == registryauth.PullCredentialPurpose {
		if !strings.EqualFold(strings.TrimSpace(info.Host), registryauth.GHCRHost) {
			return false
		}

		pushToken, exists := os.LookupEnv(registryauth.GHCRTokenEnvVar)
		if !exists || pushToken == "" {
			return false
		}

		password = pushToken
	}

	info.Username = username
	info.Password = password

	return true
}

// dockerConfig represents the Docker config.json structure.
type dockerConfig struct {
	Auths map[string]dockerAuthConfig `json:"auths"`
}

// dockerAuthConfig represents auth config for a single registry.
type dockerAuthConfig struct {
	Auth string `json:"auth"`
}

// dockerHubCanonicalKey is the canonical Docker config key used by `docker login`
// and `kubectl create secret docker-registry` for Docker Hub.
const dockerHubCanonicalKey = "https://index.docker.io/v1/"

// parseDockerConfigCredentials extracts username and password from Docker config JSON.
func parseDockerConfigCredentials(configData []byte, host string) (string, string) {
	var config dockerConfig

	err := json.Unmarshal(configData, &config)
	if err != nil {
		return "", ""
	}

	// Try exact host match first, then try with https:// prefix.
	authConfig, found := config.Auths[host]
	if !found {
		authConfig, found = config.Auths["https://"+host]
	}

	// Fall back to Docker Hub's canonical key when looking up docker.io.
	if !found && host == "docker.io" {
		authConfig, found = config.Auths[dockerHubCanonicalKey]
	}

	if !found {
		return "", ""
	}

	// Decode base64 auth (format: "username:password")
	decoded, err := base64.StdEncoding.DecodeString(authConfig.Auth)
	if err != nil {
		return "", ""
	}

	parts := strings.SplitN(string(decoded), ":", credentialParts)
	if len(parts) != credentialParts {
		return "", ""
	}

	return parts[0], parts[1]
}
