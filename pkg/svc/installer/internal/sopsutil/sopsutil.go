// Package sopsutil provides shared helpers for SOPS Age key resolution and secret building
// used by both the ArgoCD and Flux installers.
package sopsutil

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
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

// ErrNoMatchingAgeKey indicates public keys were configured but no matching private key was found.
var ErrNoMatchingAgeKey = errors.New(
	"no private key matches the configured public keys",
)

// resolveEnvVarName returns the environment variable name to use for the Age key.
// Priority: sops.Env.Var (if set) > sops.AgeKeyEnvVar (backward compat).
func resolveEnvVarName(sops v1alpha1.SOPS) string {
	if sops.Env.Var != "" {
		return sops.Env.Var
	}

	//nolint:staticcheck // SA1019: AgeKeyEnvVar is deprecated but we must read it for backward compat
	return sops.AgeKeyEnvVar
}

// resolveKeyFilePath returns the key file path to use for Age key extraction.
// Priority: sops.Extract.File (if set) > OS-specific default.
// Expands a leading "~/" to the user's home directory.
func resolveKeyFilePath(sops v1alpha1.SOPS) (string, error) {
	if sops.Extract.File != "" {
		expanded, err := fsutil.ExpandHomePath(sops.Extract.File)
		if err != nil {
			return "", fmt.Errorf("expand extract file path: %w", err)
		}

		return expanded, nil
	}

	p, err := fsutil.SOPSAgeKeyPath()
	if err != nil {
		return "", fmt.Errorf("determine age key path: %w", err)
	}

	return p, nil
}

// ResolveEnabledAgeKey checks the SOPS configuration and resolves the
// Age private key(s). It respects explicit enable/disable and falls back
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
			envVar := resolveEnvVarName(sops)

			if envVar == "" {
				return "", fmt.Errorf(
					"%w (env var lookup disabled; checked local key file only)",
					ErrSOPSKeyNotFound,
				)
			}

			return "", fmt.Errorf(
				"%w (checked env var %q and local key file)",
				ErrSOPSKeyNotFound,
				envVar,
			)
		}

		return "", nil
	}

	return ageKey, nil
}

// ResolveAgeKey resolves the Age private key(s) from available sources.
// Priority: (1) environment variable, (2) local key file.
// When extracting from a key file, all keys are returned and optionally
// filtered by SOPS.Extract.PublicKeys.
// Returns the key(s) as a newline-joined string, or empty if not found.
func ResolveAgeKey(sops v1alpha1.SOPS) (string, error) {
	if key := resolveFromEnvVar(sops); key != "" {
		return key, nil
	}

	return resolveFromKeyFile(sops)
}

// resolveFromEnvVar attempts to extract an age key from the configured env var.
func resolveFromEnvVar(sops v1alpha1.SOPS) string {
	envVar := resolveEnvVarName(sops)
	if envVar == "" {
		return ""
	}

	val := os.Getenv(envVar)
	if val == "" {
		return ""
	}

	return ExtractAgeKey(val)
}

// resolveFromKeyFile reads all age keys from the configured key file and
// optionally filters them by the configured public keys.
func resolveFromKeyFile(sops v1alpha1.SOPS) (string, error) {
	data, err := readKeyFile(sops)
	if err != nil {
		return "", err
	}

	if data == "" {
		return "", nil
	}

	allKeys := ExtractAllAgeKeys(data)
	if len(allKeys) == 0 {
		return "", nil
	}

	if len(sops.Extract.PublicKeys) > 0 {
		return filterAndJoin(allKeys, sops.Extract.PublicKeys)
	}

	return strings.Join(allKeys, "\n"), nil
}

// readKeyFile reads the contents of the configured age key file.
// Returns ("", nil) if the file does not exist.
func readKeyFile(sops v1alpha1.SOPS) (string, error) {
	keyPath, err := resolveKeyFilePath(sops)
	if err != nil {
		return "", err
	}

	canonicalKeyPath, err := fsutil.EvalCanonicalPath(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("canonicalize age key path: %w", err)
	}

	//nolint:gosec // G304: canonicalized path from controlled inputs
	data, err := os.ReadFile(canonicalKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("read age key file: %w", err)
	}

	return string(data), nil
}

// filterAndJoin filters keys by public keys and joins the result.
func filterAndJoin(allKeys, publicKeys []string) (string, error) {
	filtered, err := FilterKeysByPublicKeys(allKeys, publicKeys)
	if err != nil {
		return "", fmt.Errorf("filter age keys by public keys: %w", err)
	}

	if len(filtered) == 0 {
		return "", fmt.Errorf("%w (checked %d private keys against %d public keys)",
			ErrNoMatchingAgeKey, len(allKeys), len(publicKeys))
	}

	return strings.Join(filtered, "\n"), nil
}

// ExtractAgeKey finds and returns the first AGE-SECRET-KEY-... line
// from the input. Used for single-key extraction (e.g. from env var).
func ExtractAgeKey(input string) string {
	for line := range strings.SplitSeq(input, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, AgeSecretKeyPrefix) {
			return line
		}
	}

	return ""
}

// ExtractAllAgeKeys extracts all AGE-SECRET-KEY-... lines from the input.
func ExtractAllAgeKeys(input string) []string {
	var keys []string

	for line := range strings.SplitSeq(input, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, AgeSecretKeyPrefix) {
			keys = append(keys, line)
		}
	}

	return keys
}

// FilterKeysByPublicKeys filters private keys to only those whose derived
// public key matches one of the given public keys. Uses age.ParseX25519Identity
// to derive the public key from each private key. Empty entries in publicKeys
// are silently skipped.
func FilterKeysByPublicKeys(privateKeys, publicKeys []string) ([]string, error) {
	if len(privateKeys) == 0 {
		return nil, nil
	}

	if len(publicKeys) == 0 {
		return privateKeys, nil
	}

	pubKeySet := make(map[string]struct{}, len(publicKeys))

	for _, pk := range publicKeys {
		trimmed := strings.TrimSpace(pk)
		if trimmed != "" {
			pubKeySet[trimmed] = struct{}{}
		}
	}

	if len(pubKeySet) == 0 {
		return privateKeys, nil
	}

	var matched []string

	for i, privKey := range privateKeys {
		trimmed := strings.TrimSpace(privKey)

		identity, err := age.ParseX25519Identity(trimmed)
		if err != nil {
			return nil, fmt.Errorf("parse private key at index %d: %w", i, err)
		}

		derivedPubKey := identity.Recipient().String()
		if _, ok := pubKeySet[derivedPubKey]; ok {
			matched = append(matched, trimmed)
		}
	}

	return matched, nil
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
