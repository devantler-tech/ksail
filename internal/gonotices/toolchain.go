package gonotices

import (
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

// Toolchain identifies actual compiler material independently of Go modules.
type Toolchain struct {
	Version, Directory, LicenseFile string
	Licenses                        []string
	Packages                        []ToolchainPackage
}

// ToolchainPackage is a selected standard-library source file set from go list.
type ToolchainPackage struct {
	ImportPath, Directory              string
	GoFiles, AssemblyFiles, EmbedFiles []string
	OtherFiles                         []string
}

type toolchainRecord struct {
	Version       string     `json:"version"`
	LicenseSHA256 string     `json:"licenseSha256"`
	Licenses      []string   `json:"licenses"`
	Packages      []string   `json:"packages"`
	Materials     []material `json:"materials"`
	Files         []string   `json:"files"`
}

func (collector *Collector) collectToolchain(
	selected Toolchain,
) (toolchainRecord, map[string][]byte, error) {
	result, contents, err := initialToolchainEvidence(selected)
	if err != nil {
		return toolchainRecord{}, nil, err
	}

	files := map[string]string{}

	seen := map[string]bool{}
	for _, pkg := range selected.Packages {
		if pkg.ImportPath == "" || seen[pkg.ImportPath] {
			return toolchainRecord{}, nil, ErrIncomplete
		}

		seen[pkg.ImportPath] = true

		selectedFiles, fileErr := collectToolchainPackageFiles(selected.Directory, pkg, files)
		if fileErr != nil {
			return toolchainRecord{}, nil, fileErr
		}

		result.Packages = append(result.Packages, pkg.ImportPath)
		result.Files = append(result.Files, selectedFiles...)
	}

	err = collector.appendToolchainMaterials(selected, &result, files, contents)
	if err != nil {
		return toolchainRecord{}, nil, err
	}

	slices.Sort(result.Packages)
	slices.Sort(result.Files)

	return result, contents, nil
}

func initialToolchainEvidence(selected Toolchain) (toolchainRecord, map[string][]byte, error) {
	if !validToolchainSelection(selected) {
		return toolchainRecord{}, nil, ErrIncomplete
	}

	version, err := readMaterial(selected.Directory, "VERSION")
	if err != nil ||
		strings.SplitN(string(version), "\n", keyedToolchainValues)[0] != selected.Version {
		return toolchainRecord{}, nil, ErrIncomplete
	}

	license, label, err := toolchainLicense(selected)
	if err != nil || !publicMaterial(license) {
		return toolchainRecord{}, nil, ErrIncomplete
	}

	result := toolchainRecord{
		Version:       selected.Version,
		LicenseSHA256: digest(license),
		Licenses:      slices.Clone(selected.Licenses),
		Materials: []material{
			{
				Source: "go-toolchain:" + selected.Version,
				Path:   label,
				Kind:   "toolchain-license",
				SHA256: digest(license),
			},
		},
	}

	return result, map[string][]byte{digest(license): license}, nil
}

func validToolchainSelection(selected Toolchain) bool {
	return strings.HasPrefix(selected.Version, "go") &&
		semver.IsValid("v"+strings.TrimPrefix(selected.Version, "go")) &&
		len(selected.Licenses) != 0 &&
		!slices.Contains(selected.Licenses, "Unknown") &&
		len(selected.Packages) != 0
}

func collectToolchainPackageFiles(
	directory string,
	pkg ToolchainPackage,
	files map[string]string,
) ([]string, error) {
	relative, err := scopedRelative(directory, pkg.Directory)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(relative), "src/") {
		return nil, ErrIncomplete
	}

	err = collectNoticeAncestors(directory, relative, files)
	if err != nil {
		return nil, err
	}

	selected, err := collectHeaderFileGroups(
		relative,
		[]sourceFileGroup{
			{pkg.GoFiles, copyrightHeaderKind},
			{pkg.AssemblyFiles, assemblyHeaderKind},
			{pkg.OtherFiles, compiledHeaderKind},
		},
		files,
	)
	if err != nil {
		return nil, err
	}

	embedded, err := collectEmbeddedNotices(directory, relative, pkg.EmbedFiles, files)
	if err != nil {
		return nil, err
	}

	return append(selected, embedded...), nil
}

func (collector *Collector) appendToolchainMaterials(
	selected Toolchain,
	result *toolchainRecord,
	files map[string]string,
	contents map[string][]byte,
) error {
	identity := selected.Directory + "\x00" + selected.Version + "\x00" + result.LicenseSHA256
	primary, _ := scopedRelative(selected.Directory, selected.LicenseFile)

	for _, file := range sortedMaterialFiles(files) {
		data, err := collector.materialText(identity, selected.Directory, file)
		if err != nil || !publicMaterial(data) {
			return ErrIncomplete
		}

		if len(data) == 0 {
			continue
		}

		if file.path == filepath.ToSlash(primary) {
			if digest(data) != result.LicenseSHA256 {
				return ErrIncomplete
			}

			continue
		}

		result.Materials = append(
			result.Materials,
			material{
				Source: "go-toolchain:" + selected.Version,
				Path:   file.path,
				Kind:   file.kind,
				SHA256: digest(data),
			},
		)
		contents[digest(data)] = data
	}

	return nil
}

func toolchainLicense(selected Toolchain) ([]byte, string, error) {
	relative, err := scopedRelative(selected.Directory, selected.LicenseFile)
	if err == nil {
		data, readErr := readMaterial(selected.Directory, relative)

		return data, primaryLicenseFilename, readErr
	}
	// Homebrew moves Go's distribution LICENSE into the versioned installed
	// package while keeping VERSION and source under libexec.
	parent := filepath.Dir(selected.Directory)
	if filepath.Base(selected.Directory) != "libexec" ||
		filepath.Base(parent) != strings.TrimPrefix(selected.Version, "go") ||
		filepath.Clean(selected.LicenseFile) != filepath.Join(parent, primaryLicenseFilename) {
		return nil, "", ErrIncomplete
	}

	data, readErr := readMaterial(parent, primaryLicenseFilename)

	return data, primaryLicenseFilename, readErr
}
