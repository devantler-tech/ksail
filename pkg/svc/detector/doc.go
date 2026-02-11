// Package detector detects installed Kubernetes components by querying the
// cluster via Helm release history and Kubernetes API. This is used by the
// update command to build an accurate baseline of the running cluster state,
// replacing client-side state persistence.
package detector
