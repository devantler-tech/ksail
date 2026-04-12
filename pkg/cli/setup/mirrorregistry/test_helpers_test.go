package mirrorregistry

import (
	"time"

	"github.com/devantler-tech/ksail/v6/pkg/cli/lifecycle"
	dockerclient "github.com/devantler-tech/ksail/v6/pkg/client/docker"
)

// dockerRegistryInfo is a type alias for test usage to avoid importing
// the docker client package type name in assertions.
type dockerRegistryInfo = dockerclient.RegistryInfo

// stubTimer implements timer.Timer for tests.
type stubTimer struct{}

func (s *stubTimer) Start()                                    {}
func (s *stubTimer) NewStage()                                 {}
func (s *stubTimer) Stop()                                     {}
func (s *stubTimer) GetTiming() (time.Duration, time.Duration) { return 0, 0 }

// stubDeps creates lifecycle Deps with a no-op timer for tests.
func stubDeps() lifecycle.Deps {
	return lifecycle.Deps{Timer: &stubTimer{}}
}
