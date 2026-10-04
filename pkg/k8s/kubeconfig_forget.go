package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ErrExplicitConnectionRequired indicates an attempt to forget an implicit local connection.
var ErrExplicitConnectionRequired = errors.New("an explicit kubeconfig file and exact context name are required")

// ErrKubeconfigChanged refuses to replace a connection file changed since it was read.
var ErrKubeconfigChanged = errors.New("kubeconfig changed during recovery; retry after the other writer finishes")

// ForgetContext removes exactly one local context and its unreferenced connection records.
// It never loads a REST client, executes credential plugins, or infers resource ownership.
// Missing contexts are a no-op; missing or unreadable files remain errors. The selected file
// is replaced atomically, with private permissions, only after a successful parse.
func ForgetContext(ctx context.Context, path, contextName string) (bool, error) {
	return forgetContextWithWrite(ctx, path, contextName, fsutil.AtomicWriteFile)
}

func forgetContextWithWrite(
	ctx context.Context,
	path, contextName string,
	write func(string, []byte, os.FileMode) error,
) (bool, error) {
	if path == "" || contextName == "" {
		return false, ErrExplicitConnectionRequired
	}

	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("forget local context: %w", err)
	}

	expanded, err := fsutil.ExpandHomePath(path)
	if err != nil {
		return false, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	canonical, err := fsutil.EvalCanonicalPath(expanded)
	if err != nil {
		return false, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	// Match client-go's ModifyConfig protocol. Hold the canonical-path lock
	// through reading and replacement; never steal another writer's lock.
	lockPath := canonical + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, kubeconfigFileMode) //nolint:gosec // canonical-path exclusive lock.
	if err != nil {
		return false, fmt.Errorf("lock kubeconfig; wait for the other writer to finish: %w", err)
	}
	defer func() { _ = os.Remove(lockPath) }()

	if err := lock.Close(); err != nil {
		return false, fmt.Errorf("close kubeconfig lock: %w", err)
	}

	return forgetLockedContext(ctx, canonical, contextName, write)
}

func forgetLockedContext(
	ctx context.Context,
	canonical, contextName string,
	write func(string, []byte, os.FileMode) error,
) (bool, error) {
	info, err := os.Stat(canonical)
	if err != nil {
		return false, fmt.Errorf("stat kubeconfig: %w", err)
	}

	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("kubeconfig must be a regular file: %w", os.ErrInvalid)
	}

	data, err := os.ReadFile(canonical) //nolint:gosec // explicit user path is canonicalized above.
	if err != nil {
		return false, fmt.Errorf("read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(data)
	if err != nil {
		return false, fmt.Errorf("parse kubeconfig: %w", err)
	}

	selected, exists := config.Contexts[contextName]
	if !exists {
		return false, nil
	}

	delete(config.Contexts, contextName)
	removeUnreferencedConnection(config, selected)

	if config.CurrentContext == contextName {
		config.CurrentContext = ""
	}

	result, err := clientcmd.Write(*config)
	if err != nil {
		return false, fmt.Errorf("encode kubeconfig: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("forget local context: %w", err)
	}

	err = replaceUnchangedKubeconfig(canonical, info, data, result, write)
	if err != nil {
		return false, fmt.Errorf("save kubeconfig: %w", err)
	}

	return true, nil
}

// replaceUnchangedKubeconfig supplements the cooperative lock with a drift check.
// Writers ignoring that protocol cannot be serialized by this check alone.
func replaceUnchangedKubeconfig(
	path string,
	previousInfo os.FileInfo,
	previous, replacement []byte,
	write func(string, []byte, os.FileMode) error,
) error {
	currentInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("recheck kubeconfig: %w", err)
	}

	if currentInfo.Mode().Perm()&0o222 == 0 {
		return fmt.Errorf("refuse read-only kubeconfig: %w", os.ErrPermission)
	}

	if !os.SameFile(previousInfo, currentInfo) || previousInfo.Mode() != currentInfo.Mode() ||
		!previousInfo.ModTime().Equal(currentInfo.ModTime()) {
		return ErrKubeconfigChanged
	}

	current, err := os.ReadFile(path) //nolint:gosec // caller holds the canonical-path lock.
	if err != nil {
		return fmt.Errorf("recheck kubeconfig content: %w", err)
	}

	if !bytes.Equal(previous, current) {
		return ErrKubeconfigChanged
	}

	// A writable directory permits rename over a file whose ACL denies writes.
	// Check access without truncation before using atomic replacement.
	file, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // canonical path, no truncation.
	if err != nil {
		return fmt.Errorf("check kubeconfig write access: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close kubeconfig write check: %w", err)
	}

	return write(path, replacement, kubeconfigFileMode)
}

func removeUnreferencedConnection(config *clientcmdapi.Config, selected *clientcmdapi.Context) {
	if selected == nil {
		return
	}

	clusterUsed, authUsed := false, false
	for _, remaining := range config.Contexts {
		if remaining != nil {
			clusterUsed = clusterUsed || remaining.Cluster == selected.Cluster
			authUsed = authUsed || remaining.AuthInfo == selected.AuthInfo
		}
	}

	if !clusterUsed && selected.Cluster != "" {
		delete(config.Clusters, selected.Cluster)
	}

	if !authUsed && selected.AuthInfo != "" {
		delete(config.AuthInfos, selected.AuthInfo)
	}
}
