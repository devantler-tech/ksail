# EKS control-plane upgrade evaluation

The manual **System Test - EKS** workflow can evaluate the experimental upgrade
path on the disposable cluster it creates. Leave both upgrade inputs empty for
the existing scaling and GitOps smoke test. To enable the trial, select a
supported starting Kubernetes version and its next minor version using
`upgrade_from_version` and `upgrade_to_version` (for example `1.34` and `1.35`).
Choose versions that AWS currently supports in the selected region. The workflow
rejects incomplete pairs, same-version pairs, skipped minors, and downgrades
before creating resources.

The trial creates billable resources. Dispatch it only within the approved AWS
test budget and against the branch/head being evaluated. The existing scoped
OIDC role must allow `eks:UpdateClusterVersion`, `eks:DescribeUpdate`, and
`eks:ListUpdates` in addition to the smoke test's inspection and lifecycle
permissions. Its maximum session duration must be at least two hours. This
change does not modify the role or its trust policy.

The trial checks these observed outcomes:

1. The cluster is ACTIVE at the requested starting version.
2. A real temporary session with its expiry metadata omitted is rejected by the
   credential lifetime guard, with no new AWS update or cluster change.
3. The same session, including the expiration returned by STS, completes a
   one-minor upgrade. Exactly one new AWS version update is successful.
4. The cluster ARN and creation timestamp are preserved, its version matches the
   target, and the Kubernetes readiness endpoint succeeds.
5. Repeating the target succeeds without submitting another AWS update.

STS credentials and the OIDC token stay in an owner-only temporary directory
that the driver removes on success, error, or termination. They are never
uploaded. The trial obtains a fresh session through GitHub OIDC and exposes its
real expiry through AWS's `credential_process` format; the action's usual
environment tuple cannot carry that metadata. Final cleanup independently
refreshes credentials, waits up to 20 minutes for an accepted update to settle,
and retains its full 45-minute deletion budget, even after trial failure. AWS
rejects deletion during an update, which can continue after the CLI stops
waiting. A failed settling wait still runs deletion and leaves the job failed;
inspect cleanup and resolve any remaining resources before declaring completion.

The local command-double controls run through `go test ./internal/ciharness/...`.
They establish that the driver rejects false success and cleans up credentials;
they do not replace a successful workflow run on real AWS.

References: [GitHub OIDC token requests](https://docs.github.com/en/actions/reference/security/oidc#methods-for-requesting-the-oidc-token),
[AWS external credential process format](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sourcing-external.html).
