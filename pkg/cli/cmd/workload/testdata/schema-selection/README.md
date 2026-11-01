# Schema selection fixture

`helmrelease-v2.json` contains the public
[CRDs-catalog HelmRelease v2 schema](https://github.com/datreeio/CRDs-catalog/blob/27233a506de0085e71c7f9702597806a7f95eb24/helm.toolkit.fluxcd.io/helmrelease_v2.json).
The adjacent `LICENSE` retains that revision's original MIT terms.

The repository formatter normalizes JSON layout. The original download's SHA-256
is `73e96c113f28b430121890e75b5980c3712bda9b7bde223a82149b40b83406bb`.
The fixture and persisted cache must both retain the original JSON tokens:
SHA-256 after Go's `json.Compact` is
`c853417c55e892ae5ea7041629af046477fd70a07485c92fa656fd6dfdb65d38`.
Only insignificant whitespace is removed; schema rules, strings, numeric tokens,
key ordering and duplicate keys remain part of that identity.

The command tests serve this pinned schema through a verified, loopback-only
TLS registry. Built-in misses are real HTTP 404 responses; no invented `null`
schema is put in the cache. This tests schema selection and native validation,
not a claim that every remote reference is available offline.
