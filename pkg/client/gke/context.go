package gke

import "strings"

// clusterNameContextSegments is the number of "_"-separated segments after the
// "gke_" prefix in a gcloud-produced kubeconfig context: project, location, name.
const clusterNameContextSegments = 3

// ClusterNameFromContext extracts the cluster name from a GKE kubeconfig
// context. gcloud-produced contexts look like "gke_<project>_<location>_<name>".
// Returns an empty string when the context is not recognisably a GKE context so
// callers can apply their own fallback. This is the single parser for the
// gcloud context contract — every call site must delegate here rather than
// re-implementing the format.
func ClusterNameFromContext(kubeContext string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(kubeContext), "gke_")
	if !ok {
		return ""
	}

	parts := strings.Split(rest, "_")
	if len(parts) < clusterNameContextSegments {
		return ""
	}

	return strings.TrimSpace(parts[len(parts)-1])
}
