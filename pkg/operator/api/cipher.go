package api

import "context"

// SecretEncryptRequest is the body of an encrypt request.
type SecretEncryptRequest struct {
	// Plaintext is the YAML or JSON document to encrypt.
	Plaintext string `json:"plaintext"`
	// Recipient is the age recipient (age1…) to encrypt for. Empty uses the backend's default
	// (the first locally-available age key).
	Recipient string `json:"recipient,omitempty"`
	// Format selects the SOPS store: "yaml" (default) or "json".
	Format string `json:"format,omitempty"`
}

// SecretDecryptRequest is the body of a decrypt request.
type SecretDecryptRequest struct {
	// Encrypted is the SOPS-encrypted document.
	Encrypted string `json:"encrypted"`
	// Format selects the SOPS store: "yaml" (default) or "json".
	Format string `json:"format,omitempty"`
}

// CipherService is an optional interface a ClusterService may implement to encrypt/decrypt secrets
// with SOPS using the local age keys (cluster-independent). When the serving ClusterService
// implements it, the server registers the /api/v1/secrets/* routes and advertises
// capabilities.secretsCipher=true. The operator does not implement it (no local keys); only the
// local `ksail ui`/desktop backend does.
type CipherService interface {
	// EncryptSecret encrypts plaintext (YAML/JSON per format) for the given age recipient, or the
	// backend's default recipient when recipient is empty.
	EncryptSecret(ctx context.Context, plaintext, recipient, format string) (string, error)
	// DecryptSecret decrypts a SOPS document using the local age keys.
	DecryptSecret(ctx context.Context, encrypted, format string) (string, error)
	// CipherRecipients lists the age public keys (age1…) available locally, so the UI can offer them
	// as encryption targets. Empty when no local age key is configured.
	CipherRecipients(ctx context.Context) ([]string, error)
}
