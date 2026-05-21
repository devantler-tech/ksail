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

// ValidValues returns all valid ClusterPhase values as strings. It implements EnumValuer so
// the JSON schema and CRD generators can discover the allowed values automatically.
func (p ClusterPhase) ValidValues() []string {
	return []string{
		string(ClusterPhasePending),
		string(ClusterPhaseProvisioning),
		string(ClusterPhaseReady),
		string(ClusterPhaseUpdating),
		string(ClusterPhaseDeleting),
		string(ClusterPhaseFailed),
	}
}
