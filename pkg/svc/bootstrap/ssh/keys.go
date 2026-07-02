package sshbootstrap

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KeyPair is the per-cluster SSH identity: the public half is registered with
// the provider at server-creation time (e.g. hcloud SSH keys / cloud-init
// authorized_keys), the private half authenticates [Dial]. ed25519 keeps it
// small, modern, and universally supported by current cloud images.
type KeyPair struct {
	// Signer authenticates the client side of the connection; pass it to
	// [Options.Signer].
	Signer ssh.Signer
	// AuthorizedKey is the single-line authorized_keys / provider representation
	// of the public key (no trailing newline).
	AuthorizedKey string
	// PrivateKeyPEM is the OpenSSH PEM encoding of the private key, for
	// persisting the identity so later invocations (start/stop/delete flows) can
	// reconnect to the same cluster.
	PrivateKeyPEM []byte
}

// GenerateKeyPair mints a fresh ed25519 SSH keypair for a cluster's live
// bring-up. Generation is local and instant; nothing is registered anywhere.
func GenerateKeyPair() (KeyPair, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return KeyPair{}, fmt.Errorf("marshal private key: %w", err)
	}

	signer, err := ssh.NewSignerFromSigner(privateKey)
	if err != nil {
		return KeyPair{}, fmt.Errorf("build signer: %w", err)
	}

	return KeyPair{
		Signer:        signer,
		AuthorizedKey: authorizedKeyLine(signer),
		PrivateKeyPEM: pem.EncodeToMemory(pemBlock),
	}, nil
}

// ParsePrivateKey reconstructs a [KeyPair] from a previously persisted
// [KeyPair.PrivateKeyPEM], so a later invocation can reconnect to servers
// created with the same identity.
func ParsePrivateKey(pemBytes []byte) (KeyPair, error) {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return KeyPair{}, fmt.Errorf("parse private key: %w", err)
	}

	return KeyPair{
		Signer:        signer,
		AuthorizedKey: authorizedKeyLine(signer),
		PrivateKeyPEM: pemBytes,
	}, nil
}

// authorizedKeyLine renders the signer's public key as a single
// authorized_keys line without the trailing newline ssh.MarshalAuthorizedKey
// appends.
func authorizedKeyLine(signer ssh.Signer) string {
	return strings.TrimSuffix(
		string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
		"\n",
	)
}
