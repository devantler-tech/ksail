// Package kubeadmbootstrap renders the typed configuration a kubeadm node reads
// at bring-up — a kubeadm.k8s.io/v1beta4 InitConfiguration/ClusterConfiguration
// (for the cluster-initialising control plane) or JoinConfiguration (for a
// joining control plane or worker) — from a typed [NodeConfig]. It is the
// Vanilla (kubeadm) configuration slice of the Hetzner × K3s/Vanilla
// provisioning work (devantler-tech/ksail#3983, parent #4627, slice #5513), the
// sibling of the k3sbootstrap renderer.
//
// # Why a config renderer, not an install command
//
// ksail provisions every distribution through native Go, never by authoring or
// shelling out to install scripts (Talos via its machinery SDK, k3d/kind via
// their Go libraries, Hetzner via hcloud-go). There is no Go SDK that *installs*
// kubeadm and a container runtime on a raw VM, so installation is delegated to
// declarative cloud-init (the standard mechanism for first-boot VM bring-up,
// `packages:`/`write_files:` — never `curl … | sh`) carried by the sibling
// cloudinitbootstrap transport over the Hetzner server's user_data.
//
// The seam between the two is the *configuration*: kubeadm reads typed
// InitConfiguration/ClusterConfiguration/JoinConfiguration documents, so the
// part with a real typed surface is rendered here in native Go and dropped onto
// the node declaratively (cloud-init write_files), while the install itself
// stays declarative. This keeps ksail's code free of imperative bash: a kubeadm
// config is data the YAML marshaller encodes, with none of the shell-quoting or
// injection surface a rendered command string carries.
//
// # What it renders
//
// [Render] maps a [NodeConfig] — node role (cluster-initialising control plane,
// additional control plane, or worker), shared bootstrap token, and the
// per-role cluster or discovery settings — to the kubeadm config document(s) for
// that node. It is pure: no I/O, no network, fully unit-testable without a
// cluster or a Hetzner account.
//
// [Plan] sits one level up, expanding a cluster topology ([PlanInput]) into the
// ordered per-node [NodeConfig]s a provisioner bootstraps in sequence
// (cluster-initialising control plane → additional control planes → agents),
// mirroring the k3sbootstrap planner. Because kubeadm discovers a joining node's
// API server endpoint and CA cert hashes only once the first control plane is up,
// those are [PlanInput] fields a provisioner injects at run time before planning
// the joining nodes — unlike k3s's pre-shared token, which needs no run-time
// artifact. Every [Node] Plan returns carries a Config that passes validation, so
// [Render] never fails for a planned node.
//
// The Kubernetes *components* (kubeadm, kubelet, the container runtime) are
// installed at first boot by the cloud-config, not by the running node's
// configuration; only kubernetesVersion (which pins the control-plane image set)
// is part of ClusterConfiguration.
//
// # Token exposure
//
// The shared bootstrap token is written into the kubeadm config. On the node
// that file is root-readable only, but when the config is delivered through
// cloud-init user_data it inherits user_data's audience (anyone holding the
// Hetzner API token can read it) — the same inherent property the
// cloudinitbootstrap package documents, not a property of this renderer.
package kubeadmbootstrap
