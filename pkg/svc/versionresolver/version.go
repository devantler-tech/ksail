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
	Suffix     string // distribution-specific suffix, e.g. "-k3s1"
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

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	return Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: matches[4],
		Suffix:     matches[5],
		Original:   tag,
	}, nil
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
func (v Version) Less(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// Equal returns true if v and other have the same major.minor.patch.
func (v Version) Equal(other Version) bool {
	return v.Major == other.Major && v.Minor == other.Minor && v.Patch == other.Patch
}

// String returns the version as "vMAJOR.MINOR.PATCH" with suffix if present.
func (v Version) String() string {
	base := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
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

// MatchingSuffix filters versions to those with the same suffix as the reference.
// This is useful for K3s where tags have suffixes like "-k3s1".
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
