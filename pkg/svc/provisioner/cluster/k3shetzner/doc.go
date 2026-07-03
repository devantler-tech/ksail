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
// devantler-tech/ksail#5512 (epic #3983). The shared create flow
// ([Provisioner.Create] via hetznerbase) runs the live bring-up end to end:
// it mints the bootstrap material (client keypair plus pinned host identity),
// threads it — via this package's composeNodes callback — into every node's
// cloud-init document, and derives the live server specs; the flow then creates
// the server, retrieves the admin kubeconfig, rewrites its endpoint, and
// persists it. The live E2E validation stays with the Hetzner system-test lane
// (#5515 / #4972).
package k3shetzner
