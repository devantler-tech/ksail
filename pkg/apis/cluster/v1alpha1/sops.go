package v1alpha1

// SOPS defines configuration for automatic SOPS Age secret creation in the cluster.
// When enabled (default: auto-detect), KSail creates a "sops-age" generic Secret
// containing the Age private key(s):
//   - For Flux: in the flux-system namespace, referenced by Kustomization CRDs via spec.decryption.secretRef.
//   - For ArgoCD: in the argocd namespace, for use by Config Management Plugins or repo-server SOPS integration.
type SOPS struct {
	// Deprecated: Use Env.Var instead. AgeKeyEnvVar is the name of the environment
	// variable containing the Age private key. Kept for backward compatibility.
	// When Env.Var is set, it takes priority over AgeKeyEnvVar.
	AgeKeyEnvVar string `default:"SOPS_AGE_KEY" json:"ageKeyEnvVar,omitzero"`
	// Enabled controls whether the SOPS Age secret is created.
	// nil (default) = auto-detect (create if key is found via env var or key file).
	// true = require key (error if not found).
	// false = disable entirely (skip secret creation).
	Enabled *bool `json:"enabled,omitzero"`
	// Env configures the environment variable source for the Age private key.
	Env SOPSEnv `json:"env,omitzero"`
	// Extract configures extraction of Age private keys from a key file.
	Extract SOPSExtract `json:"extract,omitzero"`
}

// SOPSEnv configures the environment variable source for the Age private key.
type SOPSEnv struct {
	// Var is the name of the environment variable containing the Age private key.
	// When set, takes priority over SOPS.AgeKeyEnvVar.
	// Leave empty to fall back to AgeKeyEnvVar (default: "SOPS_AGE_KEY").
	Var string `json:"var,omitzero"`
}

// SOPSExtract configures extraction of Age private keys from a key file.
type SOPSExtract struct {
	// File is the path to the Age key file (absolute or relative to the working directory).
	// Defaults to the OS-specific SOPS age key path when empty.
	File string `json:"file,omitzero"`
	// PublicKeys is a list of Age public keys (age1...) used to select which
	// private keys to include in the SOPS secret. For each private key in the
	// key file, its public key is derived and compared against this list.
	// Only matching private keys are included.
	// When empty (default), all private keys from the file are included.
	PublicKeys []string `json:"publicKeys,omitzero"`
}
