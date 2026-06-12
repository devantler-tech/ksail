package v1alpha1

// PodAutoscalerVertical defines whether Vertical Pod Autoscaling (VPA) is enabled.
type PodAutoscalerVertical string

const (
	// PodAutoscalerVerticalEnabled enables Vertical Pod Autoscaling.
	PodAutoscalerVerticalEnabled PodAutoscalerVertical = "Enabled"
	// PodAutoscalerVerticalDisabled disables Vertical Pod Autoscaling.
	PodAutoscalerVerticalDisabled PodAutoscalerVertical = "Disabled"
)

// ValidPodAutoscalerVerticals returns supported PodAutoscalerVertical values.
func ValidPodAutoscalerVerticals() []PodAutoscalerVertical {
	return []PodAutoscalerVertical{PodAutoscalerVerticalEnabled, PodAutoscalerVerticalDisabled}
}

// Set for PodAutoscalerVertical (pflag.Value interface).
func (p *PodAutoscalerVertical) Set(value string) error {
	return setEnum(p, value, ValidPodAutoscalerVerticals(), ErrInvalidPodAutoscalerVertical)
}

// String returns the string representation of the PodAutoscalerVertical.
func (p *PodAutoscalerVertical) String() string {
	return string(*p)
}

// Type returns the type of the PodAutoscalerVertical.
func (p *PodAutoscalerVertical) Type() string {
	return "PodAutoscalerVertical"
}

// Default returns the default value for PodAutoscalerVertical (Disabled).
func (p *PodAutoscalerVertical) Default() any {
	return PodAutoscalerVerticalDisabled
}

// ValidValues returns all valid PodAutoscalerVertical values as strings.
func (p *PodAutoscalerVertical) ValidValues() []string {
	return validValueStrings(ValidPodAutoscalerVerticals())
}
