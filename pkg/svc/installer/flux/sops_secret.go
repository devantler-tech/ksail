package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v5/pkg/client/flux"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

const (
	// SopsAgeSecretName is the name of the Kubernetes secret used for SOPS Age decryption.
	// Flux Kustomization CRDs reference this via spec.decryption.secretRef.name.
	SopsAgeSecretName = "sops-age"
	// sopsAgeKeyField is the data key within the secret that holds the Age private key.
	sopsAgeKeyField = "sops.agekey"
	// ageSecretKeyPrefix is the prefix for Age private keys.
	//nolint:gosec // not credentials, just a key format prefix constant
	ageSecretKeyPrefix = "AGE-SECRET-KEY-"
)

var (
	errSOPSKeyNotFound = errors.New("SOPS is enabled but no Age key found")
	errAppDataNotSet   = errors.New("AppData environment variable not set")
)

// ensureSopsAgeSecret creates or updates the sops-age secret in flux-system namespace
// if SOPS is enabled and an Age key is available.
func ensureSopsAgeSecret(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	sops := clusterCfg.Spec.Cluster.SOPS

	// If explicitly disabled, skip
	if sops.Enabled != nil && !*sops.Enabled {
		return nil
	}

	ageKey := resolveAgeKey(sops)

	// If explicitly enabled but no key found, error
	if sops.Enabled != nil && *sops.Enabled && ageKey == "" {
		return fmt.Errorf(
			"%w (checked env var %q and local key file)",
			errSOPSKeyNotFound,
			sops.AgeKeyEnvVar,
		)
	}

	// Auto-detect mode: no key found, skip silently
	if ageKey == "" {
		return nil
	}

	secret := buildSopsAgeSecret(ageKey)

	scheme := k8sruntime.NewScheme()

	err := corev1.AddToScheme(scheme)
	if err != nil {
		return fmt.Errorf("failed to add core scheme: %w", err)
	}

	k8sClient, err := newDynamicClient(restConfig, scheme)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return upsertSecret(ctx, k8sClient, secret)
}

// resolveAgeKey resolves the Age private key from available sources.
// Priority: (1) environment variable named by AgeKeyEnvVar, (2) local key file.
// Returns the extracted AGE-SECRET-KEY-... string, or empty if not found.
func resolveAgeKey(sops v1alpha1.SOPS) string {
	// Try environment variable first
	if sops.AgeKeyEnvVar != "" {
		if val := os.Getenv(sops.AgeKeyEnvVar); val != "" {
			if key := extractAgeKey(val); key != "" {
				return key
			}
		}
	}

	// Try local key file
	keyPath, err := sopsAgeKeyPath()
	if err != nil {
		return ""
	}

	//#nosec G304 -- keyPath comes from platform-specific well-known paths
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return ""
	}

	return extractAgeKey(string(data))
}

// extractAgeKey finds and returns the first AGE-SECRET-KEY-... line from the input.
func extractAgeKey(input string) string {
	for line := range strings.SplitSeq(input, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ageSecretKeyPrefix) {
			return line
		}
	}

	return ""
}

// buildSopsAgeSecret creates the Secret object for SOPS Age decryption.
func buildSopsAgeSecret(ageKey string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SopsAgeSecretName,
			Namespace: fluxclient.DefaultNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "ksail",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			sopsAgeKeyField: []byte(ageKey),
		},
	}
}

// sopsAgeKeyPath returns the platform-specific path for the SOPS age keys file.
// Follows the same convention as cipher/import.go getAgeKeyPath():
//   - First checks SOPS_AGE_KEY_FILE environment variable
//   - Linux: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/.config/sops/age/keys.txt
//   - macOS: $XDG_CONFIG_HOME/sops/age/keys.txt or $HOME/Library/Application Support/sops/age/keys.txt
//   - Windows: %AppData%\sops\age\keys.txt
func sopsAgeKeyPath() (string, error) {
	if sopsAgeKeyFile := os.Getenv("SOPS_AGE_KEY_FILE"); sopsAgeKeyFile != "" {
		return sopsAgeKeyFile, nil
	}

	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "sops", "age", "keys.txt"), nil
	}

	switch goruntime.GOOS {
	case "windows":
		appData := os.Getenv("AppData")
		if appData == "" {
			return "", errAppDataNotSet
		}

		return filepath.Join(appData, "sops", "age", "keys.txt"), nil

	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		return filepath.Join(
			homeDir, "Library", "Application Support", "sops", "age", "keys.txt",
		), nil

	default:
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		return filepath.Join(homeDir, ".config", "sops", "age", "keys.txt"), nil
	}
}
