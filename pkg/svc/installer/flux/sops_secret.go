package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v5/pkg/client/flux"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var errSOPSKeyNotFound = errors.New("SOPS is enabled but no Age key found")

// ensureSopsAgeSecret creates or updates the sops-age secret in flux-system namespace
// if SOPS is enabled and an Age key is available.
func ensureSopsAgeSecret(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	sops := clusterCfg.Spec.Cluster.SOPS
	explicitlyEnabled := sops.Enabled != nil && *sops.Enabled

	// If explicitly disabled, skip
	if sops.Enabled != nil && !explicitlyEnabled {
		return nil
	}

	ageKey, err := resolveAgeKey(sops)
	if err != nil {
		if explicitlyEnabled {
			return fmt.Errorf("resolve SOPS Age key: %w", err)
		}

		// Auto-detect mode: treat errors as "no key available"
		return nil
	}

	if ageKey == "" {
		if explicitlyEnabled {
			return fmt.Errorf(
				"%w (checked env var %q and local key file)",
				errSOPSKeyNotFound,
				sops.AgeKeyEnvVar,
			)
		}

		// Auto-detect mode: no key found, skip silently
		return nil
	}

	secret := buildSopsAgeSecret(ageKey)

	k8sClient, err := newCoreV1Client(restConfig)
	if err != nil {
		return err
	}

	return upsertSecret(ctx, k8sClient, secret)
}

// resolveAgeKey resolves the Age private key from available sources.
// Priority: (1) environment variable named by AgeKeyEnvVar, (2) local key file.
// Returns the extracted AGE-SECRET-KEY-... string, or empty if not found.
// Returns an error if the key file exists but cannot be read.
func resolveAgeKey(sops v1alpha1.SOPS) (string, error) {
	// Try environment variable first
	if sops.AgeKeyEnvVar != "" {
		if val := os.Getenv(sops.AgeKeyEnvVar); val != "" {
			if key := extractAgeKey(val); key != "" {
				return key, nil
			}
		}
	}

	// Try local key file
	keyPath, err := fsutil.SOPSAgeKeyPath()
	if err != nil {
		return "", fmt.Errorf("determine age key path: %w", err)
	}

	canonicalKeyPath, err := fsutil.EvalCanonicalPath(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("canonicalize age key path: %w", err)
	}

	// Canonicalization above resolves symlinks and normalizes env-derived paths
	// before reading, so gosec G304 is acceptable here.
	//nolint:gosec // G304: canonicalized path from controlled env/config inputs
	data, err := os.ReadFile(canonicalKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("read age key file: %w", err)
	}

	return extractAgeKey(string(data)), nil
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
