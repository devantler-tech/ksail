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
// This is the Hetzner × Vanilla provisioning work of devantler-tech/ksail#5513
// (epic #3983). The Hetzner server lifecycle (network, firewall, placement group,
// server create/delete) comes from the shared hetznerbase engine, and the run-time
// two-phase join sequencing (devantler-tech/ksail#5755) is implemented in
// multinode.go: the cluster identity is a pre-seeded CA (ca.go) fixed at compose
// time, and the joining nodes dial a compose-time-stable join name whose
// resolution each node pins to the init control plane's private address at first
// boot — see [JoinName] for why a name rather than the IP.
package kubeadmhetzner
