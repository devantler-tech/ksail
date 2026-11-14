package gonotices

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	repositoryReplacementKind = "repository"
	stagedMemberCount         = 3
	stagedMemberProbeLimit    = stagedMemberCount + 1
)

// Prepare validates checked-in recipient data against actual local build inputs.
// The release caller separately binds the checkout and payload to the admitted
// public source commit before staging or comparing published artifacts.
func Prepare(root string, release Release, actual Toolchain) (Payload, error) {
	if !filepath.IsAbs(root) || !validRelease(release) {
		return Payload{}, ErrIncomplete
	}

	payload, err := storedPayload(root, release.Target)
	if err != nil {
		return Payload{}, err
	}

	metadata, err := validatePayload(release.Target, payload)
	if err != nil {
		return Payload{}, err
	}

	_, err = Binding(release, payload)
	if err != nil {
		return Payload{}, err
	}

	err = matchReceipt(root, release.Target, payload)
	if err != nil {
		return Payload{}, err
	}

	err = verifyBuildInputs(root, metadata, actual)
	if err != nil {
		return Payload{}, err
	}

	return payload, nil
}

func storedPayload(root, target string) (Payload, error) {
	directory := "notices/go/" + target + "/"

	text, err := readRecipientFile(root, directory+noticesFilename)
	if err != nil {
		return Payload{}, err
	}

	data, err := readRecipientFile(root, directory+manifestFilename)
	if err != nil {
		return Payload{}, err
	}

	return Payload{Text: text, Manifest: data}, nil
}

func verifyBuildInputs(root string, metadata manifest, actual Toolchain) error {
	for _, name := range sourceInputNames() {
		data, err := readMaterial(root, name)
		if err != nil || digest(data) != metadata.Inputs[name] {
			return ErrIncomplete
		}
	}

	err := verifyBuildToolchain(metadata.Toolchain, actual)
	if err != nil {
		return err
	}

	return verifyLocalReplacements(root, metadata.Components)
}

func verifyBuildToolchain(expected *toolchainRecord, actual Toolchain) error {
	if expected == nil || actual.Version != expected.Version {
		return ErrIncomplete
	}

	version, err := readMaterial(actual.Directory, "VERSION")
	if err != nil {
		return ErrIncomplete
	}

	if strings.SplitN(string(version), "\n", keyedToolchainValues)[0] != actual.Version {
		return ErrIncomplete
	}

	license, _, err := toolchainLicense(actual)
	if err != nil || digest(license) != expected.LicenseSHA256 {
		return ErrIncomplete
	}

	return nil
}

func verifyLocalReplacements(root string, records []component) error {
	verified := map[string]string{}

	for _, record := range records {
		if record.Replacement == nil || record.Replacement.Kind != repositoryReplacementKind {
			continue
		}

		key := record.Replacement.RepositoryRelative + "\x00" + record.Module + "@" + record.Version

		actualHash, exists := verified[key]
		if !exists {
			var err error

			actualHash, err = localReplacementHash(root, record)
			if err != nil {
				return err
			}

			verified[key] = actualHash
		}

		if actualHash != record.Replacement.ContentHash {
			return ErrIncomplete
		}
	}

	return nil
}

func localReplacementHash(root string, record component) (string, error) {
	path := record.Replacement.RepositoryRelative
	if !materialPath(path) || !strings.HasPrefix(path, "third_party/") ||
		!ordinaryDirectories(root, path) {
		return "", ErrIncomplete
	}

	value, err := repositoryContentHash(
		filepath.Join(root, filepath.FromSlash(path)),
		record.Module+"@"+record.Version,
	)
	if err != nil {
		return "", ErrIncomplete
	}

	return value, nil
}

// Stage writes only the fixed target directory beneath .cache/go-notices.
func Stage(root string, release Release, actual Toolchain) (string, error) {
	payload, err := Prepare(root, release, actual)
	if err != nil {
		return "", err
	}

	binding, err := Binding(release, payload)
	if err != nil {
		return "", err
	}

	output, err := os.OpenRoot(root)
	if err != nil {
		return "", ErrIncomplete
	}

	directory, stageErr := stagePayload(output, release.Target, payload, binding)
	if errors.Join(stageErr, output.Close()) != nil {
		return "", ErrIncomplete
	}

	return directory, nil
}

func stagePayload(output *os.Root, target string, payload Payload, binding []byte) (string, error) {
	directory := ".cache/go-notices/" + target + "/GoNotices"

	err := prepareStageDirectory(output, target, directory)
	if err != nil {
		return "", err
	}

	err = writePayloadFiles(output, directory, payload, binding)
	if err != nil {
		return "", err
	}

	return directory, nil
}

func prepareStageDirectory(root *os.Root, target, directory string) error {
	for _, path := range []string{".cache", ".cache/go-notices", ".cache/go-notices/" + target, directory} {
		err := ensureRecipientDirectory(root, path)
		if err != nil {
			return err
		}
	}

	return verifyStageMembers(root, directory)
}

func verifyStageMembers(root *os.Root, directory string) error {
	entries, err := root.Open(directory)
	if err != nil {
		return ErrIncomplete
	}

	children, readErr := entries.ReadDir(stagedMemberProbeLimit)

	closeErr := entries.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil ||
		len(children) > stagedMemberCount {
		return ErrIncomplete
	}

	for _, entry := range children {
		if !entry.Type().IsRegular() || !knownRecipientMember(entry.Name()) {
			return ErrIncomplete
		}
	}

	return nil
}

func knownRecipientMember(name string) bool {
	return name == noticesFilename || name == manifestFilename || name == bindingFilename
}
