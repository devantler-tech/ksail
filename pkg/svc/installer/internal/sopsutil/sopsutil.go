// Package sopsutil provides shared helpers for SOPS Age key resolution and secret building
// used by both the ArgoCD and Flux installers.
package sopsutil

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// SopsAgeSecretName is the name of the Kubernetes secret used for SOPS Age decryption.
	SopsAgeSecretName = "sops-age"
	// sopsAgeKeyField is the data key within the secret that holds the Age private key.
	sopsAgeKeyField = "sops.agekey"
)

// AgeSecretKeyPrefix is the prefix for Age private keys.
//
//nolint:gosec // G101: not credentials, just a key format prefix
const AgeSecretKeyPrefix = "AGE-SECRET-KEY-"

// ErrSOPSKeyNotFound indicates SOPS is explicitly enabled but no key was found.
var ErrSOPSKeyNotFound = errors.New(
	"SOPS is enabled but no Age key found",
)

// ResolveEnabledAgeKey checks the SOPS configuration and resolves the
// Age private key. It respects explicit enable/disable and falls back
// to auto-detection. Returns ("", nil) when SOPS should be skipped.
func ResolveEnabledAgeKey(sops v1alpha1.SOPS) (string, error) {
	explicitlyEnabled := sops.Enabled != nil && *sops.Enabled

	if sops.Enabled != nil && !explicitlyEnabled {
		return "", nil
	}

	ageKey, err := ResolveAgeKey(sops)
	if err != nil {
		if explicitlyEnabled {
			return "", err
		}

		return "", nil
	}

	if ageKey == "" {
		if explicitlyEnabled {
			return "", fmt.Errorf(
				"%w (checked env var %q and local key file)",
				ErrSOPSKeyNotFound,
				sops.AgeKeyEnvVar,
			)
		}

		return "", nil
	}

	return ageKey, nil
}

// ResolveAgeKey resolves the Age private key from available sources.
// Priority: (1) environment variable named by AgeKeyEnvVar, (2) local key file.
// Returns the extracted AGE-SECRET-KEY-... string, or empty if not found.
// Returns an error if the key file exists but cannot be read.
func ResolveAgeKey(sops v1alpha1.SOPS) (string, error) {
	// Try environment variable first
	if sops.AgeKeyEnvVar != "" {
		if val := os.Getenv(sops.AgeKeyEnvVar); val != "" {
			if key := ExtractAgeKey(val); key != "" {
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

	// Canonicalization above resolves symlinks and normalizes
	// env-derived paths before reading, so gosec G304 is acceptable.
	//nolint:gosec // G304: canonicalized path from controlled inputs
	data, err := os.ReadFile(canonicalKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("read age key file: %w", err)
	}

	return ExtractAgeKey(string(data)), nil
}

// ExtractAgeKey finds and returns the first AGE-SECRET-KEY-... line
// from the input.
func ExtractAgeKey(input string) string {
	for line := range strings.SplitSeq(input, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, AgeSecretKeyPrefix) {
			return line
		}
	}

	return ""
}

// BuildSopsAgeSecret constructs the Kubernetes Secret for SOPS Age decryption
// in the given namespace. This shared helper is used by both the Flux and ArgoCD installers.
func BuildSopsAgeSecret(namespace, ageKey string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SopsAgeSecretName,
			Namespace: namespace,
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
