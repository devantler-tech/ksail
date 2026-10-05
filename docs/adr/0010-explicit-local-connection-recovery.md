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

The command bypasses the root command's connection-refresh hook, including on rejected invocations.
It uses client-go's cooperative `.lock` convention on the canonical file from reading through atomic
replacement, refuses a read-only destination, and refuses an observed content or identity change.
Other writers must use the same canonical path and honor that lock; this protocol does not serialize
arbitrary editors or symlink aliases that ignore it. A held lock is never removed to force recovery.

The command requires `--experimental`, `--kubeconfig` and `--context`. It remains hidden from normal
help and generated AI tools until the local recovery interface is explicitly graduated in #7510.
