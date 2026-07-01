package kubeadmhetzner

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	// tokenAlphabet is the character set kubeadm bootstrap tokens draw from — the
	// lowercase-alphanumeric set kubeadm itself uses (its token regex is
	// `[a-z0-9]{6}\.[a-z0-9]{16}`).
	tokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	// tokenIDLength is the length of a bootstrap token's public ID part (before the
	// dot).
	tokenIDLength = 6
	// tokenSecretLength is the length of a bootstrap token's secret part (after the
	// dot).
	tokenSecretLength = 16
	// byteValueCount is the number of distinct values a single byte can hold.
	byteValueCount = 256
	// tokenRejectionCeiling is the largest multiple of len(tokenAlphabet) that fits
	// in a byte (36 * 7 = 252); a random byte at or above it is rejected so the
	// modulo mapping onto the alphabet stays uniform (no modulo bias).
	tokenRejectionCeiling = (byteValueCount / len(tokenAlphabet)) * len(tokenAlphabet)
)

// generateNodeToken returns a fresh kubeadm bootstrap token in the
// `[a-z0-9]{6}.[a-z0-9]{16}` form every node authenticates with — the public ID is
// advertised by the initialising control plane and the secret is redeemed by every
// joining node. The two parts are drawn from a cryptographically secure source with
// rejection sampling so each character is uniform over the alphabet.
func generateNodeToken() (string, error) {
	tokenID, err := randomTokenPart(tokenIDLength)
	if err != nil {
		return "", err
	}

	secret, err := randomTokenPart(tokenSecretLength)
	if err != nil {
		return "", err
	}

	return tokenID + "." + secret, nil
}

// randomTokenPart returns a string of length characters drawn uniformly from
// tokenAlphabet using crypto/rand with rejection sampling to avoid modulo bias.
func randomTokenPart(length int) (string, error) {
	var builder strings.Builder

	builder.Grow(length)

	buffer := make([]byte, 1)

	for builder.Len() < length {
		_, err := rand.Read(buffer)
		if err != nil {
			return "", fmt.Errorf("generate bootstrap token: %w", err)
		}

		if int(buffer[0]) >= tokenRejectionCeiling {
			continue
		}

		builder.WriteByte(tokenAlphabet[int(buffer[0])%len(tokenAlphabet)])
	}

	return builder.String(), nil
}
