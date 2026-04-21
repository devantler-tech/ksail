package versionresolver_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/versionresolver"
)

//nolint:cyclop,funlen // table-driven test with many assertions per case
func TestParseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantPre   string
		wantSuf   string
		wantOrig  string
		wantErr   bool
	}{
		{
			name:      "plain semver",
			input:     "v1.35.1",
			wantMajor: 1, wantMinor: 35, wantPatch: 1,
			wantOrig: "v1.35.1",
		},
		{
			name:      "without v prefix",
			input:     "1.35.1",
			wantMajor: 1, wantMinor: 35, wantPatch: 1,
			wantOrig: "1.35.1",
		},
		{
			name:      "with prerelease",
			input:     "v1.13.0-beta.1",
			wantMajor: 1, wantMinor: 13, wantPatch: 0,
			wantPre: "beta.1", wantOrig: "v1.13.0-beta.1",
		},
		{
			name:      "with suffix k3s",
			input:     "v1.35.3-k3s1",
			wantMajor: 1, wantMinor: 35, wantPatch: 3,
			wantSuf: "k3s1", wantOrig: "v1.35.3-k3s1",
		},
		{
			name:    "invalid input",
			input:   "latest",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			version, err := versionresolver.ParseVersion(testCase.input)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", testCase.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if version.Major != testCase.wantMajor ||
				version.Minor != testCase.wantMinor ||
				version.Patch != testCase.wantPatch {
				t.Errorf("version = %d.%d.%d, want %d.%d.%d",
					version.Major, version.Minor, version.Patch,
					testCase.wantMajor, testCase.wantMinor, testCase.wantPatch)
			}

			if version.PreRelease != testCase.wantPre {
				t.Errorf("prerelease = %q, want %q", version.PreRelease, testCase.wantPre)
			}

			if version.Suffix != testCase.wantSuf {
				t.Errorf("suffix = %q, want %q", version.Suffix, testCase.wantSuf)
			}

			if version.Original != testCase.wantOrig {
				t.Errorf("original = %q, want %q", version.Original, testCase.wantOrig)
			}
		})
	}
}

func TestFilterStable(t *testing.T) {
	t.Parallel()

	tags := []string{
		"v1.35.0", "v1.35.1-alpha.1", "v1.35.1-beta.0",
		"v1.35.1", "v1.35.2-rc.1", "v1.35.2",
	}

	versions := versionresolver.ParseTags(tags)
	stable := versionresolver.FilterStable(versions)

	for _, v := range stable {
		if v.PreRelease != "" {
			t.Errorf("FilterStable returned pre-release version: %s", v.Original)
		}
	}

	if len(stable) != 3 {
		t.Errorf("expected 3 stable versions, got %d", len(stable))
	}
}

func TestNewerThan(t *testing.T) {
	t.Parallel()

	current, _ := versionresolver.ParseVersion("v1.35.0")
	tags := []string{"v1.34.0", "v1.35.0", "v1.35.1", "v1.36.0"}
	versions := versionresolver.ParseTags(tags)

	newer := versionresolver.NewerThan(versions, current)

	if len(newer) != 2 {
		t.Fatalf("expected 2 newer versions, got %d", len(newer))
	}

	if newer[0].Original != "v1.35.1" {
		t.Errorf("first newer = %q, want %q", newer[0].Original, "v1.35.1")
	}

	if newer[1].Original != "v1.36.0" {
		t.Errorf("second newer = %q, want %q", newer[1].Original, "v1.36.0")
	}
}

func TestNewerThan_PreReleaseCurrent(t *testing.T) {
	t.Parallel()

	current, _ := versionresolver.ParseVersion("v1.35.1-rc.1")
	tags := []string{"v1.35.0", "v1.35.1", "v1.35.2"}
	versions := versionresolver.ParseTags(tags)

	newer := versionresolver.NewerThan(versions, current)

	// v1.35.1 should be considered newer than v1.35.1-rc.1 (stable > pre-release)
	if len(newer) != 2 {
		t.Fatalf("expected 2 newer versions (v1.35.1, v1.35.2), got %d: %v", len(newer), newer)
	}

	if newer[0].Original != "v1.35.1" {
		t.Errorf("first newer = %q, want %q", newer[0].Original, "v1.35.1")
	}

	if newer[1].Original != "v1.35.2" {
		t.Errorf("second newer = %q, want %q", newer[1].Original, "v1.35.2")
	}
}

func TestSortVersions(t *testing.T) {
	t.Parallel()

	tags := []string{"v1.36.0", "v1.35.0", "v1.35.2", "v1.35.1"}
	versions := versionresolver.ParseTags(tags)

	versionresolver.SortVersions(versions)

	expected := []string{"v1.35.0", "v1.35.1", "v1.35.2", "v1.36.0"}
	for i, v := range versions {
		if v.Original != expected[i] {
			t.Errorf("index %d = %q, want %q", i, v.Original, expected[i])
		}
	}
}

func TestMatchingSuffix(t *testing.T) {
	t.Parallel()

	tags := []string{"v1.35.0", "v1.35.0-k3s1", "v1.35.1-k3s2", "v1.35.2"}
	versions := versionresolver.ParseTags(tags)

	withSuffix := versionresolver.MatchingSuffix(versions, "k3s")
	if len(withSuffix) != 2 {
		t.Fatalf("expected 2 k3s versions, got %d", len(withSuffix))
	}

	noSuffix := versionresolver.MatchingSuffix(versions, "")
	if len(noSuffix) != 2 {
		t.Fatalf("expected 2 plain versions, got %d", len(noSuffix))
	}
}

func TestLess_K3sSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{
			name: "k3s1 < k3s2 same semver",
			a:    "v1.35.3-k3s1",
			b:    "v1.35.3-k3s2",
			want: true,
		},
		{
			name: "k3s2 not less than k3s1",
			a:    "v1.35.3-k3s2",
			b:    "v1.35.3-k3s1",
			want: false,
		},
		{
			name: "different patch takes precedence",
			a:    "v1.35.2-k3s2",
			b:    "v1.35.3-k3s1",
			want: true,
		},
		{
			name: "same version same suffix not less",
			a:    "v1.35.3-k3s1",
			b:    "v1.35.3-k3s1",
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			versionA, _ := versionresolver.ParseVersion(testCase.a)
			versionB, _ := versionresolver.ParseVersion(testCase.b)

			if got := versionA.Less(versionB); got != testCase.want {
				t.Errorf("%s.Less(%s) = %v, want %v", testCase.a, testCase.b, got, testCase.want)
			}
		})
	}
}

func TestSortVersions_K3sSuffix(t *testing.T) {
	t.Parallel()

	tags := []string{"v1.35.3-k3s2", "v1.35.3-k3s1", "v1.35.2-k3s1", "v1.35.4-k3s1"}
	versions := versionresolver.ParseTags(tags)
	versionresolver.SortVersions(versions)

	expected := []string{"v1.35.2-k3s1", "v1.35.3-k3s1", "v1.35.3-k3s2", "v1.35.4-k3s1"}
	for i, v := range versions {
		if v.Original != expected[i] {
			t.Errorf("index %d = %q, want %q", i, v.Original, expected[i])
		}
	}
}
