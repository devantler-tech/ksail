package kustomize

// HoldBuildGate takes the build gate as another build would and returns the
// function that frees it, so tests can make a caller wait for admission.
func HoldBuildGate() func() {
	buildGate <- struct{}{}

	return func() { <-buildGate }
}
