package localregistry_test

import (
	"bytes"
	"context"

	"github.com/devantler-tech/ksail/v6/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v6/pkg/timer"
	"github.com/spf13/cobra"
)

// stubLifecycleDeps creates lifecycle Deps with a timer suitable for tests.
func stubLifecycleDeps() lifecycle.Deps {
	return lifecycle.Deps{Timer: timer.New()}
}

// newTestCmd creates a new Cobra command with a buffer as output
// and a background context for testing.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetContext(context.Background())

	return cmd
}
