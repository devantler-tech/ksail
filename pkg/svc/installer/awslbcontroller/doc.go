// Package awslbcontrollerinstaller provides installation of the AWS Load
// Balancer Controller on EKS clusters.
//
// The controller provisions AWS Application/Network Load Balancers for
// Kubernetes Ingress and Service resources, replacing the legacy in-tree
// Classic Load Balancer path that EKS otherwise uses by default.
//
// Prerequisites the installer does NOT create (documented, not automated —
// these are account-level IAM/VPC concerns outside a chart install):
//   - IAM permissions for the controller: either an IRSA-backed service
//     account (eksctl `iamserviceaccount` with the controller's IAM policy)
//     or the node instance role carrying that policy. With the chart's
//     default values the controller runs under a chart-created service
//     account with no role annotation and relies on the node role.
//   - Subnet tags for load balancer discovery (kubernetes.io/role/elb and
//     kubernetes.io/role/internal-elb); eksctl-created VPCs tag these
//     automatically.
//
// Installation is an experimental opt-in gated by
// spec.cluster.eks.experimentalAWSLoadBalancerController together with
// spec.cluster.loadBalancer: Enabled.
package awslbcontrollerinstaller
