package fluxinstaller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	fluxInstanceKind        = "FluxInstance"
	fluxOCIRepositoryKind   = "OCIRepository"
	fluxInstanceDefaultName = "flux"
	fluxInstanceGroup       = "fluxcd.controlplane.io"
	fluxInstanceVersion     = "v1"
)

//
//nolint:gochecknoglobals // package-level constant for API version
var fluxInstanceGroupVersion = schema.GroupVersion{
	Group:   fluxInstanceGroup,
	Version: fluxInstanceVersion,
}

// FluxInstance mirrors the Flux operator FluxInstance CRD with the minimal fields
// KSail-Go needs to configure default sync behavior. Keeping a local definition
// avoids pulling the entire operator module into go.mod.
type FluxInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   FluxInstanceSpec   `json:"spec"`
	Status FluxInstanceStatus `json:"status"`
}

// DeepCopyInto copies all properties of this object into another object of the same type.
func (in *FluxInstance) DeepCopyInto(out *FluxInstance) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy of FluxInstance.
func (in *FluxInstance) DeepCopy() *FluxInstance {
	if in == nil {
		return nil
	}

	out := new(FluxInstance)
	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject implements runtime.Object interface.
func (in *FluxInstance) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}

	return nil
}

// FluxInstanceList registers the list kind with the scheme for completeness.
type FluxInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FluxInstance `json:"items"`
}

// DeepCopyInto copies all properties into another FluxInstanceList.
func (in *FluxInstanceList) DeepCopyInto(out *FluxInstanceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta

	in.ListMeta.DeepCopyInto(&out.ListMeta)

	if in.Items != nil {
		out.Items = make([]FluxInstance, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a deep copy of FluxInstanceList.
func (in *FluxInstanceList) DeepCopy() *FluxInstanceList {
	if in == nil {
		return nil
	}

	out := new(FluxInstanceList)
	in.DeepCopyInto(out)

	return out
}

// DeepCopyObject implements runtime.Object interface.
func (in *FluxInstanceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}

	return nil
}

// FluxInstanceSpec contains the distribution configuration and sync source.
type FluxInstanceSpec struct {
	Distribution Distribution `json:"distribution"`
	Sync         *Sync        `json:"sync,omitempty"`
}

// DeepCopyInto copies all properties from this FluxInstanceSpec into another.
func (in *FluxInstanceSpec) DeepCopyInto(out *FluxInstanceSpec) {
	*out = *in
	if in.Sync != nil {
		out.Sync = new(Sync)
		in.Sync.DeepCopyInto(out.Sync)
	}
}

// Distribution references the Flux manifests and controller images KSail should install.
type Distribution struct {
	Version  string `json:"version"`
	Registry string `json:"registry"`
	Artifact string `json:"artifact,omitempty"`
}

// Sync configures the OCI source that the operator will track and apply.
type Sync struct {
	Name       string           `json:"name,omitempty"`
	Interval   *metav1.Duration `json:"interval,omitempty"`
	Kind       string           `json:"kind"`
	URL        string           `json:"url"`
	Ref        string           `json:"ref"`
	Path       string           `json:"path"`
	PullSecret string           `json:"pullSecret,omitempty"`
	Provider   string           `json:"provider,omitempty"`
}

// DeepCopyInto copies all properties into another Sync.
func (in *Sync) DeepCopyInto(out *Sync) {
	*out = *in
	if in.Interval != nil {
		intervalCopy := *in.Interval
		out.Interval = &intervalCopy
	}
}

// FluxInstanceStatus keeps parity with the real CRD so the scheme matches expectations.
type FluxInstanceStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DeepCopyInto copies all properties into another FluxInstanceStatus.
func (in *FluxInstanceStatus) DeepCopyInto(out *FluxInstanceStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopy creates a deep copy of this FluxInstanceStatus.
func (in *FluxInstanceStatus) DeepCopy() *FluxInstanceStatus {
	if in == nil {
		return nil
	}

	out := new(FluxInstanceStatus)
	in.DeepCopyInto(out)

	return out
}

// addFluxInstanceToScheme registers the custom resources with the provided scheme.
//
//nolint:unparam // error return kept for consistency with Kubernetes scheme registration patterns
func addFluxInstanceToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(
		fluxInstanceGroupVersion,
		&FluxInstance{},
		&FluxInstanceList{},
	)
	metav1.AddToGroupVersion(scheme, fluxInstanceGroupVersion)

	return nil
}
