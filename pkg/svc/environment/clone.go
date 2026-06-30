package environment

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// encryptedFileSuffix marks a SOPS-encrypted file whose ciphertext is cloned
// byte-for-byte. Only its path is repointed to the destination environment;
// re-encryption to that environment's recipients is out of scope for the clone
// (the eventual command calls it out in --help).
const encryptedFileSuffix = ".enc.yaml"

// ErrSourceOverlayMissing is returned by CloneOverlay when repoRoot/srcRelDir does
// not exist or is not a directory.
var ErrSourceOverlayMissing = errors.New("source overlay directory not found")

// CloneOverlay clones every file under repoRoot/srcRelDir into the destination
// overlay implied by rewrites, applying the structured path+content rewrites to
// each regular file and copying SOPS-encrypted *.enc.yaml files byte-for-byte
// (their path is still repointed). It reuses [RewriteOverlayFile] for both the
// per-file path and content rewrite, so the directory clone repoints
// cluster_name/provider values, the clusters/<env>/ segment and clusters/<env>
// content references exactly as the foundation already does — no second rewrite
// implementation to drift.
//
// Existing destination files are preserved unless force is set
// (fsutil.TryWriteFile semantics), and parent directories are created as needed.
// It returns the repo-relative paths written, in deterministic (lexical) walk
// order, and ErrSourceOverlayMissing if the source overlay does not exist.
func CloneOverlay(repoRoot, srcRelDir string, rewrites []Rewrite, force bool) ([]string, error) {
	srcAbs := filepath.Join(repoRoot, srcRelDir)

	info, err := os.Stat(srcAbs)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrSourceOverlayMissing, srcRelDir)
	}

	var written []string

	walk := func(absPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if dirEntry.IsDir() {
			return nil
		}

		newRelPath, content, cloneErr := cloneFile(repoRoot, srcAbs, absPath, rewrites)
		if cloneErr != nil {
			return cloneErr
		}

		dest := filepath.Join(repoRoot, filepath.FromSlash(newRelPath))

		_, writeErr := fsutil.TryWriteFile(content, dest, force)
		if writeErr != nil {
			return fmt.Errorf("writing %s: %w", newRelPath, writeErr)
		}

		written = append(written, newRelPath)

		return nil
	}

	err = filepath.WalkDir(srcAbs, walk)
	if err != nil {
		return nil, fmt.Errorf("cloning overlay %s: %w", srcRelDir, err)
	}

	return written, nil
}

// cloneFile reads one source file and returns its destination repo-relative path
// and the contents to write. A SOPS-encrypted *.enc.yaml file keeps its ciphertext
// verbatim and only has its path repointed; every other file is rewritten through
// [RewriteOverlayFile].
func cloneFile(repoRoot, srcAbs, absPath string, rewrites []Rewrite) (string, string, error) {
	raw, err := fsutil.ReadFileSafe(srcAbs, absPath)
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", absPath, err)
	}

	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", "", fmt.Errorf("resolving %s relative to repo root: %w", absPath, err)
	}

	// Normalise to forward slashes so the rewrites — which operate on '/'-delimited
	// path tokens and clusters/<env> references — behave identically on every OS.
	relPath = filepath.ToSlash(relPath)

	if strings.HasSuffix(relPath, encryptedFileSuffix) {
		// Path-only rewrite: an empty content argument keeps the content rewrite a
		// no-op so the ciphertext is preserved exactly, while newRelPath is still
		// repointed to the destination environment.
		newRelPath, _, pathErr := RewriteOverlayFile(relPath, "", rewrites)
		if pathErr != nil {
			return "", "", pathErr
		}

		return newRelPath, string(raw), nil
	}

	return RewriteOverlayFile(relPath, string(raw), rewrites)
}
