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
var fatalMu sync.Mutex //nolint:gochecknoglobals // required: kubectl's BehaviorOnFatal is a process-global

// kubectlFatalError wraps errors from kubectl's CheckErr/BehaviorOnFatal
// that would normally cause os.Exit.
type kubectlFatalError struct {
	msg  string
	code int
}

func (e *kubectlFatalError) Error() string {
	return e.msg
}

// withSafeFatal runs fn with kubectl's BehaviorOnFatal overridden to panic
// instead of calling os.Exit. The panic is recovered and returned as an error.
// A package-level mutex serializes all BehaviorOnFatal overrides since kubectl's
// fatal handler is a global. This is safe to call from multiple goroutines.
func withSafeFatal(action func()) (retErr error) {
	fatalMu.Lock()

	cmdutil.BehaviorOnFatal(func(msg string, code int) {
		panic(&kubectlFatalError{msg: msg, code: code})
	})

	defer func() {
		cmdutil.DefaultBehaviorOnFatal()

		if recovered := recover(); recovered != nil {
			if fatalErr, ok := recovered.(*kubectlFatalError); ok {
				retErr = fmt.Errorf("%w", fatalErr)
			} else {
				// Unlock before re-panicking to avoid deadlock.
				fatalMu.Unlock()
				panic(recovered)
			}
		}

		fatalMu.Unlock()
	}()

	action()

	return nil
}

// ExecuteSafely runs a kubectl cobra command without allowing os.Exit.
//
// kubectl commands use cmdutil.CheckErr in their Run handlers, which calls
// os.Exit(1) on any error. This function overrides that behavior using
// cmdutil.BehaviorOnFatal so errors are returned instead of terminating the
// process. This is essential when KSail calls kubectl commands internally
// (backup, restore, watch) and needs to handle errors gracefully.
func ExecuteSafely(ctx context.Context, cmd *cobra.Command) error {
	var execErr error

	fatalErr := withSafeFatal(func() {
		execErr = cmd.ExecuteContext(ctx)
	})
	if fatalErr != nil {
		return fatalErr
	}

	if execErr != nil {
		return fmt.Errorf("kubectl command failed: %w", execErr)
	}

	return nil
}
