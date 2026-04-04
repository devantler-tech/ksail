package kubectl

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

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
func ExecuteSafely(ctx context.Context, cmd *cobra.Command) (retErr error) {
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
