// Package containerdbootstrap renders the containerd runtime configuration a
// kubeadm (Vanilla) node reads at bring-up — an /etc/containerd/config.toml that
// makes the container runtime agree with the way kubeadm configures the kubelet.
// It is the container-runtime slice of the Hetzner × K3s/Vanilla provisioning
// work (devantler-tech/ksail#3983, parent #4627, slice #5513), a sibling of the
// kubeadmbootstrap, k3sbootstrap and cloudinitbootstrap renderers.
//
// # Why a containerd config renderer
//
// kubeadm configures the kubelet to use the systemd cgroup driver (the default
// since kubeadm 1.22). A stock containerd install, however, defaults to the
// cgroupfs driver (SystemdCgroup = false). When the runtime and the kubelet
// disagree on the cgroup driver the kubelet cannot manage pod cgroups and the
// node never reaches Ready — the single most common failure of a hand-rolled
// kubeadm-on-containerd node. Rendering the runtime config so it enables the
// systemd cgroup driver removes that mismatch before the node ever boots.
//
// Secondarily, containerd's CRI sandbox_image (the "pause" image) should match
// the one kubeadm/kubelet expects; otherwise every pod sandbox triggers an extra
// pull and a version-mismatch warning. [RenderContainerdConfig] pins it when the
// caller supplies it and omits it (leaving containerd's default) when it does
// not.
//
// # Native Go, delivered declaratively
//
// Like the sibling renderers, this package authors typed configuration in native
// Go and never shells out. The container runtime itself is installed at first
// boot by the declarative cloud-config (packages:), and the config this package
// renders is dropped onto the node the same way — a cloud-init write_files entry
// (the transport capability added in #5620) — so no imperative bash touches the
// runtime. The output is a complete, deterministic containerd v2 config document
// prefixed with a ksail managed-file header.
//
// [RenderContainerdConfig] is pure: no I/O, no network, fully unit-testable
// without a cluster or a Hetzner account. It is orthogonal to the topology
// planner (#5611) and the kubeadm install renderer (#5616) — it imports neither
// — so it advances #5513 without stacking on those in-flight slices; a later
// provisioner increment folds all three into the node's cloud-init.
package containerdbootstrap
