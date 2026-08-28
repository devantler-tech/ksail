package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconstants "github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/stretchr/testify/assert"
)

// TestDefaultKubernetesVersionFollowsTalosMachinery catches the copied-literal
// drift that can leave new clusters behind after Dependabot updates Talos.
func TestDefaultKubernetesVersionFollowsTalosMachinery(t *testing.T) {
	t.Parallel()

	assert.Equal(t, talosconstants.DefaultKubernetesVersion, talos.DefaultKubernetesVersion)
}
