package v1alpha1

import (
	"fmt"
	"strings"
)

// PodAutoscalerVertical defines whether Vertical Pod Autoscaling (VPA) is enabled.
type PodAutoscalerVertical string

const (
	// PodAutoscalerVerticalEnabled enables Vertical Pod Autoscaling.
	PodAutoscalerVerticalEnabled PodAutoscalerVertical = "Enabled"
	// PodAutoscalerVerticalDisabled disables Vertical Pod Autoscaling.
	PodAutoscalerVerticalDisabled PodAutoscalerVertical = "Disabled"
)

// Set for PodAutoscalerVertical (pflag.Value interface).
func (p *PodAutoscalerVertical) Set(value string) error {
	for _, v := range ValidPodAutoscalerVerticals() {
		if strings.EqualFold(value, string(v)) {
			*p = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidPodAutoscalerVertical,
		value,
		PodAutoscalerVerticalEnabled,
		PodAutoscalerVerticalDisabled,
	)
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
	return []string{
		string(PodAutoscalerVerticalEnabled),
		string(PodAutoscalerVerticalDisabled),
	}
}
