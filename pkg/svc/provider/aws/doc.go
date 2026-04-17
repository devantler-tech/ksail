// Package aws implements the KSail Provider interface for Amazon EKS clusters.
//
// The AWS provider is a thin layer over the eksctl CLI wrapper in
// pkg/client/eksctl. All infrastructure operations (list clusters, list
// nodegroups, scale nodegroups) are delegated to eksctl, which in turn
// uses CloudFormation and the EKS/EC2 AWS APIs.
//
// Lifecycle semantics differ from the Docker and Hetzner providers:
//   - EKS control planes cannot be "stopped". StartNodes/StopNodes therefore
//     scale managed nodegroups to and from zero desired capacity, respectively.
//   - DeleteNodes is a no-op because `eksctl delete cluster` already deletes
//     all owned nodegroups via CloudFormation.
//
// Credentials are resolved by eksctl itself through the standard AWS SDK v2
// credential chain (env vars, ~/.aws/credentials, IMDS, SSO, etc.). KSail does
// not interact with AWS credentials directly.
package aws
