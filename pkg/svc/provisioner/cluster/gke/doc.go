// Package gkeprovisioner manages Google Kubernetes Engine clusters through
// the native Go SDK (pkg/client/gke), mirroring the EKS provisioner's shape:
// cluster lifecycle (Create/Delete/List/Exists) delegates to the GKE client,
// while Start/Stop delegate node scaling to the GCP infrastructure provider
// (pkg/svc/provider/gcp). GKE control planes are Google-managed and cannot be
// stopped, so Stop scales every node pool to zero and Start restores the
// pools' configured initial sizes.
package gkeprovisioner
