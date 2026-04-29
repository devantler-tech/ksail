package v1alpha1

import (
	"fmt"
	"strings"
)

// PodAutoscalerHorizontal defines whether Horizontal Pod Autoscaling (HPA) is enabled.
type PodAutoscalerHorizontal string

const (
	// PodAutoscalerHorizontalEnabled enables Horizontal Pod Autoscaling.
	PodAutoscalerHorizontalEnabled PodAutoscalerHorizontal = "Enabled"
	// PodAutoscalerHorizontalDisabled disables Horizontal Pod Autoscaling.
	PodAutoscalerHorizontalDisabled PodAutoscalerHorizontal = "Disabled"
)

// Set for PodAutoscalerHorizontal (pflag.Value interface).
func (p *PodAutoscalerHorizontal) Set(value string) error {
	for _, v := range ValidPodAutoscalerHorizontals() {
		if strings.EqualFold(value, string(v)) {
			*p = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidPodAutoscalerHorizontal,
		value,
		PodAutoscalerHorizontalEnabled,
		PodAutoscalerHorizontalDisabled,
	)
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
	return []string{
		string(PodAutoscalerHorizontalEnabled),
		string(PodAutoscalerHorizontalDisabled),
	}
}
