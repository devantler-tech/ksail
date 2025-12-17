package kindprovisioner_test

// This file intentionally left minimal as NewDefaultDockerClient requires Docker to be available,
// which is not suitable for pure unit tests. The adapter is tested through:
// 1. Compile-time verification in provider_adapter_test.go
// 2. Unit tests in the provisioner tests (which use mocks)
