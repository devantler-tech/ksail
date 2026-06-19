// Package render expands a GitOps manifest stream into the resources Flux would
// actually apply, so shift-left tooling (validate/scan) reasons about real
// output rather than opaque custom resources.
//
// The input is the multi-document YAML produced by `kustomize build` and Flux
// variable substitution (callers run both before calling Expand). Expand parses
// that stream, and for every Flux HelmRelease it resolves the chart from the
// OCIRepository / HelmRepository source objects present in the same stream and
// renders it in-process with Helm (see HelmChartResolver, backed by
// pkg/client/helm). A successfully rendered HelmRelease is replaced by its
// rendered children; one that cannot be rendered offline is left in place and
// reported as a Degradation so callers fall back to validating the CR shape and
// never hard-fail purely because a chart could not be resolved.
//
// Chart source resolution is intentionally limited to OCIRepository and
// HelmRepository (including type: oci); GitRepository, HelmChart and Bucket
// sources, and sources not present in the stream, degrade gracefully.
package render
