package v1alpha1

// AutoscalerExpander defines the node expander strategy for the cluster autoscaler.
type AutoscalerExpander string

const (
	// AutoscalerExpanderPrice selects the node group with the lowest cost.
	AutoscalerExpanderPrice AutoscalerExpander = "Price"
	// AutoscalerExpanderLeastWaste selects the node group that will have the least idle CPU/memory.
	AutoscalerExpanderLeastWaste AutoscalerExpander = "LeastWaste"
	// AutoscalerExpanderLeastNodes selects the node group that will result in the fewest total nodes.
	AutoscalerExpanderLeastNodes AutoscalerExpander = "LeastNodes"
	// AutoscalerExpanderRandom selects a node group at random.
	AutoscalerExpanderRandom AutoscalerExpander = "Random"
)

// ValidAutoscalerExpanders returns supported AutoscalerExpander values.
func ValidAutoscalerExpanders() []AutoscalerExpander {
	return []AutoscalerExpander{
		AutoscalerExpanderPrice,
		AutoscalerExpanderLeastWaste,
		AutoscalerExpanderLeastNodes,
		AutoscalerExpanderRandom,
	}
}

// Set for AutoscalerExpander (pflag.Value interface).
func (a *AutoscalerExpander) Set(value string) error {
	return setEnum(a, value, ValidAutoscalerExpanders(), ErrInvalidAutoscalerExpander)
}

// String returns the string representation of the AutoscalerExpander.
func (a *AutoscalerExpander) String() string {
	return string(*a)
}

// Type returns the type of the AutoscalerExpander.
func (a *AutoscalerExpander) Type() string {
	return "AutoscalerExpander"
}

// Default returns the default value for AutoscalerExpander (LeastWaste).
func (a *AutoscalerExpander) Default() any {
	return AutoscalerExpanderLeastWaste
}

// ValidValues returns all valid AutoscalerExpander values as strings.
func (a *AutoscalerExpander) ValidValues() []string {
	return validValueStrings(ValidAutoscalerExpanders())
}
