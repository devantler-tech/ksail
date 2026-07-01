// Package kubeadmhetzner composes the cloud-init user_data that bootstraps a
// kubeadm (Vanilla Kubernetes) cluster on Hetzner Cloud servers.
//
// It is the kubeadm sibling of the k3shetzner provisioner and the first place the
// independently-built kubeadm foundations are composed end to end:
//   - kubeadmbootstrap plans the cluster topology and renders each node's kubeadm
//     configuration and its declarative, distribution-native install (apt sources,
//     packages, files, first-boot commands) — never a `curl … | sh`;
//   - containerdbootstrap renders the /etc/containerd/config.toml that makes the
//     container runtime agree with kubeadm's systemd cgroup driver;
//   - cloudinitbootstrap marshals the whole install into a #cloud-config document —
//     the declarative, SSH-free delivery seam attached to each server's user_data.
//
// This is the user_data-composition slice of the Hetzner × Vanilla provisioning
// work (devantler-tech/ksail#5513, epic #3983). The Hetzner server lifecycle
// (network, firewall, placement group, server create/delete) and the run-time join
// sequencing that depends on the first control plane's address are provided by the
// provisioner that consumes this composer — a later increment, mirroring how
// k3shetzner wraps its own user_data builder.
package kubeadmhetzner
