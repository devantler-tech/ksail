# Release admission

Every CD job consumes the preflight's decision for the current run attempt before
building or changing a publication. A partial rerun carrying an older decision
fails immediately and asks for a full CD replay. Missing decisions also fail.

The [CD workflow](../../workflows/cd.yaml) contains the supported calls. Jobs that
use repository scripts check out the release source; this action needs no
checkout. Calls pin this action to its reviewed commit, preserving static
validation while actionlint gains support for GitHub's newer self-repository
syntax.

`attempt` and `mode` come from the preflight, directly or through its downstream
job outputs. `proof` is required for `verify` mode and binds the publication to
the current tag, source and original run. The preflight verifies its release
attestation and OCI signatures; this action checks the receipt's identity before
allowing a consumer to use it.

See [the release replay guide](../../docs/release-replay.md) for supported
recovery and historical-run limitations.
