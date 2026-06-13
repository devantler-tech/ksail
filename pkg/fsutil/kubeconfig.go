package fsutil

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	// kubeconfigFileMode is the file mode kubeconfig files are written with.
	kubeconfigFileMode = 0o600
	// kubeconfigDirMode is the directory mode for created kubeconfig parents.
	kubeconfigDirMode = 0o700
)

// KubeconfigUpdateOptions controls the prologue of UpdateKubeconfigFile.
//
// The zero value performs the most common ritual: canonicalize the path, load
// the file (or start from an empty config when it does not exist), then write
// back atomically. Each field toggles one deliberate per-caller deviation; the
// options exist to encode those differences explicitly rather than erase them.
type KubeconfigUpdateOptions struct {
	// ExpandHome runs ExpandHomePath on the path before canonicalization,
	// expanding a leading "~/" and making the path absolute.
	ExpandHome bool
	// MkdirParent creates the parent directory (mode 0o700) before
	// canonicalization. EvalCanonicalPath requires the parent to exist, so this
	// is needed when the kubeconfig may live in a directory that has not been
	// created yet.
	MkdirParent bool
	// RequireExists fails with the read error when the kubeconfig file does not
	// exist, instead of starting the mutation from an empty config. Use this for
	// mutations that are meaningless on a fresh file (e.g. adding entries that
	// reference a cluster the file is expected to already contain).
	RequireExists bool
	// SkipCanonicalize skips EvalCanonicalPath. Use only when the caller has
	// already canonicalized the path upstream; the path is then read and written
	// verbatim.
	SkipCanonicalize bool
}

// UpdateKubeconfigFile loads the kubeconfig at path, applies mutate, and writes
// the result back atomically with mode 0o600. The prologue (home expansion,
// parent-directory creation, canonicalization, and missing-file handling) is
// controlled by opts so that each caller's intentional differences are
// preserved explicitly.
//
// mutate receives the loaded *api.Config (never nil) and may return an error to
// abort the write; that error is returned to the caller unwrapped so callers
// keep their exact sentinels.
func UpdateKubeconfigFile(
	path string,
	mutate func(*api.Config) error,
	opts KubeconfigUpdateOptions,
) error {
	resolvedPath, err := resolveKubeconfigPath(path, opts)
	if err != nil {
		return err
	}

	config, err := loadKubeconfigForUpdate(resolvedPath, opts.RequireExists)
	if err != nil {
		return err
	}

	mutateErr := mutate(config)
	if mutateErr != nil {
		return mutateErr
	}

	result, err := clientcmd.Write(*config)
	if err != nil {
		return fmt.Errorf("serialize kubeconfig: %w", err)
	}

	err = AtomicWriteFile(resolvedPath, result, kubeconfigFileMode)
	if err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}

	return nil
}

// resolveKubeconfigPath applies the home-expand, parent-create, and
// canonicalize prologue steps selected by opts and returns the path to read
// and write.
func resolveKubeconfigPath(path string, opts KubeconfigUpdateOptions) (string, error) {
	if opts.ExpandHome {
		expanded, err := ExpandHomePath(path)
		if err != nil {
			return "", fmt.Errorf("expand kubeconfig path: %w", err)
		}

		path = expanded
	}

	if opts.MkdirParent {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			mkdirErr := os.MkdirAll(dir, kubeconfigDirMode)
			if mkdirErr != nil {
				return "", fmt.Errorf("create kubeconfig directory: %w", mkdirErr)
			}
		}
	}

	if opts.SkipCanonicalize {
		return path, nil
	}

	canonical, err := EvalCanonicalPath(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	return canonical, nil
}

// loadKubeconfigForUpdate reads and parses the kubeconfig at path. When the
// file is absent it returns an empty config, unless requireExists is set in
// which case the read error is returned.
func loadKubeconfigForUpdate(path string, requireExists bool) (*api.Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path validated by caller/prologue
	if err != nil {
		if !requireExists && os.IsNotExist(err) {
			return api.NewConfig(), nil
		}

		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	return config, nil
}
