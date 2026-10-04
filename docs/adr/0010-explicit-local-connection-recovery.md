# Explicit local connection recovery

Status: Proposed for #7505.

Deleting a nested cluster requires current ownership evidence from its host. A deletion can remove
the final owned namespace and then fail while reading another namespace. The connection remains,
but its name cannot authorize further resource deletion. Host API failures remain visible errors.

`cluster forget` is a separate, default-off experimental operation that edits one explicitly named
kubeconfig file and one exact context. It never constructs a provider or REST client, runs credential
plugins, or reads host resources. It does not certify that remote resources are absent. An operator
chooses it only to retire a local connection, independently of resolving a failed resource deletion.

The operation preserves every other context and keeps cluster or credential records referenced by
any remaining context in that file. It clears the current context only when that exact context is
removed. A missing context is an unchanged success; read, parse and write failures remain errors.
The replacement file is written atomically with private permissions. The selected file is the full
scope: configuration from environment-selected files is never merged into the operation.

The command requires `--experimental`, `--kubeconfig` and `--context`. It remains hidden from normal
help and generated AI tools until the local recovery interface is explicitly graduated in #7510.
