package gonotices

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	maxPayloadBytes        = 64 << 20
	recipientDirectoryMode = 0o755
	recipientFileMode      = 0o644
	noticesFilename        = "THIRD_PARTY_NOTICES.txt"
	manifestFilename       = "manifest.json"
	bindingFilename        = "release.txt"
)

func sourceInputNames() []string {
	return []string{
		"go.mod",
		"go.sum",
		".github/go-license-policy.json",
		".github/go-notice-materials.json",
	}
}

// Write updates the repository-owned, generated recipient payload for one target.
func Write(directory, target string, payload Payload) (err error) {
	if !filepath.IsAbs(directory) {
		return ErrIncomplete
	}

	_, err = validatePayload(target, payload)
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return ErrIncomplete
	}
	defer func() { err = errors.Join(err, root.Close()) }()

	for _, relative := range []string{"notices", "notices/go", "notices/go/" + target} {
		err = ensureRecipientDirectory(root, relative)
		if err != nil {
			return err
		}
	}

	return writePayloadFiles(root, "notices/go/"+target, payload, nil)
}

func ensureRecipientDirectory(root *os.Root, relative string) error {
	err := root.Mkdir(relative, recipientDirectoryMode)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return ErrIncomplete
	}

	info, err := root.Lstat(relative)
	if err != nil || !info.IsDir() {
		return ErrIncomplete
	}

	return nil
}

func payloadFiles(payload Payload, binding []byte) map[string][]byte {
	files := map[string][]byte{noticesFilename: payload.Text, manifestFilename: payload.Manifest}
	if binding != nil {
		files[bindingFilename] = binding
	}

	return files
}

func writePayloadFiles(root *os.Root, directory string, payload Payload, binding []byte) error {
	for name, data := range payloadFiles(payload, binding) {
		err := writeRecipientFile(root, directory+"/"+name, data)
		if err != nil {
			return err
		}
	}

	return nil
}

func writeRecipientFile(root *os.Root, name string, data []byte) error {
	info, err := root.Lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return ErrIncomplete
	}

	temporary := filepath.Dir(name) + "/.notice-" + rand.Text() + ".tmp"

	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, recipientFileMode)
	if err != nil {
		return ErrIncomplete
	}

	count, writeErr := file.Write(data)
	if count != len(data) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}

	if errors.Join(writeErr, file.Close()) != nil {
		_ = root.Remove(temporary)

		return ErrIncomplete
	}

	err = root.Rename(temporary, name)
	if err != nil {
		_ = root.Remove(temporary)

		return ErrIncomplete
	}

	return nil
}

// Verify compares recipient files with independently derived source evidence.
func Verify(directory, target string, expected Payload) error {
	_, err := validatePayload(target, expected)
	if err != nil {
		return err
	}

	for name, want := range payloadFiles(expected, nil) {
		actual, readErr := readRecipientFile(directory, "notices/go/"+target+"/"+name)
		if readErr != nil || !bytes.Equal(actual, want) {
			return ErrIncomplete
		}
	}

	return nil
}

func readRecipientFile(directory, name string) ([]byte, error) {
	file, err := openMaterial(directory, name)
	if err != nil {
		return nil, err
	}

	contents, readErr := io.ReadAll(io.LimitReader(file, maxPayloadBytes+1))
	if errors.Join(readErr, file.Close()) != nil || len(contents) == 0 ||
		len(contents) > maxPayloadBytes {
		return nil, ErrIncomplete
	}

	return contents, nil
}

func safePayloadBytes(payload Payload) bool {
	return len(payload.Text) > 0 && len(payload.Manifest) > 0 &&
		len(payload.Text) <= maxPayloadBytes && len(payload.Manifest) <= maxPayloadBytes &&
		publicMaterial(payload.Text) &&
		publicMaterial(payload.Manifest)
}

func validatePayload(target string, payload Payload) (manifest, error) {
	if !validTarget(target) || !safePayloadBytes(payload) {
		return manifest{}, ErrIncomplete
	}

	decoded, err := decodeManifest(payload.Manifest)
	if err != nil {
		return manifest{}, ErrIncomplete
	}

	if decoded.Target != target || decoded.TextSHA256 != digest(payload.Text) ||
		len(decoded.Components) == 0 {
		return manifest{}, ErrIncomplete
	}

	if !validSourceInputHashes(decoded.Inputs) {
		return manifest{}, ErrIncomplete
	}

	return decoded, nil
}

func validSourceInputHashes(inputs map[string]string) bool {
	if len(inputs) != len(sourceInputNames()) {
		return false
	}

	for _, name := range sourceInputNames() {
		if !sha256Hex(inputs[name]) {
			return false
		}
	}

	return true
}
