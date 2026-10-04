package k8s

import (
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

	err = write(canonical, result, kubeconfigFileMode)
	if err != nil {
		return false, fmt.Errorf("save kubeconfig: %w", err)
	}

	return true, nil
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
