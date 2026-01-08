package v1alpha1

import (
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
