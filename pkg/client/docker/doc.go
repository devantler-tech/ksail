// Package docker provides client wrappers for Docker Engine API operations.
//
// This package includes:
//   - Docker client creation via environment configuration
//   - Registry container lifecycle management for mirror/pull-through caching
//   - Network and volume management utilities
//
// The RegistryManager handles creating, deleting, and managing Docker registry
// containers used for pull-through caching to upstream registries like Docker Hub.
// It supports sharing registry volumes across different Kubernetes distributions
// (Kind, K3d) to optimize storage and download times.
//
// Example usage:
//
//	// Create a registry manager
//	dockerClient, err := docker.GetDockerClient()
//	if err != nil {
//	    return err
//	}
//	regMgr, err := docker.NewRegistryManager(dockerClient)
//	if err != nil {
//	    return err
//	}
//
//	// Create a pull-through cache registry
//	err = regMgr.CreateRegistry(ctx, docker.RegistryConfig{
//	    Name:        "docker-io-mirror",
//	    Port:        5001,
//	    UpstreamURL: "https://registry-1.docker.io",
//	    NetworkName: "kind",
//	    VolumeName:  "docker-io-cache",
//	})
package docker
