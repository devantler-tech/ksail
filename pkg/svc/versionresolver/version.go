package versionresolver

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Version represents a parsed semantic version.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string
	Suffix     string // distribution-specific suffix, e.g. "k3s1" (without leading dash)
	Original   string // original tag string as found in the registry
}

// semverRegexp matches semver-like tags with optional "v" prefix, optional
// pre-release, and optional suffix (e.g., "-k3s1").
var semverRegexp = regexp.MustCompile(
	`^v?(\d+)\.(\d+)\.(\d+)` + // major.minor.patch
		`(?:-((?:alpha|beta|rc)[\w.]*))?` + // optional pre-release
		`(?:-(.+))?$`, // optional suffix like "k3s1"
)

// preReleaseRegexp identifies pre-release indicators in tags.
var preReleaseRegexp = regexp.MustCompile(`(?i)(alpha|beta|rc|dev|snapshot|nightly|canary|preview)`)

// ParseVersion parses a version tag string into a Version struct.
func ParseVersion(tag string) (Version, error) {
	matches := semverRegexp.FindStringSubmatch(tag)
	if matches == nil {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalidVersion, tag)
	}

	major, errMajor := strconv.Atoi(matches[1])
	minor, errMinor := strconv.Atoi(matches[2])
	patch, errPatch := strconv.Atoi(matches[3])

	if errMajor != nil || errMinor != nil || errPatch != nil {
		return Version{}, fmt.Errorf("%w: %q (numeric overflow)", ErrInvalidVersion, tag)
	}

	version := Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: matches[4],
		Suffix:     matches[5],
		Original:   tag,
	}

	// If the regex didn't capture a pre-release but the suffix contains a
	// pre-release indicator (e.g., "dev", "nightly"), promote it so that
	// Less() correctly orders pre-release < stable.
	if version.PreRelease == "" && version.Suffix != "" &&
		preReleaseRegexp.MatchString(version.Suffix) {
		version.PreRelease = version.Suffix
		version.Suffix = ""
	}

	return version, nil
}

// IsStable returns true if the version has no pre-release component and the
// original tag does not contain pre-release indicators.
func (v Version) IsStable() bool {
	if v.PreRelease != "" {
		return false
	}

	return !preReleaseRegexp.MatchString(v.Original)
}

// Less returns true if v is strictly less than other.
// When major.minor.patch are equal, suffixes are compared by extracting the
// trailing numeric portion (e.g., k3s1 < k3s2) so that K3s patch-level
// bumps are ordered correctly. Pre-release versions are always less than
// their stable counterpart (e.g., v1.35.1-rc.1 < v1.35.1).
func (v Version) Less(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}

	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}

	if v.Patch != other.Patch {
		return v.Patch < other.Patch
	}

	// Pre-release < stable (e.g., v1.35.1-rc.1 < v1.35.1).
	if v.PreRelease != "" && other.PreRelease == "" {
		return true
	}

	if v.PreRelease == "" && other.PreRelease != "" {
		return false
	}

	return suffixNum(v.Suffix) < suffixNum(other.Suffix)
}

// suffixNum extracts the trailing numeric portion of a suffix string.
// For example, "k3s2" → 2, "" → 0, "abc" → 0.
func suffixNum(suffix string) int {
	if suffix == "" {
		return 0
	}

	idx := len(suffix)
	for idx > 0 && suffix[idx-1] >= '0' && suffix[idx-1] <= '9' {
		idx--
	}

	if idx == len(suffix) {
		return 0
	}

	num, err := strconv.Atoi(suffix[idx:])
	if err != nil {
		return 0
	}

	return num
}

// Equal returns true if v and other have the same major.minor.patch, suffix,
// and pre-release label.
func (v Version) Equal(other Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor &&
		v.Patch == other.Patch && v.Suffix == other.Suffix &&
		v.PreRelease == other.PreRelease
}

// String returns the version as "vMAJOR.MINOR.PATCH" with pre-release and
// suffix if present (e.g., "v1.13.0-beta.1", "v1.35.3-k3s1").
func (v Version) String() string {
	base := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		base += "-" + v.PreRelease
	}

	if v.Suffix != "" {
		base += "-" + v.Suffix
	}

	return base
}

// FilterStable returns only stable versions from the input.
func FilterStable(versions []Version) []Version {
	result := make([]Version, 0, len(versions))
	for _, v := range versions {
		if v.IsStable() {
			result = append(result, v)
		}
	}

	return result
}

// NewerThan returns versions strictly newer than current, sorted ascending.
func NewerThan(versions []Version, current Version) []Version {
	result := make([]Version, 0, len(versions))
	for _, v := range versions {
		if current.Less(v) {
			result = append(result, v)
		}
	}

	SortVersions(result)

	return result
}

// SortVersions sorts versions in ascending order.
func SortVersions(versions []Version) {
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Less(versions[j])
	})
}

// ParseTags parses a list of tag strings into versions, skipping unparseable tags.
func ParseTags(tags []string) []Version {
	versions := make([]Version, 0, len(tags))
	for _, tag := range tags {
		// Skip tags that are clearly not semver
		if tag == "latest" || tag == "" {
			continue
		}

		v, err := ParseVersion(tag)
		if err != nil {
			continue
		}

		versions = append(versions, v)
	}

	return versions
}

// MatchingSuffix filters versions to those whose suffix shares the same base
// prefix as the reference. Trailing digits are stripped before comparison, so
// passing "k3s1" matches "k3s1", "k3s2", etc. When suffix is empty, only
// versions without a suffix are returned.
func MatchingSuffix(versions []Version, suffix string) []Version {
	if suffix == "" {
		result := make([]Version, 0, len(versions))
		for _, v := range versions {
			if v.Suffix == "" {
				result = append(result, v)
			}
		}

		return result
	}

	// Normalize suffix prefix matching (e.g., "k3s" matches "k3s1", "k3s2")
	baseSuffix := strings.TrimRight(suffix, "0123456789")

	result := make([]Version, 0, len(versions))
	for _, v := range versions {
		if strings.HasPrefix(v.Suffix, baseSuffix) {
			result = append(result, v)
		}
	}

	return result
}
