# KSail analysis runner

This image supplies Go, Node.js and the GTK/WebKit development libraries needed
for complete CLI and Linux desktop extraction on the platform's ARC pool.
The base image, package snapshot and downloaded toolchains are pinned.

The publisher verifies the actual Linux image with a non-root runner listener
and cgo compilation, using a read-only root filesystem, no capabilities and
no privilege escalation. Its writable runner home and temporary directory
must allow execution of the copied listener and compiler output.

Only a verified push to main publishes
`ghcr.io/devantler-tech/ksail-analysis-runner:<commit>`. The workflow signs the
published digest and verifies the exact certificate identity:

```text
https://github.com/devantler-tech/ksail/.github/workflows/publish-ksail-analysis-runner.yaml@refs/heads/main
```

Platform owns registration, capacity, network and admission policy. It must
verify and pin the published digest before activation. Image validation does
not establish complete managed extraction or runner cleanup.
