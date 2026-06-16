package v1alpha1

// PodAutoscalerHorizontal defines whether Horizontal Pod Autoscaling (HPA) is enabled.
type PodAutoscalerHorizontal string

const (
	// PodAutoscalerHorizontalEnabled enables Horizontal Pod Autoscaling.
	PodAutoscalerHorizontalEnabled PodAutoscalerHorizontal = "Enabled"
	// PodAutoscalerHorizontalDisabled disables Horizontal Pod Autoscaling.
	PodAutoscalerHorizontalDisabled PodAutoscalerHorizontal = "Disabled"
)

// ValidPodAutoscalerHorizontals returns supported PodAutoscalerHorizontal values.
func ValidPodAutoscalerHorizontals() []PodAutoscalerHorizontal {
	return []PodAutoscalerHorizontal{
		PodAutoscalerHorizontalEnabled,
		PodAutoscalerHorizontalDisabled,
	}
}

// Set for PodAutoscalerHorizontal (pflag.Value interface).
func (p *PodAutoscalerHorizontal) Set(value string) error {
	return setEnum(p, value, ValidPodAutoscalerHorizontals(), ErrInvalidPodAutoscalerHorizontal)
}

// String returns the string representation of the PodAutoscalerHorizontal.
func (p *PodAutoscalerHorizontal) String() string {
	return string(*p)
}

// Type returns the type of the PodAutoscalerHorizontal.
func (p *PodAutoscalerHorizontal) Type() string {
	return "PodAutoscalerHorizontal"
}

// Default returns the default value for PodAutoscalerHorizontal (Disabled).
func (p *PodAutoscalerHorizontal) Default() any {
	return PodAutoscalerHorizontalDisabled
}

// ValidValues returns all valid PodAutoscalerHorizontal values as strings.
func (p *PodAutoscalerHorizontal) ValidValues() []string {
	return validValueStrings(ValidPodAutoscalerHorizontals())
}
