// Package sshbootstrap is the post-provision half of the server bootstrap
// transport: a minimal, provider-agnostic SSH client for talking to a server
// that cloud-init (pkg/svc/bootstrap/cloudinit) has already brought up.
//
// The provision-time seam delivers install commands as user_data at server
// creation; it is fire-and-forget and cannot observe the server afterwards.
// The K3s × Hetzner (devantler-tech/ksail#5512) and kubeadm × Hetzner
// (devantler-tech/ksail#5513) live bring-up paths additionally need to reach
// the first server after boot — to retrieve the kubeconfig
// (/etc/rancher/k3s/k3s.yaml, /etc/kubernetes/admin.conf) and to observe join
// sequencing — which is exactly the seam this package provides
// (devantler-tech/ksail#5696):
//
//   - [GenerateKeyPair] mints the per-cluster ed25519 identity: the public half
//     is registered at server creation, the private half authenticates the
//     client.
//   - [DialWithRetry] waits for the server's SSH endpoint to come up after
//     first boot, bounded by the caller's context.
//   - [Client.Run] executes a single command; [Client.ReadFile] streams a
//     remote file (the kubeconfig) over the same exec channel, so no SFTP
//     subsystem or extra dependency is needed.
//
// Everything is native Go (golang.org/x/crypto/ssh) — no shelled-out ssh
// binary — and unit-testable against an in-process SSH server, so the package
// carries no Hetzner coupling and no cloud cost.
package sshbootstrap
