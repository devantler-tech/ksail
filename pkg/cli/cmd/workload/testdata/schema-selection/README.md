# Schema selection fixture

`helmrelease-v2.json` is an unmodified copy of the public
[CRDs-catalog HelmRelease v2 schema](https://github.com/datreeio/CRDs-catalog/blob/27233a506de0085e71c7f9702597806a7f95eb24/helm.toolkit.fluxcd.io/helmrelease_v2.json).
The adjacent `LICENSE` retains that revision's original MIT terms.

The command tests serve these original bytes through a verified, loopback-only
TLS registry. Built-in misses are real HTTP 404 responses; no invented `null`
schema is put in the cache. This tests schema selection and native validation,
not a claim that every remote reference is available offline.
