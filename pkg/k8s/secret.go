package k8s

import (
	"bytes"
	"maps"

	corev1 "k8s.io/api/core/v1"
)

// MergeSecretData merges desiredData into secret.Data in place, setting
// StringData to nil to avoid conflicts. It returns false (no update needed)
// when every key in desiredData already matches the existing value.
func MergeSecretData(secret *corev1.Secret, desiredData map[string][]byte) bool {
	if SecretDataContains(secret.Data, desiredData) {
		return false
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte, len(desiredData))
	}

	maps.Copy(secret.Data, desiredData)
	secret.StringData = nil

	return true
}

// SecretDataContains returns true when every key in desiredData exists in
// existing with an equal value.
func SecretDataContains(existing, desiredData map[string][]byte) bool {
	for key, desiredValue := range desiredData {
		existingValue, ok := existing[key]
		if !ok {
			return false
		}

		if !bytes.Equal(existingValue, desiredValue) {
			return false
		}
	}

	return true
}
