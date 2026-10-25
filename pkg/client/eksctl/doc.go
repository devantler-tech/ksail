// Package eksctl wraps the `eksctl` CLI binary as a KSail client.
//
// KSail runs the eksctl binary for the EKS operations it does not perform
// natively: creating and deleting clusters, creating, listing and scaling node
// groups, and listing and describing clusters. Control-plane upgrades go
// through the AWS SDK in pkg/client/eks instead (see
// docs/adr/0001-explicit-eks-control-plane-upgrades.md).
//
// The binary is used rather than eksctl's Go module because that module does
// not expose a programmatic create API: cluster creation is coupled to Cobra
// and cmdutils inside `pkg/ctl/create/cluster.go`. The upstream CLI is released
// from https://github.com/eksctl-io/eksctl (module path is still
// github.com/weaveworks/eksctl for historical reasons).
//
// The client is fully testable via an injectable Runner interface so unit
// tests do not need the eksctl binary on PATH.
package eksctl
