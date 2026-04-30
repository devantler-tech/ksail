package clusterautoscalerinstaller

// chartVersion returns the pinned Cluster Autoscaler chart version.
// This must be updated manually when upgrading the chart.
func chartVersion() string {
	return "9.46.6"
}
