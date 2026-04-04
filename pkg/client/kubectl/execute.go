package kubectl

import (
	"context"
	"fmt"
	"sync"

	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// fatalMu serializes all BehaviorOnFatal overrides. kubectl's fatal handler
// is a package-level global, so concurrent overrides would race. Every call
// site that touches BehaviorOnFatal must hold this lock.
var fatalMu sync.Mutex

// kubectlFatalError wraps errors from kubectl's CheckErr/BehaviorOnFatal
// that would normally cause os.Exit.
type kubectlFatalError struct {
	msg  string
	code int
}

func (e *kubectlFatalError) Error() string {
	return e.msg
}

// ExecuteSafely runs a kubectl cobra command without allowing os.Exit.
//
// kubectl commands use cmdutil.CheckErr in their Run handlers, which calls
// os.Exit(1) on any error. This function overrides that behavior using
// cmdutil.BehaviorOnFatal so errors are returned instead of terminating the
// process. This is essential when KSail calls kubectl commands internally
// (backup, restore, watch) and needs to handle errors gracefully.
//
// A package-level mutex serializes all BehaviorOnFatal overrides so this
// function is safe to call from multiple goroutines (e.g., concurrent
// backup exports via errgroup).
func ExecuteSafely(ctx context.Context, cmd *cobra.Command) (retErr error) {
	fatalMu.Lock()
	defer fatalMu.Unlock()

	cmdutil.BehaviorOnFatal(func(msg string, code int) {
		panic(&kubectlFatalError{msg: msg, code: code})
	})

	defer func() {
		cmdutil.DefaultBehaviorOnFatal()

		if r := recover(); r != nil {
			if e, ok := r.(*kubectlFatalError); ok {
				retErr = fmt.Errorf("%w", e)
			} else {
				// Re-panic for unexpected panics (bugs, nil pointers, etc.)
				panic(r)
			}
		}
	}()

	return cmd.ExecuteContext(ctx)
}
