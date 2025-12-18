// Package oci provides OCI artifact management for Kubernetes workloads.
//
// This package handles building, packaging, and pushing Kubernetes manifests
// as OCI artifacts to container registries. It supports collecting YAML/JSON
// manifests from a directory, bundling them into an OCI-compliant layer, and
// pushing the resulting artifact to a registry endpoint.
//
// Key functionality:
//   - Manifest collection from directories (.yaml, .yml, .json files)
//   - OCI artifact packaging using go-containerregistry
//   - Registry push operations with validation
//   - Build options validation and normalization
//
// Example usage:
//
//	// Create a workload artifact builder
//	b := oci.NewWorkloadArtifactBuilder()
//
//	// Build and push an artifact
//	result, err := b.Build(ctx, oci.BuildOptions{
//	    Name:             "my-workload",
//	    SourcePath:       "./k8s/manifests",
//	    RegistryEndpoint: "localhost:5000",
//	    Repository:       "ksail-workloads/app",
//	    Version:          "1.0.0",
//	})
//	if err != nil {
//	    return err
//	}
//
//	// Access the resulting artifact metadata
//	fmt.Printf("Pushed artifact: %s/%s:%s\n",
//	    result.Artifact.RegistryEndpoint,
//	    result.Artifact.Repository,
//	    result.Artifact.Tag)
package oci
