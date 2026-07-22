package kubeadmhetzner

import (
	"fmt"
	"slices"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	containerdbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/containerd"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
)

// containerdConfigPermissions is the mode of the on-node containerd config dropped
// via cloud-init write_files: the runtime config carries no secrets and matches
// containerd's own world-readable default (0644).
const containerdConfigPermissions = "0644"

// Input is the typed input for [BuildNodeUserData]: the cluster's kubeadm topology
// plus the containerd/Hetzner delivery details the composer needs to turn each
// planned node into a deliverable cloud-init document. It performs no I/O.
type Input struct {
	// ClusterName labels every provisioned Hetzner server so the provider can find a
	// cluster's nodes.
	ClusterName string
	// Plan is the kubeadm cluster topology: the Kubernetes version, node counts, the
	// shared bootstrap token, and (for a multi-node cluster) the join settings. Its
	// KubernetesVersion is required here — it selects the community package
	// repository track every node installs the kube* components from — so a Plan
	// without it is rejected by the install renderer.
	Plan kubeadmbootstrap.PlanInput
	// SandboxImage optionally pins containerd's CRI "pause" sandbox image on every
	// node (see [containerdbootstrap.ContainerdConfig]). Empty leaves containerd's
	// built-in default in place.
	SandboxImage string
	// SSHAuthorizedKeys are public keys delivered into every node's
	// authorized_keys via cloud-init (see [cloudinitbootstrap.Config]) so the
	// post-provision SSH bootstrap seam can authenticate. Optional; the live
	// bring-up composition generates the bootstrap keypair and threads its
	// public half through here.
	SSHAuthorizedKeys []string
	// HostKeys is the pre-generated SSH host identity delivered into every node
	// via the cloud-init ssh_keys module (see [cloudinitbootstrap.HostKeys]) so
	// the bootstrap SSH dial can pin the host key instead of trusting first use.
	// Optional; nil lets the node generate its own host keys at first boot.
	HostKeys *cloudinitbootstrap.HostKeys
	// ServerInitFiles are extra files delivered only to the cluster-initialising
	// control plane — e.g. the pre-seeded cluster CA ([ClusterCA]) the two-phase
	// multi-node flow fixes the cluster identity with. Optional.
	ServerInitFiles []cloudinitbootstrap.File
	// ServerInitPrelude are commands run on the cluster-initialising control
	// plane before its install commands. Optional; ignored on joining nodes.
	ServerInitPrelude []string
	// JoinPrelude are commands run on every joining node before its install
	// commands — e.g. pinning the stable join name to the init control plane's
	// resolved private address in /etc/hosts, which must happen before `kubeadm
	// join` dials it. Optional; ignored on the cluster-initialising control plane.
	JoinPrelude []string
}

// NodeUserData pairs a planned node with the cloud-init user_data that bootstraps
// it and the Hetzner labels that identify the server it runs on. It mirrors the
// k3s × Hetzner provisioner's per-node shape.
type NodeUserData struct {
	// Index is the node's zero-based bootstrap position (the cluster-initialising
	// control plane is 0).
	Index int
	// Role is the node's kubeadm cluster role.
	Role kubeadmbootstrap.Role
	// UserData is the cloud-init document delivered as the server's user_data.
	UserData string
	// Labels is the Hetzner label set applied to the server.
	Labels map[string]string
}

// BuildNodeUserData composes the ordered per-node cloud-init user_data for a
// kubeadm cluster on Hetzner Cloud. For every node the plan produces it renders the
// kubeadm configuration ([kubeadmbootstrap.Render]), turns it into the declarative,
// distribution-native install ([kubeadmbootstrap.RenderInstall]), adds the
// containerd runtime configuration ([containerdbootstrap.RenderContainerdConfig]) so
// the runtime agrees with kubeadm's systemd cgroup driver, and marshals the whole
// install into a cloud-init document ([cloudinitbootstrap.BuildUserData]) — the
// declarative, SSH-free delivery seam. It attaches the Hetzner node labels.
//
// It is the first composition of the independently-built kubeadm foundations
// (Plan → Render → RenderInstall → containerd config → cloud-init) and the kubeadm
// sibling of the k3s × Hetzner provisioner's user_data builder. Where the k3s path
// installs by piping a script into a shell, the kubeadm path installs real OS
// packages declaratively (apt sources / packages / write_files), so this composer
// assembles the richer cloud-init Config form rather than a command-only document.
//
// BuildNodeUserData is pure — no I/O, no network — and never returns a partial
// result: any configuration error from a foundation renderer (an invalid topology,
// a missing Kubernetes version, a malformed sandbox image) is reported instead. The
// containerd config is identical for every node, so it is rendered once and shared.
func BuildNodeUserData(input Input) ([]NodeUserData, error) {
	nodes, err := kubeadmbootstrap.Plan(input.Plan)
	if err != nil {
		return nil, fmt.Errorf("plan kubeadm nodes: %w", err)
	}

	containerdConfig, err := containerdbootstrap.RenderContainerdConfig(
		containerdbootstrap.ContainerdConfig{SandboxImage: input.SandboxImage},
	)
	if err != nil {
		return nil, fmt.Errorf("render containerd config: %w", err)
	}

	containerdFile := cloudinitbootstrap.File{
		Path:        containerdbootstrap.ConfigPath,
		Permissions: containerdConfigPermissions,
		Content:     containerdConfig,
	}

	result := make([]NodeUserData, 0, len(nodes))

	for _, node := range nodes {
		userData, buildErr := buildNodeCloudInit(input, node, containerdFile)
		if buildErr != nil {
			return nil, buildErr
		}

		result = append(result, NodeUserData{
			Index:    node.Index,
			Role:     node.Config.Role,
			UserData: userData,
			Labels:   hetzner.NodeLabels(input.ClusterName, nodeType(node.Config.Role), node.Index),
		})
	}

	return result, nil
}

// buildNodeCloudInit renders one planned node's cloud-init user_data: the node's
// kubeadm configuration, the declarative install derived from it, the shared
// containerd config file, all marshalled into a single #cloud-config document. The
// cluster-wide Kubernetes version (not the per-node config's, which is set only on
// the cluster-initialising control plane) selects the package repository track so
// every node — including the joining ones — installs the kube* components from the
// same minor track.
func buildNodeCloudInit(
	input Input,
	node kubeadmbootstrap.Node,
	containerdFile cloudinitbootstrap.File,
) (string, error) {
	config, err := kubeadmbootstrap.Render(node.Config)
	if err != nil {
		return "", fmt.Errorf("render kubeadm config for node %d: %w", node.Index, err)
	}

	install, err := kubeadmbootstrap.RenderInstall(kubeadmbootstrap.InstallConfig{
		KubernetesVersion: input.Plan.KubernetesVersion,
		Role:              node.Config.Role,
		Config:            config,
	})
	if err != nil {
		return "", fmt.Errorf("render install for node %d: %w", node.Index, err)
	}

	files := append(toCloudInitFiles(install.Files), containerdFile)
	commands := install.Commands

	// The per-role extras: the init control plane receives the pre-seeded cluster
	// identity files (and any init prelude); every joining node runs the join
	// prelude (e.g. the /etc/hosts pin of the stable join name) before its
	// install commands reach `kubeadm join`. Additional control planes are not
	// given private cluster PKI through cloud-init user-data.
	switch node.Config.Role {
	case kubeadmbootstrap.RoleServerInit:
		files = append(files, input.ServerInitFiles...)
		if len(input.ServerInitPrelude) > 0 {
			commands = append(slices.Clone(input.ServerInitPrelude), commands...)
		}
	case kubeadmbootstrap.RoleServer:
		if len(input.JoinPrelude) > 0 {
			commands = append(slices.Clone(input.JoinPrelude), commands...)
		}
	case kubeadmbootstrap.RoleAgent:
		if len(input.JoinPrelude) > 0 {
			commands = append(slices.Clone(input.JoinPrelude), commands...)
		}
	}

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		AptSources:        toCloudInitAptSources(install.AptSources),
		Packages:          install.Packages,
		Files:             files,
		Commands:          commands,
		SSHAuthorizedKeys: input.SSHAuthorizedKeys,
		HostKeys:          input.HostKeys,
	})
	if err != nil {
		return "", fmt.Errorf("build cloud-init for node %d: %w", node.Index, err)
	}

	return userData, nil
}

// nodeType maps a kubeadm role to the Hetzner node-type label value: an agent is a
// worker, and every server (control-plane) role is a control plane.
func nodeType(role kubeadmbootstrap.Role) string {
	if role == kubeadmbootstrap.RoleAgent {
		return hetzner.NodeTypeWorker
	}

	return hetzner.NodeTypeControlPlane
}

// toCloudInitAptSources maps the kubeadm install's apt sources onto the cloud-init
// transport's own apt-source shape; the two are field-for-field equivalent.
func toCloudInitAptSources(
	sources []kubeadmbootstrap.AptSource,
) []cloudinitbootstrap.AptSource {
	out := make([]cloudinitbootstrap.AptSource, len(sources))
	for index, source := range sources {
		out[index] = cloudinitbootstrap.AptSource{
			Name:   source.Name,
			Source: source.Source,
			Key:    source.Key,
		}
	}

	return out
}

// toCloudInitFiles maps the kubeadm install's files onto the cloud-init transport's
// write_files shape; the two are field-for-field equivalent. A fresh slice is
// returned so a caller can append (e.g. the containerd config) without aliasing.
func toCloudInitFiles(files []kubeadmbootstrap.File) []cloudinitbootstrap.File {
	out := make([]cloudinitbootstrap.File, len(files))
	for index, file := range files {
		out[index] = cloudinitbootstrap.File{
			Path:        file.Path,
			Permissions: file.Permissions,
			Content:     file.Content,
		}
	}

	return out
}
