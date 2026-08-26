# KSail compatibility patch

This directory contains the source of `github.com/moby/go-archive` v0.3.0,
tag commit `1c23372e409716c3691a540871806083644f348a`. Its module checksum is
`h1:nos4BtzzUIqB406BgQnWGMI4qib9BZ8XUHU+ucv/n1c=`.

KSail adds only `compat_legacy.go` and `compat_legacy_test.go`. They restore the
deprecated package-level compression aliases required by Docker 28.5.2 while
retaining v0.3.0's fix for GO-2026-6253. Remove this local module when KSail's
dependency graph no longer imports Docker's deprecated archive wrapper.

The nested module deliberately retains upstream's `go` directive; KSail's root
module owns the repository toolchain requirement, while changing the imported
module's directive would break the byte-parity boundary. CI runs
`.github/scripts/verify-go-archive-parity.sh`, which resolves the reviewed
module checksum and rejects every content difference except absent upstream
repository metadata, this note, and the two compatibility files.
