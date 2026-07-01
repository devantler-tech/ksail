// Package k3sbootstrap renders the native k3s install commands used to bootstrap
// a K3s node on a raw Linux server (e.g. a Hetzner Cloud server), as opposed to
// the Docker-local k3d path.
//
// It is the first, transport-agnostic slice of the Hetzner × K3s provisioning
// work (devantler-tech/ksail#3983, parent #4627): given a typed [InstallConfig],
// [Render] produces the exact `curl … | … sh -s - {server,agent} …` command the
// official k3s install script expects. [Plan] sits one level up, expanding a
// cluster topology ([PlanInput]) into the ordered per-node [InstallConfig]s a
// provisioner bootstraps in sequence (server-init → additional servers →
// agents). How a rendered command is delivered to the server — over SSH or via
// cloud-init user_data — is a separate concern handled by the remote-exec
// transport that consumes this package.
//
// [RenderConfig] is the declarative counterpart to [Render]: it maps a
// [NodeConfig] to the node's /etc/rancher/k3s/config.yaml content — the standing
// per-node settings file the install command's k3s service reads — for delivery
// via cloud-init write_files. It shares the [Role] values and validation
// sentinels with the command renderer.
//
// Both renderers and the planner are pure: they perform no I/O and reach no
// network, so they are fully unit-testable without a cluster or a Hetzner account.
package k3sbootstrap
