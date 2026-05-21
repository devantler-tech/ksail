package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// SchemeGroupVersion is the group/version used to register the Cluster custom resource
// with a controller-runtime scheme. It reuses the package's Group and Version constants.
//
//nolint:gochecknoglobals // scheme registration globals follow the controller-runtime convention
var SchemeGroupVersion = schema.GroupVersion{Group: Group, Version: Version}

//nolint:gochecknoglobals // scheme registration globals follow the controller-runtime convention
var (
	// SchemeBuilder registers the v1alpha1 types with a runtime.Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}

	// AddToScheme adds the v1alpha1 types to a runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

//nolint:gochecknoinits // controller-runtime type registration is conventionally done in init
func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}

// Resource maps a resource name to a GroupResource in this API group. It is provided for
// parity with generated clientsets and downstream RBAC tooling.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// objectKindAssertions ensures the API types satisfy runtime.Object at compile time once
// DeepCopyObject is generated. These are zero-cost compile-time assertions.
//
//nolint:gochecknoglobals // compile-time interface assertions
var (
	_ runtime.Object = &Cluster{}
	_ runtime.Object = &ClusterList{}
)
