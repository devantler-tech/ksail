// Package awslbcontrollerinstaller provides installation of the AWS Load
// Balancer Controller on EKS clusters.
//
// The controller provisions AWS Application/Network Load Balancers for
// Kubernetes Ingress and Service resources, replacing the legacy in-tree
// Classic Load Balancer path that EKS otherwise uses by default.
//
// Prerequisites the installer does NOT create (documented, not automated —
// these are account-level IAM/VPC concerns outside a chart install):
//   - IAM permissions for the controller. By default the chart installs its
//     own service account with no role annotation, so the node instance
//     role must carry the controller's IAM policy (node-role credentials).
//     Alternatively, a PRE-created IRSA service account (AWS's documented
//     eksctl path) is reused by setting
//     spec.cluster.eks.awsLoadBalancerControllerServiceAccount — the chart
//     then installs with serviceAccount.create=false and that name, and the
//     IAM role annotation on the pre-created account carries permissions.
//   - Subnet tags for load balancer discovery (kubernetes.io/role/elb and
//     kubernetes.io/role/internal-elb); eksctl-created VPCs tag these
//     automatically.
//
// Installation is an experimental opt-in gated by
// spec.cluster.eks.experimentalAWSLoadBalancerController together with
// spec.cluster.loadBalancer: Enabled. It runs at cluster create, on the
// operator's reconcile, and during cluster update when the opt-in changes.
package awslbcontrollerinstaller
