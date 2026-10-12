# Public Image Factory transport comparison

This internal diagnostic investigates KSail #7707. It does not change KSail's
production transport, image configuration or registration retries.

The dedicated Actions workflow uses Ubuntu 24.04 and the repository's Go toolchain,
without production environments, secrets, Kubernetes access or provider resources.
It registers only the fixed empty public schematic. There are no URL, credentials,
schematic, or cluster inputs.

Each of two trials pairs curl with seven SDK requests on the same hosted runner:

- The SDK's unmodified default HTTP client.
- HTTP/1.1 only.
- Go's [pre-1.26 curve set](https://github.com/golang/go/blob/go1.25.0/src/crypto/tls/defaults.go),
  including X25519MLKEM768; certificate verification is still enabled.
- IPv4 only.
- Smaller TCP writes with unchanged TLS defaults.
- Classical X25519 and TLS 1.2 controls, **only for this synthetic public request**.
  Neither is a proposed production downgrade or fallback.

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
