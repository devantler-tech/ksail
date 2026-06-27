package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
)

func TestAPIServerFeatureGatesPatch(t *testing.T) {
	t.Parallel()

	patch := talos.APIServerFeatureGatesPatch()

	expectedContent := "cluster:\n" +
		"  apiServer:\n" +
		"    extraArgs:\n" +
		"      feature-gates: MutatingAdmissionPolicy=true\n" +
		"      runtime-config: admissionregistration.k8s.io/v1beta1=true\n"

	assert.Equal(t, "ksail-apiserver-feature-gates", patch.Path)
	assert.Equal(t, talos.PatchScopeCluster, patch.Scope)
	assert.Equal(t, []byte(expectedContent), patch.Content)
}
