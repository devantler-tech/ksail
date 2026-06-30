// Package k3shetzner provisions a K3s cluster on Hetzner Cloud servers.
//
// It composes three merged foundations rather than shelling out to a CLI:
//   - k3sbootstrap renders each node's native k3s install command (the
//     upstream-documented, Docker-free install for a raw Linux server);
//   - cloudinitbootstrap wraps that command in a cloud-init user_data document
//     that runs once at first boot — the declarative, SSH-free delivery seam;
//   - the hetzner provider creates the network, firewall, placement group, and
//     servers, attaching the user_data so each node bootstraps itself.
//
// The sequencing mirrors the Talos × Hetzner provisioner: ensure shared
// infrastructure, then create the cluster-initialising control-plane node, then
// the joining nodes. SSH-based remote execution is intentionally out of scope —
// cloud-init is the declarative transport — so it is not used here.
//
// This package is the K3s × Hetzner provisioner tracked by
// devantler-tech/ksail#5512 (epic #3983). The K3s × Hetzner distribution/provider
// combination stays unselectable until the validation flip (#5514), so the
// factory wiring exists but the path is gated. Live bring-up — actual server
// creation (which needs the boot-image resolution), the runtime join sequencing
// that depends on the first server's private address, and pulling the generated
// kubeconfig off a node — lands with the Hetzner system-test lane (#5515 / #4972);
// [Provisioner.Create] composes the per-node user_data and prepares the
// infrastructure up to that boundary.
package k3shetzner
