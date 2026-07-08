// Package csrapprover provides the kubelet-serving-cert-approver manifest
// for embedding in Talos machine configs via cluster.inlineManifests.
//
// The upstream project (alex1989hu/kubelet-serving-cert-approver) recommends
// the :main image tag for deployments. Versioned images stopped at 0.6.1 in
// GHCR, but the :main tag receives continuous updates. The Dockerfile in this
// package tracks the image for documentation; Dependabot will update it if
// versioned images resume.
//
// See: https://github.com/alex1989hu/kubelet-serving-cert-approver
package csrapprover

import _ "embed"

// manifestTemplate is the kubelet-serving-cert-approver standalone deployment manifest,
// embedded verbatim from manifest.yaml so it stays a byte-for-byte, easily diffable copy of
// https://github.com/alex1989hu/kubelet-serving-cert-approver/blob/main/deploy/standalone-install.yaml
// (the upstream-recommended :main image tag) — kept as a real .yaml file, not an inline Go string
// literal, so the repo's manifest-hygiene tooling (kubeconform, jscpd) treats it as the vendored data
// it is rather than duplicated Go source.
//
//go:embed manifest.yaml
var manifestTemplate string

// Manifest returns the kubelet-serving-cert-approver manifest YAML.
// The manifest uses the upstream-recommended :main image tag.
func Manifest() string {
	return manifestTemplate
}
