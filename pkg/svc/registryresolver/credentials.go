package registryresolver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
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
func mergeCredentialsFromClusterSecrets(ctx context.Context, info *Info) {
	clientset, err := getKubernetesClient()
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

// getKubernetesClient creates a Kubernetes clientset from the default kubeconfig.
func getKubernetesClient() (*kubernetes.Clientset, error) {
	restConfig, err := k8s.GetRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("get REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return clientset, nil
}

// tryFluxSecret attempts to retrieve credentials from the Flux registry secret.
// Returns true if credentials were found and set.
func tryFluxSecret(ctx context.Context, clientset *kubernetes.Clientset, info *Info) bool {
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
	if username != "" {
		info.Username = username
		info.Password = password

		return true
	}

	return false
}

// tryArgoCDSecret attempts to retrieve credentials from the ArgoCD repository secret.
// Returns true if credentials were found and set.
func tryArgoCDSecret(
	ctx context.Context,
	clientset *kubernetes.Clientset,
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

	if username != "" {
		info.Username = username
		info.Password = password

		return true
	}

	return false
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
