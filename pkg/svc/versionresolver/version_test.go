package versionresolver_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/svc/versionresolver"
)

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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v, err := versionresolver.ParseVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if v.Major != tt.wantMajor || v.Minor != tt.wantMinor || v.Patch != tt.wantPatch {
				t.Errorf("version = %d.%d.%d, want %d.%d.%d",
					v.Major, v.Minor, v.Patch, tt.wantMajor, tt.wantMinor, tt.wantPatch)
			}

			if v.PreRelease != tt.wantPre {
				t.Errorf("prerelease = %q, want %q", v.PreRelease, tt.wantPre)
			}

			if v.Suffix != tt.wantSuf {
				t.Errorf("suffix = %q, want %q", v.Suffix, tt.wantSuf)
			}

			if v.Original != tt.wantOrig {
				t.Errorf("original = %q, want %q", v.Original, tt.wantOrig)
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
