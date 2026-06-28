// Package k3sbootstrap renders the native k3s install commands used to bootstrap
// a K3s node on a raw Linux server (e.g. a Hetzner Cloud server), as opposed to
// the Docker-local k3d path.
//
// It is the first, transport-agnostic slice of the Hetzner × K3s provisioning
// work (devantler-tech/ksail#3983, parent #4627): given a typed [InstallConfig],
// [Render] produces the exact `curl … | … sh -s - {server,agent} …` command the
// official k3s install script expects. How that command is delivered to the
// server — over SSH or via cloud-init user_data — is a separate concern handled
// by the remote-exec transport that consumes this package.
//
// The renderer is pure: it performs no I/O and reaches no network, so it is fully
// unit-testable without a cluster or a Hetzner account.
package k3sbootstrap
