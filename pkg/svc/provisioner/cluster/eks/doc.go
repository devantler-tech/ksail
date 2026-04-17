// Package eksprovisioner provisions Amazon EKS clusters by shelling out to
// the eksctl CLI via pkg/client/eksctl.
//
// The eksctl Go packages under github.com/weaveworks/eksctl/pkg/actions are
// not designed for library use: the canonical Create path lives in the
// Cobra-coupled pkg/ctl tree, and importing pkg/actions/cluster transitively
// pulls in kops, cfssl, kubicorn, and amazon-ec2-instance-selector — each
// requiring explicit dependency pins. Binary invocation keeps KSail's module
// graph small and isolates EKS behind a single external binary requirement
// (eksctl) that AWS users already install.
//
// The eksctl binary must be on PATH. If it is not, Create/Delete/Update
// surface a clear error via pkg/client/eksctl.CheckAvailable.
package eksprovisioner
