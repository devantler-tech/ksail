// Package awslbcontrollerinstaller provides installation of the AWS Load
// Balancer Controller on EKS clusters.
//
// The controller provisions AWS Application/Network Load Balancers for
// Kubernetes Ingress and Service resources, replacing the legacy in-tree
// Classic Load Balancer path that EKS otherwise uses by default.
//
// Prerequisites the installer does NOT create (documented, not automated —
// these are account-level IAM/VPC concerns outside a chart install):
//   - IAM permissions for the controller: the node instance role must carry
//     the controller's IAM policy. The chart installs its own service
//     account with no role annotation (node-role credentials); a
//     PRE-created IRSA service account is not yet supported — the chart
//     would try to create the ServiceAccount that eksctl already created
//     and fail (#6232 tracks IRSA support).
//   - Subnet tags for load balancer discovery (kubernetes.io/role/elb and
//     kubernetes.io/role/internal-elb); eksctl-created VPCs tag these
//     automatically.
//
// Installation is an experimental opt-in gated by
// spec.cluster.eks.experimentalAWSLoadBalancerController together with
// spec.cluster.loadBalancer: Enabled. It runs at cluster create and on the
// operator's reconcile; enabling the opt-in on an EXISTING cluster via
// `cluster update` is not yet detected by the diff engine (#6231).
package awslbcontrollerinstaller
