# KSail analysis runner

This image supplies Go, Node.js and the GTK/WebKit development libraries needed
for complete CLI and Linux desktop extraction on the platform's ARC pool.
The base image, package snapshot and downloaded toolchains are pinned.

The publisher verifies the actual Linux image by executing the non-root runner
listener binary and compiling cgo, using a read-only root filesystem, no capabilities and
no privilege escalation. Its writable runner home and temporary directory
must allow execution of the copied listener and compiler output.
This binary check does not prove registration or job delivery; the actual ARC
preflight below provides that proof after Platform activation.

Only a verified push to main publishes
`ghcr.io/devantler-tech/ksail-analysis-runner:<commit>`. The workflow also runs
the smoke test against that published digest before signing it, then verifies
the exact certificate identity:

```text
https://github.com/devantler-tech/ksail/.github/workflows/publish-ksail-analysis-runner.yaml@refs/heads/main
```

Publication also requires an anonymous pull and signature verification of that
exact digest using a separate empty registry configuration. The publisher's
login cannot satisfy this gate. GHCR initially creates packages as private;
the package owner must set this public image's visibility to public after its
first publication. The workflow does not change package access or permissions.
If that prerequisite is missing, publication acceptance fails and Platform must
not activate the digest.

Platform owns registration, capacity, network and admission policy. It must
verify and pin the published digest before activation. Image validation does
not establish complete managed extraction or runner cleanup.

After Platform proves live admission, capacity and network isolation, the
main-only `Verify KSail ARC Delivery` workflow can be manually enabled to
exercise one real ARC job. It defaults to disabled and does not change managed
Code Quality's runner selection. Its `--live-runner` smoke mode preserves the
active listener and registration files while checking the desktop toolchain,
read-only root, zero capabilities and disabled privilege escalation. The job
also checks external connectivity, absence of an API-token mount and the bounded
container memory limit. Platform must join that job to the actual runner pod,
verify its pinned image and cleanup, then perform the complete managed analyses.
