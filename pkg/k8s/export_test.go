package k8s

import (
	"context"
	"os"

	"k8s.io/client-go/kubernetes"
)

// ForgetContextWithWriteForTest injects a failed atomic write without filesystem-specific permissions.
func ForgetContextWithWriteForTest(
	ctx context.Context,
	path, contextName string,
	write func(string, []byte, os.FileMode) error,
) (bool, error) {
	return forgetContextWithWrite(ctx, path, contextName, write)
}

// ReplaceUnchangedKubeconfigForTest exercises observed content and identity drift.
func ReplaceUnchangedKubeconfigForTest(
	path string,
	previousInfo os.FileInfo,
	previous, replacement []byte,
	write func(string, []byte, os.FileMode) error,
) error {
	return replaceUnchangedKubeconfig(path, previousInfo, previous, replacement, write)
}

// WaitForAuthorizedReadForTest exposes waitForAuthorizedRead for unit testing
// with a fake clientset, avoiding the need for a real API server.
func WaitForAuthorizedReadForTest(ctx context.Context, clientset kubernetes.Interface) error {
	return waitForAuthorizedRead(ctx, clientset)
}
