// Package eks wraps the AWS SDK operations the EKS connector needs:
// describing a cluster (endpoint, CA, status) and minting the standard EKS
// bearer token (a presigned STS GetCallerIdentity URL). It complements
// pkg/client/eksctl, which drives cluster lifecycle through the eksctl
// binary — this package is for the operator-side read/auth path, where
// shelling out is not an option.
package eks
