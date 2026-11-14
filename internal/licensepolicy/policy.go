// Package licensepolicy applies reviewed license decisions to authenticated Go sources.
package licensepolicy

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/internal/moduleintegrity"
)

const maxEvidenceBytes = 1 << 20

// ErrUnapproved identifies a missing license decision or unauthenticated exception.
var ErrUnapproved = errors.New("dependency license is not approved")

// Module describes the selected source, rather than a requested dependency range.
type Module struct {
	Path, Version, Sum, Dir string
	Main, Replaced          bool
}

// Finding preserves one package's raw classifier result and source ownership.
type Finding struct {
	Package, PackageDir, LicenseFile string
	Module                           Module
	Detected                         []string
}

// Exception binds a reviewed disposition to exact packages and authenticated bytes.
type Exception struct {
	Packages         []string `json:"packages"`
	Module           string   `json:"module"`
	Version          string   `json:"version"`
	Sum              string   `json:"sum"`
	Evidence         string   `json:"evidence"`
	SHA256           string   `json:"sha256"`
	ReportedFile     string   `json:"reportedFile"`
	Detected         []string `json:"detected"`
	Effective        []string `json:"effective"`
	Rationale        string   `json:"rationale"`
	SourceURL        string   `json:"sourceUrl"`
	PermissionRecord string   `json:"permissionRecord,omitempty"`
}

// Policy keeps first-party ownership separate from third-party license grants.
type Policy struct {
	Allowed           []string    `json:"allowed"`
	FirstPartyModule  string      `json:"firstPartyModule"`
	FirstPartySHA256  string      `json:"firstPartySha256"`
	FirstPartyLicense string      `json:"firstPartyLicense"`
	Exceptions        []Exception `json:"exceptions"`
}

// Evaluate returns effective terms without changing the raw finding. Unknown
// licenses require an exact source-bound disposition; no prefix grants exist.
func Evaluate(policy Policy, root string, finding Finding) ([]string, error) {
	if !validOwnership(root, finding) {
		return nil, ErrUnapproved
	}

	if finding.Module.Main {
		return firstParty(policy, root, finding)
	}

	var matched *Exception

	for _, exception := range policy.Exceptions {
		if slices.Contains(exception.Packages, finding.Package) {
			if matched != nil {
				return nil, ErrUnapproved
			}

			copyOfException := exception
			matched = &copyOfException
		}
	}

	if matched != nil {
		return evaluateException(policy, *matched, finding)
	}

	if !allowed(policy, finding.Detected) {
		return nil, ErrUnapproved
	}

	return canonical(finding.Detected), nil
}

func validOwnership(root string, finding Finding) bool {
	if !validIdentity(root, finding) {
		return false
	}

	directory := finding.Module.Dir
	if !ordinaryDirectory(directory) || !ordinaryDirectory(finding.PackageDir) ||
		!within(directory, finding.PackageDir) {
		return false
	}

	return finding.LicenseFile == "" || within(directory, finding.LicenseFile)
}

func validIdentity(root string, finding Finding) bool {
	source := finding.Module
	if !filepath.IsAbs(root) || !filepath.IsAbs(source.Dir) {
		return false
	}

	if source.Path == "" || finding.Package == "" {
		return false
	}

	if !source.Main && source.Version == "" {
		return false
	}

	return finding.Package == source.Path || strings.HasPrefix(finding.Package, source.Path+"/")
}

func ordinaryDirectory(path string) bool {
	info, err := os.Lstat(path)

	return err == nil && info.IsDir()
}

func firstParty(policy Policy, root string, finding Finding) ([]string, error) {
	if !rootOwnership(policy, root, finding.Module) ||
		!fingerprint(root, "LICENSE", policy.FirstPartySHA256) {
		return nil, ErrUnapproved
	}

	if finding.LicenseFile != "" &&
		filepath.Clean(finding.LicenseFile) != filepath.Join(root, "LICENSE") {
		if !allowed(policy, finding.Detected) {
			return nil, ErrUnapproved
		}

		return canonical(finding.Detected), nil
	}

	if !inheritsRootLicense(root, finding.PackageDir) {
		return nil, ErrUnapproved
	}

	return []string{policy.FirstPartyLicense}, nil
}

func rootOwnership(policy Policy, root string, source Module) bool {
	return source.Path == policy.FirstPartyModule && !source.Replaced &&
		filepath.Clean(source.Dir) == filepath.Clean(root) && policy.FirstPartyLicense != ""
}

func inheritsRootLicense(root, directory string) bool {
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)

	resolvedDirectory, directoryErr := filepath.EvalSymlinks(directory)
	if rootErr != nil || directoryErr != nil || !within(resolvedRoot, resolvedDirectory) {
		return false
	}

	return walkLicenseParents(resolvedRoot, resolvedDirectory)
}

func walkLicenseParents(root, directory string) bool {
	for directory = filepath.Clean(directory); directory != filepath.Clean(root); directory = filepath.Dir(directory) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return false
		}

		for _, entry := range entries {
			if !entry.IsDir() && licenseFilename(entry.Name()) {
				return false
			}
		}
	}

	return true
}

func licenseFilename(name string) bool {
	name = strings.ToUpper(name)
	if strings.HasSuffix(name, ".GO") {
		return false
	}

	// Match the pinned scanner's candidate prefixes conservatively. An unknown
	// nested declaration must not silently inherit the first-party root terms.
	for _, prefix := range []string{"LICENSE", "LICENCE", "UNLICENSE", "UNLICENCE", "COPYING", "README", "NOTICE"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

func evaluateException(policy Policy, exception Exception, finding Finding) ([]string, error) {
	if !matchesExceptionSource(exception, finding.Module) ||
		!slices.Equal(canonical(finding.Detected), canonical(exception.Detected)) ||
		!allowed(policy, exception.Effective) {
		return nil, ErrUnapproved
	}

	if !matchesReportedFile(exception, finding) {
		return nil, ErrUnapproved
	}

	source := finding.Module

	err := moduleintegrity.Verify(source.Dir, source.Path+"@"+source.Version, source.Sum)
	if err != nil {
		return nil, ErrUnapproved
	}

	if !fingerprint(source.Dir, exception.Evidence, exception.SHA256) {
		return nil, ErrUnapproved
	}

	return canonical(exception.Effective), nil
}

func matchesExceptionSource(exception Exception, source Module) bool {
	return !source.Replaced && source.Path == exception.Module &&
		source.Version == exception.Version && source.Sum == exception.Sum && strings.HasPrefix(source.Sum, "h1:")
}

func matchesReportedFile(exception Exception, finding Finding) bool {
	expected := ""

	if exception.ReportedFile != "" {
		if !filepath.IsLocal(exception.ReportedFile) {
			return false
		}

		expected = filepath.Join(finding.Module.Dir, exception.ReportedFile)
	}

	return finding.LicenseFile == expected
}

func allowed(policy Policy, names []string) bool {
	if len(names) == 0 {
		return false
	}

	for _, name := range names {
		if name == "" || name == "Unknown" || !slices.Contains(policy.Allowed, name) {
			return false
		}
	}

	return true
}

func canonical(names []string) []string {
	result := slices.Clone(names)
	slices.Sort(result)

	return slices.Compact(result)
}

func within(root, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}

	resolvedRoot, rootErr := filepath.EvalSymlinks(root)

	resolvedPath, pathErr := filepath.EvalSymlinks(path)
	if rootErr != nil || pathErr != nil {
		return false
	}

	relative, err := filepath.Rel(resolvedRoot, resolvedPath)

	return err == nil && (relative == "." || filepath.IsLocal(relative))
}

func fingerprint(directory, name, expected string) bool {
	if !filepath.IsLocal(name) || name == "." || len(expected) != sha256.Size*2 {
		return false
	}

	contents, err := readEvidence(directory, name)
	if err != nil {
		return false
	}

	return fmt.Sprintf("%x", sha256.Sum256(contents)) == expected
}

func readEvidence(directory, name string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open license root: %w", err)
	}

	contents, readErr := readRootEvidence(root, name)

	return contents, errors.Join(readErr, root.Close())
}

func readRootEvidence(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect license evidence: %w", err)
	}

	if !info.Mode().IsRegular() || info.Size() > maxEvidenceBytes {
		return nil, ErrUnapproved
	}

	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open license evidence: %w", err)
	}

	contents, readErr := io.ReadAll(io.LimitReader(file, maxEvidenceBytes+1))
	err = errors.Join(readErr, file.Close())

	if len(contents) > maxEvidenceBytes {
		return nil, ErrUnapproved
	}

	if err != nil {
		return nil, fmt.Errorf("read license evidence: %w", err)
	}

	return contents, nil
}
