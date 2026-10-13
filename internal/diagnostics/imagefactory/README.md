# Public Image Factory transport comparison

This internal diagnostic investigates KSail #7707. The production registration
client uses the shared smaller-write transport; TLS versions, hybrid groups,
certificate verification, image configuration and registration retries are unchanged.

The dedicated Actions workflow uses Ubuntu 24.04 and the repository's Go toolchain,
without production environments, secrets, Kubernetes access or provider resources.
It registers only the fixed empty public schematic. There are no URL, credentials,
schematic, or cluster inputs.

Each of two trials pairs curl with six SDK requests on the same hosted runner:

- The SDK's unmodified default HTTP client.
- HTTP/1.1 only.
- Go's [pre-1.26 curve set](https://github.com/golang/go/blob/go1.25.0/src/crypto/tls/defaults.go),
  including X25519MLKEM768; certificate verification is still enabled.
- IPv4 only.
- The actual production transport's smaller TCP writes with unchanged TLS defaults.
- Classical X25519, **only for this synthetic public request**;
  it is not a proposed production downgrade or fallback.

Each registration has a 30-second budget, including any SDK follow-up request.
The complete comparison is bounded to fifteen minutes. Output contains only controlled
outcomes, phase-completion flags, negotiated protocol and elapsed time. It does not
print raw errors, addresses, headers, credentials or response bodies.

`diagnostic: completed` means observations were collected, not that they succeeded
or that the platform is repaired. Compare failed defaults with successful paired
controls and variants across both trials. If all SDK requests pass, the failure
has not been reproduced; investigate the actual CLI process or runtime differences.
Contradictory or interrupted observations remain inconclusive.

The local TLS fixtures require verified certificates, exact input and returned
schematic identities, rejection of an untrusted certificate, and cancellation.
They verify the probe's reporting boundaries, not public network reachability.

## Strict link transport observation

Dispatch the same workflow with `strict_links: true` to investigate the daily
link check on its configured `ubuntu-latest` runner. This default-off route skips
registration requests. It records bounded curl GET timings for the two fixed
public links, then checks those links with the daily Lychee version and configuration.
It also runs the complete daily strict caller, even when the focused link check fails.

`observation=completed` means both curl requests were observed; their exit codes
and the two strict checks determine reachability. Timeouts remain failures, TLS
verification stays enabled, and no addresses, headers or raw curl errors are printed.
The workflow has a twenty-minute deadline. A successful engineering-host check
does not establish hosted recovery; compare observations made in the same run.
