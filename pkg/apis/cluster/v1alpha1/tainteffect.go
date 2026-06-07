package v1alpha1

// TaintEffect defines the scheduling effect of a node pool taint. It mirrors the
// Kubernetes core taint effects and is used for per-pool taints on autoscaler
// node pools (spec.cluster.autoscaler.node.pools[].taints[].effect).
type TaintEffect string

const (
	// TaintEffectNoSchedule prevents pods that do not tolerate the taint from
	// being scheduled onto the node (already-running pods are unaffected).
	TaintEffectNoSchedule TaintEffect = "NoSchedule"
	// TaintEffectPreferNoSchedule asks the scheduler to avoid placing
	// non-tolerating pods on the node, but does not guarantee it.
	TaintEffectPreferNoSchedule TaintEffect = "PreferNoSchedule"
	// TaintEffectNoExecute evicts already-running pods that do not tolerate the
	// taint, in addition to preventing new ones from scheduling.
	TaintEffectNoExecute TaintEffect = "NoExecute"
)

// ValidValues returns all valid TaintEffect values as strings. It satisfies the
// [EnumValuer] interface so the schema generator emits an enum constraint.
func (e *TaintEffect) ValidValues() []string {
	return []string{
		string(TaintEffectNoSchedule),
		string(TaintEffectPreferNoSchedule),
		string(TaintEffectNoExecute),
	}
}

// String returns the string representation of the TaintEffect.
func (e *TaintEffect) String() string {
	return string(*e)
}

// ValidTaintEffects returns all valid taint effects.
func ValidTaintEffects() []TaintEffect {
	return []TaintEffect{
		TaintEffectNoSchedule,
		TaintEffectPreferNoSchedule,
		TaintEffectNoExecute,
	}
}
