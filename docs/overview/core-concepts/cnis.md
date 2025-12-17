# Container Network Interfaces (CNI)

The Container Network Interface determines how pods receive IP addresses and communicate inside your cluster. KSail-Go exposes CNI selection both declaratively (via `spec.cluster.cni`) and imperatively with `ksail cluster init --cni`.

## Available Options

### `Default`

Uses the distribution's built-in networking (`kindnetd` for Kind, `flannel` for K3d). Choose this for quick local iterations and CI environments where defaults are already pre-tested.

### `Cilium`

Installs [Cilium](https://cilium.io/) through the GitOps manifests generated at init time. Pick Cilium when you need advanced observability, eBPF-based policies, or WireGuard encryption.

### `None`

Skips CNI installation entirely. Use this option when you want to install a different CNI manually (for example Calico or custom lab scenarios).

> **Tip:** The init command writes your selection to `ksail.yaml`. Future runs of `ksail cluster create` read from that file, so the entire team shares the same networking baseline.
