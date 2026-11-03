package kubescape_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/licensepolicy"
	"github.com/google/go-licenses/v2/licenses"
)

func TestLicenseReportsRetainReplacementIdentityWithoutLocalPaths(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"direct", "published-fork", "local"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := newLicenseInventoryFixture()
			directory := t.TempDir()
			file := filepath.Join(directory, "LICENSE")

			err := os.WriteFile(file, []byte("MIT"), 0o600)
			if err != nil {
				t.Fatal(err)
			}

			pkg := &fixture.packages[0]
			pkg.Dir, pkg.Module.Dir = directory, directory
			pkg.Module.Sum = "h1:declared-source"
			fixture.libraries[0].LicenseFile = file
			want := replacementReportFixture(name, directory, pkg.Module)
			rows := evaluateLicenseInventory(
				t,
				directory,
				licensepolicy.Policy{Allowed: []string{"MIT"}},
				fixture.packages,
				fixture.libraries,
			)

			encoded, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}

			var decoded []map[string]any

			err = json.Unmarshal(encoded, &decoded)
			if err != nil || len(decoded) != 1 {
				t.Fatalf("invalid report: %v", err)
			}

			row := decoded[0]
			if row["module"] != pkg.Module.Path || row["version"] != pkg.Module.Version ||
				row["checksum"] != pkg.Module.Sum || !reflect.DeepEqual(row["replacement"], want) {
				t.Fatalf("declared/replacement source evidence changed or disappeared: %s", encoded)
			}
		})
	}
}

func replacementReportFixture(name, directory string, module *goListModule) any {
	switch name {
	case "published-fork":
		module.Replace = &goListModule{
			Path: "example.test/fork", Version: "v2.0.0", Sum: "h1:selected-fork", Dir: directory,
		}

		return map[string]any{
			"kind":     "module",
			"module":   "example.test/fork",
			"version":  "v2.0.0",
			"checksum": "h1:selected-fork",
		}
	case "local":
		module.Replace = &goListModule{Path: "/private/source/never-publish-this", Dir: directory}

		return map[string]any{"kind": "local"}
	default:
		return nil
	}
}

func TestLicenseInventoryRequiresCompleteIndependentMembership(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"complete", "unknown-still-counted", "standard-library", "empty-graph", "empty-inventory",
		"partial", "duplicate-graph", "duplicate-membership", "unexpected-package",
		"missing-module", "missing-source-directory", "nil-library", "empty-library", "missing-license-file",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := newLicenseInventoryFixture()
			changeLicenseGraph(name, &fixture)
			changeLicenseLibraries(name, &fixture)
			_, err := completeLicenseMembership(fixture.packages, fixture.libraries)

			wantPass := name == "complete" || name == "unknown-still-counted" ||
				name == "standard-library"
			if (err == nil) != wantPass {
				t.Fatalf("%s: pass=%v want=%v error=%v", name, err == nil, wantPass, err)
			}
		})
	}
}

type licenseClassifierFunc func(string) ([]licenses.License, error)

func (function licenseClassifierFunc) Identify(path string) ([]licenses.License, error) {
	return function(path)
}

func TestClassifierSerializesConcurrentEvidenceReads(t *testing.T) {
	t.Parallel()

	var (
		active, calls atomic.Int32
		overlapped    atomic.Bool
	)

	classifier := strictLicenseClassifier{
		delegate: licenseClassifierFunc(func(string) ([]licenses.License, error) {
			calls.Add(1)

			if active.Add(1) != 1 {
				overlapped.Store(true)
			}
			defer active.Add(-1)

			time.Sleep(time.Millisecond)

			return nil, nil
		}),
	}
	start := make(chan struct{})

	var workers sync.WaitGroup
	for index := range 32 {
		workers.Go(func() {
			<-start

			_, _ = classifier.Identify("fixture-license-" + strconv.Itoa(index))
		})
	}

	close(start)
	workers.Wait()

	if overlapped.Load() || calls.Load() != 32 {
		t.Fatal("concurrent scanner workers must not share mutable classifier state")
	}
}

func TestClassifierReusesImmutableEvidenceWithoutAliasingResults(t *testing.T) {
	t.Parallel()

	calls := 0
	delegateResult := []licenses.License{{Name: "MIT"}}
	classifier := strictLicenseClassifier{
		delegate: licenseClassifierFunc(func(string) ([]licenses.License, error) {
			calls++

			return delegateResult, nil
		}),
	}

	first, err := classifier.Identify("same-immutable-source")
	if err != nil || len(first) != 1 {
		t.Fatal("first classification failed")
	}

	first[0].Name = "caller-mutation"
	delegateResult[0].Name = "delegate-mutation"

	second, err := classifier.Identify("same-immutable-source")
	if err != nil || len(second) != 1 || second[0].Name != "MIT" || calls != 1 {
		t.Fatal("classification reuse must retain its own evidence and isolate callers")
	}
}

func TestClassifierRetainsFailuresAndCachesSuccessfulUnknownEvidence(t *testing.T) {
	t.Parallel()

	calls := 0
	classifier := strictLicenseClassifier{
		delegate: licenseClassifierFunc(func(string) ([]licenses.License, error) {
			calls++
			if calls == 1 {
				return nil, os.ErrPermission
			}

			return nil, nil
		}),
	}

	_, err := classifier.Identify("same-immutable-source")
	if !errors.Is(err, os.ErrPermission) || !classifier.hadFailure() {
		t.Fatal("classification failure must retain its cause and fail the inventory")
	}

	for range 2 {
		result, retryErr := classifier.Identify("same-immutable-source")
		if retryErr != nil || len(result) != 0 {
			t.Fatal("successful unknown classification must remain unknown")
		}
	}

	if calls != 2 || !classifier.hadFailure() {
		t.Fatal("errors must stay uncached and successful reuse must not clear a prior failure")
	}
}

func TestClassifierReadErrorsCannotBecomeUnknownApprovals(t *testing.T) {
	t.Parallel()

	for _, failed := range []bool{true, false} {
		classifier := strictLicenseClassifier{
			delegate: licenseClassifierFunc(func(string) ([]licenses.License, error) {
				if failed {
					return nil, os.ErrPermission
				}

				return nil, nil
			}),
		}

		_, err := classifier.Identify("fixture-license")
		if (err != nil) != failed || classifier.hadFailure() != failed {
			t.Fatal(
				"classifier failure evidence was lost or an unknown match became an I/O failure",
			)
		}
	}
}

type licenseInventoryFixture struct {
	packages  []goListPackage
	libraries []*licenses.Library
}

func newLicenseInventoryFixture() licenseInventoryFixture {
	packages := make([]goListPackage, 1, 2)
	packages[0] = goListPackage{
		ImportPath: "example.test/dependency", Dir: "/source",
		Module: &goListModule{Path: "example.test/dependency", Version: "v1.0.0"},
	}

	return licenseInventoryFixture{packages, []*licenses.Library{{
		Packages: []string{"example.test/dependency"}, LicenseFile: "/source/LICENSE",
		Licenses: []licenses.License{{Name: "MIT"}},
	}}}
}

func changeLicenseGraph(name string, fixture *licenseInventoryFixture) {
	switch name {
	case "standard-library":
		fixture.packages = append(
			fixture.packages,
			goListPackage{ImportPath: "bytes", Standard: true},
		)
	case "empty-graph":
		fixture.packages = nil
	case "partial":
		fixture.packages = append(fixture.packages, goListPackage{
			ImportPath: "example.test/dependency/new",
			Dir:        "/source/new",
			Module:     fixture.packages[0].Module,
		})
	case "duplicate-graph":
		fixture.packages = append(fixture.packages, fixture.packages[0])
	case "missing-module":
		fixture.packages[0].Module = nil
	case "missing-source-directory":
		fixture.packages[0].Dir = ""
	}
}

func changeLicenseLibraries(name string, fixture *licenseInventoryFixture) {
	switch name {
	case "unknown-still-counted":
		fixture.libraries[0].Licenses = nil
		fixture.libraries[0].LicenseFile = ""
	case "empty-inventory":
		fixture.libraries = nil
	case "duplicate-membership":
		fixture.libraries[0].Packages = append(
			fixture.libraries[0].Packages,
			fixture.libraries[0].Packages[0],
		)
	case "unexpected-package":
		fixture.libraries[0].Packages = []string{"example.test/other"}
	case "nil-library":
		fixture.libraries = []*licenses.Library{nil}
	case "empty-library":
		fixture.libraries[0].Packages = nil
	case "missing-license-file":
		fixture.libraries[0].LicenseFile = ""
	}
}
