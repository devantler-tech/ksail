package kindprovisioner_test

// Note: Tests for ConfigureContainerdRegistryMirrors, listKindNodes, and injectHostsToml
// require a running Docker daemon and Kind cluster, so they are tested in integration tests.
// See cmd/cluster/create_test.go for end-to-end testing of the mirror configuration.
//
// Unit tests for the internal logic are covered by:
// - registry.GenerateHostsToml tests in pkg/svc/provisioner/registry/
// - registry.BuildMirrorEntries tests in pkg/svc/provisioner/registry/
