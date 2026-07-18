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

// ErrSourceConfigMissing is returned by CloneEnvironmentConfig when
// repoRoot/srcConfigRel does not exist or is not a regular file.
var ErrSourceConfigMissing = errors.New("source environment config not found")

// ErrDestinationEscapesRepository is returned when a clone destination would
// write outside repoRoot, including through an existing symlinked parent or
// destination file.
var ErrDestinationEscapesRepository = errors.New("destination escapes repository root")

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
// It returns the repo-relative paths it actually wrote — a destination skipped
// because it already exists (force is false) is excluded — in deterministic
// (lexical) walk order. Source and destination paths are containment-checked
// against repoRoot, so a srcRelDir or rewritten path with ".." segments or a
// symlink escape is rejected (ErrSourceOverlayMissing / ErrPathOutsideBase) rather
// than reading or writing outside the repository.
func CloneOverlay(repoRoot, srcRelDir string, rewrites []Rewrite, force bool) ([]string, error) {
	srcAbs := filepath.Join(repoRoot, srcRelDir)

	// Containment guard: a srcRelDir with ".." segments or a symlinked overlay must
	// not let the clone read from outside repoRoot.
	if !fsutil.IsPathWithinDirectory(srcAbs, repoRoot) {
		return nil, fmt.Errorf("%w: %s escapes the repository root",
			ErrSourceOverlayMissing, srcRelDir)
	}

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

		wrote, writeErr := writeClone(repoRoot, newRelPath, content, force)
		if writeErr != nil {
			return writeErr
		}

		if wrote {
			written = append(written, newRelPath)
		}

		return nil
	}

	err = filepath.WalkDir(srcAbs, walk)
	if err != nil {
		return nil, fmt.Errorf("cloning overlay %s: %w", srcRelDir, err)
	}

	return written, nil
}

// CloneEnvironmentConfig clones a single root environment config file (e.g.
// ksail.<src>.yaml) into the destination environment's config (ksail.<dst>.yaml).
// Pass the rewrites from [DeriveConfigRewrites] (not [DeriveRewrites]): the root
// config carries the environment identity in its top-level `name:` field, so the
// config-specific derivation repoints `name` and `provider` field values and swaps
// any clusters/<env> path or content reference, while every other byte (the
// connection context, version pins, comments, the rest of the spec) is preserved.
// The destination filename is derived from the rewrites' PathSegment
// (ksail.<src>.yaml -> ksail.<dst>.yaml), so the source and destination need not be
// named by convention and the caller does not repeat the naming logic the
// foundation already owns.
//
// It mirrors CloneOverlay's write semantics: the source and destination are
// containment-checked against repoRoot, an existing destination is preserved unless
// force is set, and parent directories are created as needed. It returns the
// destination's repo-relative path and whether a file was actually written (false
// when an existing destination was skipped because force is unset). A SOPS-encrypted
// *.enc.yaml source keeps its ciphertext verbatim and only has its path repointed,
// matching cloneFile.
func CloneEnvironmentConfig(
	repoRoot, srcConfigRel string,
	rewrites []Rewrite,
	force bool,
) (string, bool, error) {
	srcConfigRel = filepath.ToSlash(srcConfigRel)
	srcAbs := filepath.Join(repoRoot, filepath.FromSlash(srcConfigRel))

	// Containment guard: a srcConfigRel with ".." segments or a symlink escape must
	// not let the clone read from outside repoRoot.
	if !fsutil.IsPathWithinDirectory(srcAbs, repoRoot) {
		return "", false, fmt.Errorf("%w: %s escapes the repository root",
			ErrSourceConfigMissing, srcConfigRel)
	}

	info, err := os.Stat(srcAbs)
	if err != nil || !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("%w: %s", ErrSourceConfigMissing, srcConfigRel)
	}

	newRelPath, content, err := cloneFile(repoRoot, repoRoot, srcAbs, rewrites)
	if err != nil {
		return "", false, err
	}

	wrote, err := writeClone(repoRoot, newRelPath, content, force)
	if err != nil {
		return "", false, err
	}

	return newRelPath, wrote, nil
}

// writeClone writes one cloned file's content to repoRoot/newRelPath, returning
// whether a file was actually written. The destination is containment-checked
// against repoRoot, and an existing file is preserved (wrote=false) unless force
// is set — mirroring fsutil.TryWriteFile's skip semantics so the caller's
// "paths written" list excludes skipped files.
func writeClone(repoRoot, newRelPath, content string, force bool) (bool, error) {
	dest := filepath.Join(repoRoot, filepath.FromSlash(newRelPath))

	if err := validateCloneDestination(repoRoot, dest, newRelPath); err != nil {
		return false, err
	}

	// TryWriteFile returns the content (not a write-status), so decide up front
	// whether it will skip: it skips iff the file exists and force is false.
	if !force {
		_, statErr := os.Stat(dest)
		if statErr == nil {
			return false, nil
		}

		if !errors.Is(statErr, os.ErrNotExist) {
			return false, fmt.Errorf("checking destination %s: %w", newRelPath, statErr)
		}
	}

	if err := validateCloneDestination(repoRoot, dest, newRelPath); err != nil {
		return false, err
	}

	_, writeErr := fsutil.TryWriteFile(content, dest, force)
	if writeErr != nil {
		return false, fmt.Errorf("writing %s: %w", newRelPath, writeErr)
	}

	return true, nil
}

func validateCloneDestination(repoRoot, dest, displayPath string) error {
	rel, relErr := filepath.Rel(repoRoot, dest)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("destination %s escapes the repository root: %w",
			displayPath, ErrDestinationEscapesRepository)
	}

	if err := rejectSymlinkPath(repoRoot, dest); err != nil {
		return fmt.Errorf("destination %s escapes the repository root: %w",
			displayPath, err)
	}

	return nil
}

func rejectSymlinkPath(repoRoot, dest string) error {
	rel, err := filepath.Rel(repoRoot, dest)
	if err != nil {
		return ErrDestinationEscapesRepository
	}

	current := repoRoot
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}

		current = filepath.Join(current, part)
		info, lstatErr := os.Lstat(current)
		if errors.Is(lstatErr, os.ErrNotExist) {
			return nil
		}
		if lstatErr != nil {
			return lstatErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrDestinationEscapesRepository
		}
	}

	return nil
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
