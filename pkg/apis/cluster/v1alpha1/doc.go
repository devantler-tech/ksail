// Package v1alpha1 provides model definitions for a KSail cluster.
//
// This package contains type definitions, constructors, and model structures
// for defining and working with KSail cluster configurations in the v1alpha1 API version.
// The Cluster type is both the CLI configuration model (ksail.yaml) and the
// controller-runtime custom resource reconciled by the KSail operator.
//
// All ksail config fields have defaults, so CRD validation treats fields as optional by default
// (a minimal Cluster needs only spec.cluster.distribution). Mark specific fields Required as needed.
//
// +kubebuilder:validation:Optional
// +kubebuilder:object:generate=true
// +groupName=ksail.io
package v1alpha1

// Regenerate DeepCopy methods (zz_generated.deepcopy.go) and the Cluster CRD manifest
// (config/crd/bases/ksail.io_clusters.yaml) after changing the API types. Mirrors the
// schema generation in schemas/ (go run gen_schema.go).
//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0 object:headerFile=../../../../hack/boilerplate.go.txt paths=.
//go:generate go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0 crd paths=. output:crd:dir=../../../../config/crd/bases
