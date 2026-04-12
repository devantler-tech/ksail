package localregistry_test

import (
	"bytes"
	"context"
	"time"

	"github.com/devantler-tech/ksail/v6/pkg/cli/lifecycle"
	"github.com/spf13/cobra"
)

// stubTimer implements timer.Timer for tests.
type stubTimer struct{}

func (s *stubTimer) Start()                                    {}
func (s *stubTimer) NewStage()                                 {}
func (s *stubTimer) Stop()                                     {}
func (s *stubTimer) GetTiming() (time.Duration, time.Duration) { return 0, 0 }

// stubLifecycleDeps creates lifecycle Deps with a no-op timer.
func stubLifecycleDeps() lifecycle.Deps {
	return lifecycle.Deps{Timer: &stubTimer{}}
}

// newTestCmd creates a new Cobra command with a buffer as output
// and a background context for testing.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetContext(context.Background())

	return cmd
}
