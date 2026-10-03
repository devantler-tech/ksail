# 0009: Keep otelzap compatible with the fixed logging SDK

- Status: Accepted
- Date: 2026-10-02
- Related: #7411

## Context

Kubescape's logging dependencies use otelzap v0.3.2. Its otelutil dependency uses attribute constructors removed from the OpenTelemetry logging API. The available adapter therefore cannot compile with the logging SDK release that fixes the BatchProcessor busy-spin vulnerability.

KSail needs the fixed SDK without disabling logging, replacing the SDK implementation or accepting that vulnerability. The CLI and desktop share the same Go dependency graph.

## Decision

Use a narrowly patched copy of otelzap v0.3.2 as a local module replacement. Preserve its public API, BSD license and logging behavior. Use OpenTelemetry attribute values for record attributes and bodies, and keep its recursive value conversion inside the adapter. The logging API, SDK and HTTP exporter remain unreplaced upstream modules at v0.21.0 or later.

Store the original module checksum and a reproducible patch. Required CI reconstructs the adapter from that exact upstream module and compares every source file with the selected local module. The comparison rejects missing or extra files, source changes and symlinks; a top-level provenance note is the only excluded file.

Exercise structured fields and recursive values through the public adapter. Exercise binary and integer fields and trace context through the actual fixed BatchProcessor and HTTP exporter. Release-graph tests require the fixed, unreplaced logging modules for every shipped CLI and desktop target and reject incomplete graphs or unverified vendor sources.

## Consequences

- KSail owns this small compatibility patch and its provenance checks until an upstream adapter supports the fixed logging API.
- The local replacement applies to the adapter only. It cannot claim to supply the fixed SDK, and it does not add a vulnerability exception.
- The original module license and public API remain intact. Binary fields retain their byte representation, and array conversion handles values that reflection cannot slice.
- Adoption of a compatible upstream adapter must remove the local replacement, copied module and reconstruction patch together while preserving the behavior and fixed-module tests.
