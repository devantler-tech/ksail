package eksinventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/pathutil"
)

//nolint:cyclop // Each filesystem and decoding invariant fails independently.
func readPrivateEvidence(path string, target any) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid private evidence path: %w", ErrInventory)
	}

	parent, err := pathutil.EvalCanonicalPath(filepath.Dir(absolute))
	if err != nil {
		return fmt.Errorf("invalid private evidence parent: %w", ErrInventory)
	}

	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open private evidence parent: %w", ErrInventory)
	}
	defer func() { _ = root.Close() }()

	parentInfo, err := root.Stat(".")
	if err != nil || !privateBaselineParent(parentInfo) {
		return fmt.Errorf("private evidence parent is not owner-only: %w", ErrInventory)
	}

	name := filepath.Base(absolute)

	info, err := root.Lstat(name)
	if err != nil || !privateEvidenceFile(info) || info.Size() <= 0 ||
		info.Size() > maxBaselineBytes {
		return fmt.Errorf("private evidence file is invalid: %w", ErrInventory)
	}

	file, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("open private evidence file: %w", ErrInventory)
	}
	defer func() { _ = file.Close() }()

	decoder := json.NewDecoder(io.LimitReader(file, maxBaselineBytes+1))

	err = decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("decode private evidence: %w", ErrInventory)
	}

	var trailing any

	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("private evidence has trailing data: %w", ErrInventory)
	}

	return nil
}

//nolint:cyclop // Atomic create and replacement deliberately take distinct guarded paths.
func publishOwnership(path string, data []byte, replace bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid ownership destination: %w", ErrInventory)
	}

	parent, err := pathutil.EvalCanonicalPath(filepath.Dir(absolute))
	if err != nil {
		return fmt.Errorf("invalid ownership parent: %w", ErrInventory)
	}

	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open ownership parent: %w", ErrInventory)
	}
	defer func() { _ = root.Close() }()

	parentInfo, err := root.Stat(".")
	if err != nil || !privateBaselineParent(parentInfo) {
		return fmt.Errorf("ownership parent must be private: %w", ErrInventory)
	}

	name := filepath.Base(absolute)
	if replace {
		info, statErr := root.Lstat(name)
		if statErr != nil || !privateEvidenceFile(info) {
			return fmt.Errorf("existing ownership evidence changed: %w", ErrInventory)
		}
	} else {
		_, statErr := root.Lstat(name)
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("ownership destination must be new: %w", ErrInventory)
		}
	}

	temporary, err := writeTemporaryPrivateEvidence(root, ".eks-ownership-", data)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()

	if replace {
		err = root.Rename(temporary, name)
	} else {
		err = root.Link(temporary, name)
	}

	if err != nil {
		return fmt.Errorf("publish private ownership evidence: %w", ErrInventory)
	}

	return nil
}
