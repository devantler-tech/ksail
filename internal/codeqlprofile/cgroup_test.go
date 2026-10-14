package codeqlprofile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

func cgroupFixture(t *testing.T) (string, string) {
	t.Helper()

	proc, hierarchy := t.TempDir(), t.TempDir()
	for name, value := range map[string]string{
		"cgroup.controllers": "cpu memory\n",
		"parent/memory.max":  "12000\n", "parent/memory.swap.max": "max\n",
		"parent/worker/memory.max": "max\n", "parent/worker/memory.swap.max": "8000\n",
	} {
		path := filepath.Join(hierarchy, name)

		err := os.MkdirAll(filepath.Dir(path), 0o700)
		if err != nil {
			t.Fatal(err)
		}

		err = os.WriteFile(path, []byte(value), 0o600)
		if err != nil {
			t.Fatal(err)
		}
	}

	err := os.MkdirAll(filepath.Join(proc, "self"), 0o700)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(proc, "self/cgroup"), []byte("0::/parent/worker\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	return proc, hierarchy
}

func TestCgroupLimitsIncludesAncestorConstraints(t *testing.T) {
	t.Parallel()
	proc, hierarchy := cgroupFixture(t)

	limits, err := codeqlprofile.ReadCgroupLimits(proc, hierarchy)
	memory, swap := uint64(12000), uint64(8000)

	want := codeqlprofile.CgroupLimits{MemoryBytes: &memory, SwapBytes: &swap, Verified: true}
	if err != nil || !reflect.DeepEqual(limits, want) {
		t.Fatalf("inherited limits lost: %+v, %v", limits, err)
	}
}

func TestCgroupRootHasNoLocalConstraint(t *testing.T) {
	t.Parallel()
	proc, hierarchy := cgroupFixture(t)

	err := os.WriteFile(
		filepath.Join(proc, "self/cgroup"),
		[]byte("0::/\n"),
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}

	limits, err := codeqlprofile.ReadCgroupLimits(proc, hierarchy)
	if err != nil || !reflect.DeepEqual(limits, codeqlprofile.CgroupLimits{Verified: true}) {
		t.Fatal("root unlimited observation rejected")
	}
}

func TestCgroupLimitsRejectsUnknownMalformedAndEscapingReads(t *testing.T) {
	t.Parallel()

	for mode, mutate := range cgroupInvalidFixtures(t) {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			proc, hierarchy := cgroupFixture(t)

			err := mutate(proc, hierarchy)
			if err != nil {
				t.Fatal(err)
			}

			limits, err := codeqlprofile.ReadCgroupLimits(
				proc,
				hierarchy,
			)
			if err == nil ||
				limits.Verified {
				t.Fatalf("unknown limits accepted: %s", mode)
			}
		})
	}
}

func cgroupInvalidFixtures(t *testing.T) map[string]func(string, string) error {
	t.Helper()

	writeGroup := func(value string) func(string, string) error {
		return func(proc, _ string) error {
			return os.WriteFile(filepath.Join(proc, "self/cgroup"), []byte(value), 0o600)
		}
	}

	return map[string]func(string, string) error{
		"malformed": writeGroup("partial\n"),
		"duplicate": writeGroup("0::/parent\n0::/parent/worker\n"),
		"escape":    writeGroup("0::/../../outside\n"),
		"missing": func(_, hierarchy string) error {
			return os.Remove(filepath.Join(hierarchy, "parent/worker/memory.max"))
		},
		"symlink": func(_, hierarchy string) error {
			err := os.RemoveAll(filepath.Join(hierarchy, "parent/worker"))
			if err != nil {
				t.Fatal(err)
			}

			return os.Symlink(t.TempDir(), filepath.Join(hierarchy, "parent/worker"))
		},
		"bad-limit": func(_, hierarchy string) error {
			return os.WriteFile(
				filepath.Join(hierarchy, "parent/worker/memory.max"),
				[]byte("123\nmax\n"),
				0o600,
			)
		},
	}
}
