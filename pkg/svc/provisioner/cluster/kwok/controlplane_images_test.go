package kwokprovisioner_test

import (
	_ "embed"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kwok/pkg/consts"
	"sigs.k8s.io/kwok/pkg/kwokctl/k8s"
	"sigs.k8s.io/kwok/pkg/utils/version"
)

// controlPlaneDockerfile embeds the KWOK control plane Dockerfile so the build
// breaks if the file is renamed or removed, and the test below catches tag drift.
//
// Dockerfile.controlplane is the manually-maintained source of truth that
// .github/actions/warm-mirror-cache greps to pre-seed kwokctl's control plane
// images into the runner's Docker daemon. It is not referenced by any production
// Go code, so without this guard a sigs.k8s.io/kwok module bump that changes the
// default Kubernetes/etcd/controller version would silently desync the pinned
// tags from what kwokctl actually requests: kwokctl's "docker inspect" would
// miss and every KWOK system-test leg would fall back to live registry.k8s.io
// pulls (issues #4058 / #4069 / #4495). This converts that silent CI flake into
// an actionable, at-dep-bump-time test failure.
//
//go:embed Dockerfile.controlplane
var controlPlaneDockerfile string

// TestControlPlaneImageTagsMatchKwokDefaults asserts the image tags pinned in
// Dockerfile.controlplane exactly match the versions the embedded
// sigs.k8s.io/kwok module defaults to (and the KWOK controller version KSail
// pins in provisioner.go).
func TestControlPlaneImageTagsMatchKwokDefaults(t *testing.T) {
	t.Parallel()

	// kube-apiserver / kube-controller-manager / kube-scheduler / kubectl track
	// KWOK's default Kubernetes version (consts.KubeVersion, e.g. "1.33.0" → the
	// "v1.33.0" image tag).
	kubeTag := version.AddPrefixV(consts.KubeVersion)

	// etcd tracks the version KWOK maps the Kubernetes minor release to, exactly
	// as KWOK's own setKwokctlEtcdConfig does: k8s.GetEtcdVersion(<minor>).
	kubeVersion, err := version.ParseVersion(consts.KubeVersion)
	require.NoError(t, err, "kwok consts.KubeVersion must be valid semver")

	etcdTag := k8s.GetEtcdVersion(kubeMinor(kubeVersion))

	// kwok-controller tracks the released image version KSail pins in provisioner.go.
	kwokTag := kwokprovisioner.KwokControllerImageVersionForTest

	want := []string{
		"registry.k8s.io/etcd:" + etcdTag,
		"registry.k8s.io/kube-apiserver:" + kubeTag,
		"registry.k8s.io/kube-controller-manager:" + kubeTag,
		"registry.k8s.io/kube-scheduler:" + kubeTag,
		"registry.k8s.io/kubectl:" + kubeTag,
		"registry.k8s.io/kwok/kwok:" + kwokTag,
	}

	got := parser.ParseAllImagesFromDockerfile(controlPlaneDockerfile)

	assert.ElementsMatch(t, want, got,
		"Dockerfile.controlplane image tags are out of sync with the embedded kwok module "+
			"defaults; update pkg/svc/provisioner/cluster/kwok/Dockerfile.controlplane (and let "+
			"the CI mirror cache re-warm) when bumping sigs.k8s.io/kwok")
}

// kubeMinor returns the Kubernetes minor release as an int, mirroring KWOK's own
// parseRelease (pkg/config/vars.go) that feeds k8s.GetEtcdVersion. The minor of a
// Kubernetes release is always small and non-negative, so the conversion is safe.
func kubeMinor(kubeVersion version.Version) int {
	const minorMax = 1 << 30 // guards the uint64→int conversion against absurd input.

	if kubeVersion.Minor > minorMax {
		return minorMax
	}

	return int(kubeVersion.Minor)
}
