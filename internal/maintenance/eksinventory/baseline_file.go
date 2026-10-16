package eksinventory

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/pathutil"
)

const baselineFileMode = 0o600

func baselineDestination(destination string) (*os.Root, string, error) {
	if destination == "" {
		return nil, "", fmt.Errorf("missing private baseline destination: %w", ErrInventory)
	}

	absolute, err := filepath.Abs(destination)
	if err != nil {
		return nil, "", fmt.Errorf("invalid baseline destination: %w", ErrInventory)
	}

	parent, err := pathutil.EvalCanonicalPath(filepath.Dir(absolute))
	if err != nil {
		return nil, "", fmt.Errorf("invalid baseline parent: %w", ErrInventory)
	}

	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", fmt.Errorf("open baseline parent: %w", ErrInventory)
	}

	info, err := root.Stat(".")
	if err != nil || !privateBaselineParent(info) {
		_ = root.Close()

		return nil, "", fmt.Errorf(
			"baseline parent must be a private owned directory: %w",
			ErrInventory,
		)
	}

	name := filepath.Base(absolute)

	_, err = root.Lstat(name)
	if !os.IsNotExist(err) {
		_ = root.Close()

		return nil, "", fmt.Errorf("baseline destination must be new: %w", ErrInventory)
	}

	return root, name, nil
}

func publishBaseline(root *os.Root, name string, data []byte) error {
	temporary, err := writeTemporaryPrivateEvidence(root, ".eks-baseline-", data)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()

	// Link publishes the complete file atomically and refuses an existing target,
	// including a symlink created while the AWS reads were in progress.
	err = root.Link(temporary, name)
	if err != nil {
		return fmt.Errorf("publish new private baseline: %w", ErrInventory)
	}

	return nil
}

func writeTemporaryPrivateEvidence(root *os.Root, prefix string, data []byte) (string, error) {
	temporary := prefix + rand.Text()

	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, baselineFileMode)
	if err != nil {
		return "", fmt.Errorf("create private evidence: %w", ErrInventory)
	}

	_, writeErr := file.Write(data)
	syncErr := file.Sync()

	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = root.Remove(temporary)

		return "", fmt.Errorf("write private evidence: %w", ErrInventory)
	}

	return temporary, nil
}
