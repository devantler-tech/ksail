// Package hetznerbase carries the Hetzner infrastructure lifecycle shared by the
// k3s and kubeadm × Hetzner cluster provisioners.
//
// The two provisioners are deliberate siblings: they use the same Hetzner provider
// seam ([Infra]) and the same network/firewall/placement-group/SSH-key setup and
// server Delete/Start/Stop/List/Exists operations, differing only in how they
// compose each node's cloud-init user_data (a k3s install command vs. a declarative
// kubeadm #cloud-config) and the node token they generate. This package holds that
// shared base ([Base]) so each provisioner embeds it instead of duplicating the
// lifecycle; see devantler-tech/ksail#5650 (epic #3983).
package hetznerbase
