package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/gofrs/flock"
)

const eksLockRetryInterval = 25 * time.Millisecond

// WithEKSLifecycleLock serializes an EKS lifecycle transition across processes using this user's
// state store. The name-scoped lock covers region/ownership resolution as well as mutations:
// spec.json and the local API are keyed by name, so account or region selectors cannot safely
// partition access to that shared state. This lock does not replace immutable AWS identity checks.
//
// Lock files live outside the removable cluster directory and are never unlinked. The operating
// system releases ownership on process exit; an existing file does not indicate a stale lock.
func WithEKSLifecycleLock(
	ctx context.Context,
	clusterName string,
	action func() error,
) (result error) {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("acquire EKS lifecycle lock: %w", err)
	}

	path, err := eksLifecycleLockPath(clusterName)
	if err != nil {
		return err
	}

	lock := flock.New(path, flock.SetPermissions(filePermissions))

	defer func() {
		closeErr := lock.Close()
		if closeErr != nil {
			result = errors.Join(result, fmt.Errorf("release EKS lifecycle lock: %w", closeErr))
		}
	}()

	_, err = lock.TryLockContext(ctx, eksLockRetryInterval)
	if err != nil {
		return fmt.Errorf("wait for EKS lifecycle operation on %q: %w", clusterName, err)
	}

	// Cancellation can race the successful OS lock attempt. Do not enter the transition after it.
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("enter EKS lifecycle operation: %w", err)
	}

	return action()
}

func eksLifecycleLockPath(clusterName string) (string, error) {
	if strings.TrimSpace(clusterName) == "" || clusterName == "." {
		return "", ErrInvalidClusterName
	}

	clusterDir, err := clusterStateDir(clusterName)
	if err != nil {
		return "", err
	}

	dir := filepath.Join(filepath.Dir(filepath.Dir(clusterDir)), "locks", "eks")

	err = os.MkdirAll(dir, dirPermissions)
	if err != nil {
		return "", fmt.Errorf("create EKS lifecycle lock directory: %w", err)
	}

	path, err := fsutil.EvalCanonicalPath(filepath.Join(dir, clusterName+".lock"))
	if err != nil {
		return "", fmt.Errorf("resolve EKS lifecycle lock path: %w", err)
	}

	return path, nil
}
