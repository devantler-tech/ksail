// Package k3sbootstrap renders the typed configuration a k3s node reads at
// startup — the contents of /etc/rancher/k3s/config.yaml — from a typed
// [NodeConfig]. It is the native-Go configuration slice of the Hetzner ×
// K3s/Vanilla provisioning work (devantler-tech/ksail#3983, parent #4627,
// slice #5510).
//
// # Why a config renderer, not an install command
//
// ksail provisions every distribution through native Go, never by authoring or
// shelling out to install scripts (Talos via its machinery SDK, k3d/kind via
// their Go libraries, Hetzner via hcloud-go). There is, however, no Go SDK that
// *installs* k3s on a raw VM, so installation is delegated to declarative
// cloud-init (the standard mechanism for first-boot VM bring-up) carried by the
// sibling cloudinitbootstrap transport over the Hetzner server's user_data.
//
// The seam between the two is the *configuration*: k3s reads a typed
// config.yaml whose keys mirror its CLI flags, so the part with a real typed
// surface is rendered here in native Go and dropped onto the node declaratively
// (cloud-init write_files), while the install itself stays declarative. This
// keeps ksail's code free of imperative bash: a config.yaml is data the YAML
// marshaller encodes, with none of the shell-quoting or injection surface a
// rendered `curl … | sh` command string carries.
//
// # What it renders
//
// [Render] maps a [NodeConfig] — node role (cluster-initialising server,
// additional server, or agent), shared token, join server URL, TLS SANs,
// disabled components, and kubeconfig mode — to the config.yaml content for that
// node. It is pure: no I/O, no network, fully unit-testable without a cluster or
// a Hetzner account.
//
// The k3s *version* is deliberately not part of config.yaml — it is pinned at
// install time (INSTALL_K3S_VERSION) by the cloud-config that installs k3s, not
// by the running node's configuration.
//
// # Token exposure
//
// The shared node token is written into config.yaml. On the node that file is
// root-readable only, but when the config is delivered through cloud-init
// user_data it inherits user_data's audience (anyone holding the Hetzner API
// token can read it) — the same inherent property the cloudinitbootstrap package
// documents, not a property of this renderer.
package k3sbootstrap
