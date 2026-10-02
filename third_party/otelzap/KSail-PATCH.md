# KSail OpenTelemetry logging compatibility patch

This module is `github.com/uptrace/opentelemetry-go-extra/otelzap` v0.3.2,
with module checksum `h1:cj/Z6FKTTYBnstI0Lni9PA+k2foounKIPUmj1LBwNiQ=`.
The original BSD license and public logger API are retained.

OpenTelemetry logging v0.21 uses `attribute.Value` and `attribute.KeyValue`.
KSail's adapter uses those types for record bodies, structured fields and
sugared values. Its sole `otelutil.LogValue` dependency is implemented here
with the same scalar, recursive-slice, stringer and JSON conversions, using
direct indexing for arrays so an unaddressable array does not panic. Binary
Zap fields remain byte values rather than integer arrays.

The copied README links to the existing upstream documentation instead of an
example directory that is absent from the published module. Its code examples
use the indentation required by the repository's documentation checks.

The root module selects the v0.21 API, SDK and HTTP exporter together. The
batch processor is the fixed upstream implementation; no logging operation,
processor or vulnerability check is disabled. The integration regression
checks fields and context at the adapter boundary, and a real batch processor
and HTTP exporter produce protobuf records through a captured transport.

Cloning rebinds the OTel logger to the clone's provider, instrumentation version
and schema while preserving its logger name. The error-status threshold applies
to recording spans independently of the emission threshold for structured,
formatted and key/value logging. Suppressed formatted messages stay unevaluated
when no recording span qualifies for an error status.

The compatibility patch records the differences from the checksum-verified
upstream module. Source changes must update that patch and retain the
integration regression. Remove the local replacement once a published
adapter supports OTel logging v0.21 or later and the regression passes.
