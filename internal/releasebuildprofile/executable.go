package releasebuildprofile

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"io"
	"io/fs"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

// InspectOutputs checks all target/toolchain/VCS settings and executable hashes.
// -trimpath omits -ldflags from build info. Injected metadata is instead bound
// by the audited configuration, the native Linux version and cross-arm hashes.
func InspectOutputs(directory string) ([]Output, error) {
	canonical, err := fsutil.EvalCanonicalPath(directory)
	if err != nil {
		return nil, errInvalid
	}

	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, errInvalid
	}
	defer func() { _ = root.Close() }()

	var outputs []Output

	err = fs.WalkDir(root.FS(), ".", func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errInvalid
		}

		if entry.Name() != "ksail" && entry.Name() != "ksail.exe" {
			return nil
		}

		if !entry.Type().IsRegular() {
			return errInvalid
		}

		output, inspectErr := inspectOutput(root, filename)
		if inspectErr != nil {
			return inspectErr
		}

		outputs = append(outputs, output)

		return nil
	})
	if err != nil || !validOutputs(outputs) {
		return nil, errInvalid
	}

	return outputs, nil
}

func inspectOutput(root *os.Root, filename string) (Output, error) {
	file, err := root.Open(filename)
	if err != nil {
		return Output{}, errInvalid
	}
	defer func() { _ = file.Close() }()

	info, err := buildinfo.Read(file)
	if err != nil || info.GoVersion != GoVersion {
		return Output{}, errInvalid
	}

	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	if !validBuildSettings(settings) {
		return Output{}, errInvalid
	}

	digest := sha256.New()

	_, err = io.Copy(digest, file)
	if err != nil {
		return Output{}, errInvalid
	}

	modified := false

	return Output{
		Target:      settings["GOOS"] + "/" + settings["GOARCH"],
		SHA256:      hex.EncodeToString(digest.Sum(nil)),
		GoVersion:   info.GoVersion,
		Commit:      settings["vcs.revision"],
		VCSModified: &modified,
	}, nil
}

func validBuildSettings(settings map[string]string) bool {
	return settings["vcs"] == "git" && settings["vcs.revision"] == SourceSHA &&
		settings["vcs.modified"] == "false" && settings["CGO_ENABLED"] == "0" && settings["-trimpath"] == "true"
}
