package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- OCI Registry Types ---

// OCIRegistryStatus represents lifecycle states for the local OCI registry instance.
type OCIRegistryStatus string

const (
	// OCIRegistryStatusNotProvisioned indicates the registry has not been created.
	OCIRegistryStatusNotProvisioned OCIRegistryStatus = "NotProvisioned"
	// OCIRegistryStatusProvisioning indicates the registry is currently being created or started.
	OCIRegistryStatusProvisioning OCIRegistryStatus = "Provisioning"
	// OCIRegistryStatusRunning indicates the registry is available for pushes/pulls.
	OCIRegistryStatusRunning OCIRegistryStatus = "Running"
	// OCIRegistryStatusError indicates the registry failed to start or crashed.
	OCIRegistryStatusError OCIRegistryStatus = "Error"
)

// OCIRegistry captures host-local OCI registry metadata and lifecycle status.
type OCIRegistry struct {
	Name       string            `json:"name,omitzero"`
	Endpoint   string            `json:"endpoint,omitzero"`
	Port       int32             `json:"port,omitzero"`
	DataPath   string            `json:"dataPath,omitzero"`
	VolumeName string            `json:"volumeName,omitzero"`
	Status     OCIRegistryStatus `json:"status,omitzero"`
	LastError  string            `json:"lastError,omitzero"`
}

// OCIArtifact describes a versioned OCI artifact that packages Kubernetes manifests.
type OCIArtifact struct {
	Name             string      `json:"name,omitzero"`
	Version          string      `json:"version,omitzero"`
	RegistryEndpoint string      `json:"registryEndpoint,omitzero"`
	Repository       string      `json:"repository,omitzero"`
	Tag              string      `json:"tag,omitzero"`
	SourcePath       string      `json:"sourcePath,omitzero"`
	CreatedAt        metav1.Time `json:"createdAt,omitzero"`
}
