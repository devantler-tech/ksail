package kind_test

import (
	"testing"

	kind "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/stretchr/testify/assert"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestApplyCDIPatches_AppendsToEmptyConfig(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{}

	kind.ApplyCDIPatches(kindConfig)

	assert.Equal(t, []string{kind.CDIPatch}, kindConfig.ContainerdConfigPatches)
}

func TestApplyCDIPatches_AppendsAlongsideUnrelatedPatch(t *testing.T) {
	t.Parallel()

	const unrelated = `[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"`

	kindConfig := &kindv1alpha4.Cluster{
		ContainerdConfigPatches: []string{unrelated},
	}

	kind.ApplyCDIPatches(kindConfig)

	assert.Equal(t, []string{unrelated, kind.CDIPatch}, kindConfig.ContainerdConfigPatches)
}

func TestApplyCDIPatches_IsIdempotent(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{}

	kind.ApplyCDIPatches(kindConfig)
	kind.ApplyCDIPatches(kindConfig)

	assert.Len(t, kindConfig.ContainerdConfigPatches, 1)
	assert.Equal(t, []string{kind.CDIPatch}, kindConfig.ContainerdConfigPatches)
}

func TestApplyCDIPatches_SkipsWhenEnableCDIAlreadyPresent(t *testing.T) {
	t.Parallel()

	// Any existing patch containing the "enable_cdi" marker should suppress
	// appending, even if it is not the exact CDIPatch string.
	existing := []string{"some other patch with enable_cdi already set"}
	kindConfig := &kindv1alpha4.Cluster{ContainerdConfigPatches: existing}

	kind.ApplyCDIPatches(kindConfig)

	assert.Equal(t, existing, kindConfig.ContainerdConfigPatches)
}
