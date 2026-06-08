package v1alpha1

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- Flux Types ---

// FluxObjectMeta provides the minimal metadata required for Flux custom resources.
type FluxObjectMeta struct {
	Name      string `json:"name,omitzero"`
	Namespace string `json:"namespace,omitzero"`
}

// FluxOCIRepository models the Flux OCIRepository custom resource fields relevant to KSail-Go.
type FluxOCIRepository struct {
	Metadata FluxObjectMeta          `json:"metadata,omitzero"`
	Spec     FluxOCIRepositorySpec   `json:"spec,omitzero"`
	Status   FluxOCIRepositoryStatus `json:"status,omitzero"`
}

// FluxOCIRepositorySpec encodes connection details to an OCI registry repository.
type FluxOCIRepositorySpec struct {
	URL      string               `json:"url,omitzero"`
	Interval metav1.Duration      `json:"interval,omitzero"`
	Ref      FluxOCIRepositoryRef `json:"ref,omitzero"`
}

// FluxOCIRepositoryRef targets a specific OCI artifact tag.
type FluxOCIRepositoryRef struct {
	Tag string `json:"tag,omitzero"`
}

// FluxOCIRepositoryStatus exposes reconciliation conditions for OCIRepository resources.
type FluxOCIRepositoryStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitzero"`
}

// FluxKustomization models the Flux Kustomization custom resource fields relevant to KSail-Go.
type FluxKustomization struct {
	Metadata FluxObjectMeta          `json:"metadata,omitzero"`
	Spec     FluxKustomizationSpec   `json:"spec,omitzero"`
	Status   FluxKustomizationStatus `json:"status,omitzero"`
}

// FluxKustomizationSpec defines how Flux should apply manifests from a referenced source.
type FluxKustomizationSpec struct {
	Path            string                     `json:"path,omitzero"`
	Interval        metav1.Duration            `json:"interval,omitzero"`
	Prune           bool                       `json:"prune,omitzero"`
	TargetNamespace string                     `json:"targetNamespace,omitzero"`
	SourceRef       FluxKustomizationSourceRef `json:"sourceRef,omitzero"`
}

// FluxKustomizationSourceRef identifies the Flux source object backing a Kustomization.
type FluxKustomizationSourceRef struct {
	Name      string `json:"name,omitzero"`
	Namespace string `json:"namespace,omitzero"`
}

// FluxKustomizationStatus exposes reconciliation conditions for Kustomization resources.
type FluxKustomizationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitzero"`
}

// FluxVerifySpec configures signature verification for the flux-system
// OCIRepository that KSail generates and owns when gitOpsEngine is Flux. KSail
// renders it onto that OCIRepository's spec.verify (Flux's native
// OCIRepository.spec.verify) so Flux rejects any artifact whose signature fails
// verification at pull time. Because KSail owns and continuously reconciles the
// OCIRepository, a hand-written in-repo override would fight reconciliation — so
// verification is configured here instead. Has no effect for other GitOps
// engines (e.g. ArgoCD).
type FluxVerifySpec struct {
	Provider          string                   `json:"provider,omitzero"          jsonschema:"enum=cosign,enum=notation" jsonschema_description:"Signature verification technology for the generated Flux OCIRepository (cosign or notation). Set to enable verification; empty disables it. Applies only when gitOpsEngine is Flux."`         //nolint:lll
	SecretRef         FluxVerifySecretRef      `json:"secretRef,omitzero"                                                jsonschema_description:"Reference to a Kubernetes Secret in the flux-system namespace holding the trusted public keys or certificates. Used for key-based cosign or notation verification; omit for cosign keyless."` //nolint:lll
	MatchOIDCIdentity []FluxVerifyOIDCIdentity `json:"matchOIDCIdentity,omitzero"                                        jsonschema_description:"Identity matchers for cosign keyless verification. The artifact is accepted if any matcher matches the signing identity in the Fulcio certificate."`                                          //nolint:lll,tagliatelle // mirrors Flux OCIRepository.spec.verify.matchOIDCIdentity casing
}

// FluxVerifySecretRef references the Kubernetes Secret holding the trusted
// public keys or certificates used for signature verification.
type FluxVerifySecretRef struct {
	Name string `json:"name,omitzero" jsonschema_description:"Name of the Kubernetes Secret in the flux-system namespace containing the trusted public keys or certificates."` //nolint:lll
}

// FluxVerifyOIDCIdentity is a cosign keyless identity matcher. Both fields
// are Go regular expressions matched against the Fulcio signing certificate.
type FluxVerifyOIDCIdentity struct {
	Issuer  string `json:"issuer,omitzero"  jsonschema_description:"Go regular expression matched against the OIDC issuer in the Fulcio certificate (e.g. ^https://token\\.actions\\.githubusercontent\\.com$)."`                        //nolint:lll
	Subject string `json:"subject,omitzero" jsonschema_description:"Go regular expression matched against the identity subject in the Fulcio certificate (e.g. ^https://github\\.com/org/repo/\\.github/workflows/cd\\.yaml@refs/.*$)."` //nolint:lll
}

// Enabled reports whether signature verification is configured. Verification
// requires a provider; an empty provider leaves the generated OCIRepository
// without a spec.verify block.
func (v FluxVerifySpec) Enabled() bool {
	return strings.TrimSpace(v.Provider) != ""
}
