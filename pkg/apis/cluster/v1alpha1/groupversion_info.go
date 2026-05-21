package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemeGroupVersion is the group/version used to register the Cluster custom resource
// with a controller-runtime scheme. It reuses the package's Group and Version constants.
//
//nolint:gochecknoglobals // scheme registration globals follow the Kubernetes API convention
var SchemeGroupVersion = schema.GroupVersion{Group: Group, Version: Version}

//nolint:gochecknoglobals // scheme registration globals follow the Kubernetes API convention
var (
	// SchemeBuilder collects the functions that register the v1alpha1 types with a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the v1alpha1 types to a runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// addKnownTypes registers the Cluster types with the given scheme under SchemeGroupVersion.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion, &Cluster{}, &ClusterList{})
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)

	return nil
}
