//nolint:testpackage // Test needs package access for internal helpers.
package mirrorregistry

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
)

// dockerRegistryInfo is a type alias for test usage to avoid importing
// the docker client package type name in assertions.
type dockerRegistryInfo = dockerclient.RegistryInfo

// stubDeps creates lifecycle Deps with a timer suitable for tests.
func stubDeps() lifecycle.Deps {
	return lifecycle.Deps{Timer: timer.New()}
}
