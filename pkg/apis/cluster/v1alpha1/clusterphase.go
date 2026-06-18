package v1alpha1

// ClusterPhase is a high-level summary of where a Cluster is in its lifecycle, as observed
// by the KSail operator. It is reported in ClusterStatus.Phase.
type ClusterPhase string

const (
	// ClusterPhasePending indicates the Cluster has been accepted but reconciliation has
	// not yet started provisioning it.
	ClusterPhasePending ClusterPhase = "Pending"
	// ClusterPhaseProvisioning indicates the operator is creating the cluster and installing
	// its components.
	ClusterPhaseProvisioning ClusterPhase = "Provisioning"
	// ClusterPhaseReady indicates the cluster exists and is reconciled to the desired state.
	ClusterPhaseReady ClusterPhase = "Ready"
	// ClusterPhaseStopped indicates the cluster's infrastructure exists but is not running (e.g. a
	// Docker-based cluster whose containers are stopped). It is a deliberate, recoverable state — the
	// cluster can be started again without recreating it — distinct from Ready (running) and Failed
	// (an error). Backends that cannot tell a cluster apart from running (cloud providers today) never
	// report it. For backward compatibility the same state is also surfaced as a
	// Ready=False/reason=Stopped status condition, so consumers predating this enum value keep working.
	ClusterPhaseStopped ClusterPhase = "Stopped"
	// ClusterPhaseUpdating indicates the operator is applying configuration changes to a
	// running cluster.
	ClusterPhaseUpdating ClusterPhase = "Updating"
	// ClusterPhaseDeleting indicates the cluster is being torn down.
	ClusterPhaseDeleting ClusterPhase = "Deleting"
	// ClusterPhaseFailed indicates reconciliation could not complete; see Conditions for details.
	ClusterPhaseFailed ClusterPhase = "Failed"
)

// String returns the string representation of the ClusterPhase.
func (p ClusterPhase) String() string {
	return string(p)
}

// ValidValues returns all valid ClusterPhase values as strings. It implements EnumValuer so the
// JSON schema generator can discover the allowed values automatically. CRD enum validation is
// enforced separately by the +kubebuilder:validation:Enum marker on ClusterStatus.Phase, because
// controller-gen does not consult this interface.
func (p ClusterPhase) ValidValues() []string {
	return []string{
		string(ClusterPhasePending),
		string(ClusterPhaseProvisioning),
		string(ClusterPhaseReady),
		string(ClusterPhaseStopped),
		string(ClusterPhaseUpdating),
		string(ClusterPhaseDeleting),
		string(ClusterPhaseFailed),
	}
}
