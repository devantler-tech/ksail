// Package eksctl wraps the `eksctl` CLI binary as a KSail client.
//
// eksctl's programmatic Go library exposes Upgrade/Delete/nodegroup operations
// via `github.com/weaveworks/eksctl/pkg/actions/...` but does not expose a
// programmatic Create API — cluster creation is tightly coupled to Cobra and
// cmdutils inside `pkg/ctl/create/cluster.go`. KSail therefore adopts a hybrid
// integration:
//
//   - Shell out to the `eksctl` CLI binary for all write operations in this
//     client package.
//   - Embed the Go library directly in the EKS provisioner for operations that
//     the library exposes cleanly (Upgrade, Delete, nodegroup scale).
//
// This package provides the binary side of the hybrid. The upstream CLI is
// released from https://github.com/eksctl-io/eksctl (module path is still
// github.com/weaveworks/eksctl for historical reasons).
//
// The client is fully testable via an injectable Runner interface so unit
// tests do not need the eksctl binary on PATH.
package eksctl
