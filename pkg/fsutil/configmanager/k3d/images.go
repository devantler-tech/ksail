package k3d

import (
	_ "embed"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// k3sImage returns the K3s container image reference from the embedded Dockerfile.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func k3sImage() string {
	return parser.ParseImageFromDockerfile(dockerfile, `FROM\s+(rancher/k3s:[^\s]+)`, "K3s")
}

// DefaultK3sVersion returns the K3s version tag (e.g. "v1.36.1-k3s1") from
// DefaultK3sImage ("rancher/k3s:<tag>@sha256:<digest>"), without the registry,
// repository, or digest. It is used to pin nested k3k clusters to the same K3s
// version as standalone K3d so they share the proven apiserver behavior (e.g. the
// admissionregistration.k8s.io/v1beta1 API needed by Calico's CRD chart). Returns
// "" if the reference has no tag.
func DefaultK3sVersion() string {
	// Strip the digest, then take the tag after the final ':' in "repo:tag".
	ref, _, _ := strings.Cut(DefaultK3sImage, "@")

	idx := strings.LastIndex(ref, ":")
	if idx < 0 {
		return ""
	}

	return ref[idx+1:]
}
