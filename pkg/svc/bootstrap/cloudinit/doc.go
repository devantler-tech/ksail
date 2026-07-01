// Package cloudinitbootstrap builds the cloud-init user_data that bootstraps a
// raw Linux server (e.g. a Hetzner Cloud server) at first boot. It carries both
// imperative and declarative directives: shell Commands (for example the native
// k3s install command rendered by the sibling k3sbootstrap package) and, for a
// fully declarative install, cloud-init's own Packages, AptSources, and Files
// modules — so a node can install its packages and trust an APT signing key
// without a `curl | sh` at first boot.
//
// It is the provision-time delivery slice of the Hetzner × K3s/Vanilla
// provisioning work (devantler-tech/ksail#3983, parent #4627, slice #5511): a
// raw server has no Docker daemon to install into, so the bootstrap directives
// must reach it some other way. Baking them into the server's cloud-init
// user_data is the simplest such transport — it needs no SSH key handling or
// post-provision connection, and it composes with provisioner-level sequencing
// (create the cluster-init server, wait for its API, then create the joining
// nodes whose own user_data registers against it).
//
// [BuildUserData] is the pure builder; [Transport] wraps it as the
// [UserDataProvider] seam the K3s × Hetzner provisioner (#5512) depends on. A
// future SSH transport that executes on an already-running server is a separate,
// post-provision concern (its dial path is integration-tested behind the Hetzner
// system-test lane, #4972).
//
// Everything here is pure: it performs no I/O and reaches no network, so it is
// fully unit-testable without a cluster or a Hetzner account. Note that
// cloud-init user_data is readable by anyone holding the Hetzner API token, so
// sensitive material embedded in a directive (such as the k3s node token in a
// command) is exposed to that audience; that is inherent to user_data delivery,
// not a property of this package. An apt source's Key is a public signing key,
// so exposing it carries no such risk.
package cloudinitbootstrap
