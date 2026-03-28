package v1alpha1

// SOPS defines configuration for automatic SOPS Age secret creation in the cluster.
// When enabled (default: auto-detect), KSail creates a "sops-age" generic Secret in
// the flux-system namespace containing the Age private key, which Flux Kustomization
// CRDs reference via spec.decryption.secretRef.
type SOPS struct {
	// AgeKeyEnvVar is the name of the environment variable containing the Age private key.
	// Defaults to "SOPS_AGE_KEY". Set empty to disable environment variable lookup.
	AgeKeyEnvVar string `default:"SOPS_AGE_KEY" json:"ageKeyEnvVar,omitzero"`
	// Enabled controls whether the SOPS Age secret is created.
	// nil (default) = auto-detect (create if key is found via env var or key file).
	// true = require key (error if not found).
	// false = disable entirely (skip secret creation).
	Enabled *bool `json:"enabled,omitzero"`
}
