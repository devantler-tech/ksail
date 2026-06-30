package k3shetzner

import "errors"

var (
	// ErrClusterAlreadyExists is returned by [Provisioner.Create] when servers for
	// the target cluster already exist, so creation would collide with a running
	// cluster.
	ErrClusterAlreadyExists = errors.New("k3s-hetzner: cluster already exists")

	// ErrLiveBringUpNotImplemented is returned by [Provisioner.Create] after the
	// shared infrastructure is ensured and the per-node cloud-init user_data is
	// composed. The remaining steps — creating the servers (which needs boot-image
	// resolution), the runtime join sequencing that depends on the first server's
	// private address, and retrieving the generated kubeconfig (k3s exposes no
	// remote config API and SSH is out of scope by design) — are integration paths
	// that land with the Hetzner system-test lane (devantler-tech/ksail#5515).
	ErrLiveBringUpNotImplemented = errors.New(
		"k3s-hetzner: live cluster bring-up is not yet implemented (tracked by #5515)",
	)

	// ErrMultiNodeNotImplemented is returned by [Provisioner.Create] for a topology
	// with joining nodes (more than one control-plane node, or any agent). Joining
	// nodes register against the first server's address, which is only known once
	// that server is running, so multi-node bring-up requires the runtime
	// sequencing tracked by devantler-tech/ksail#5515.
	ErrMultiNodeNotImplemented = errors.New(
		"k3s-hetzner: multi-node bring-up is not yet implemented (tracked by #5515)",
	)
)
