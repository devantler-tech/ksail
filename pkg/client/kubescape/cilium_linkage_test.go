package kubescape_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ciliumModulePath = "github.com/cilium/cilium"

// GO-2026-6596 concerns Cilium's HTTP route controller. Its symbol-free Go
// report conservatively flags the Cilium module linked by KSail. The audited
// graph includes Hubble APIs and transitive label utilities, but excludes the
// HTTP route controller. This guard pins that package set and v1.20.2; any new
// package, changed version or replacement requires another assessment (#7432).
func TestCiliumHTTPRouteControllerStaysUnlinked(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "vendor", "modules.txt")); !os.IsNotExist(err) {
		t.Fatal("vendored Cilium sources require a new GO-2026-6596 assessment")
	}

	for _, name := range []string{"root", "desktop"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cgo, knownCGO := moduleCGO()[name]
			tags, knownTags := moduleBuildTags()[name]
			if !knownCGO || !knownTags {
				t.Fatalf("%s lacks release-validated build settings", name)
			}

			for _, target := range shippedPlatforms() {
				packages := listDependencyPackages(t, root, target, cgo, tags)
				if failure := validateCiliumLinkedPackages(packages); failure != "" {
					t.Fatalf("%s on %s: %s", name, target.String(), failure)
				}
			}
		})
	}
}

func validateCiliumLinkedPackages(packages []goListPackage) string {
	expected := map[string]bool{
		ciliumModulePath + "/api/v1/flow":                     false,
		ciliumModulePath + "/api/v1/models":                   false,
		ciliumModulePath + "/api/v1/observer":                 false,
		ciliumModulePath + "/api/v1/relay":                    false,
		ciliumModulePath + "/pkg/cidr":                        false,
		ciliumModulePath + "/pkg/clustermesh/types":           false,
		ciliumModulePath + "/pkg/command":                     false,
		ciliumModulePath + "/pkg/container/cache":             false,
		ciliumModulePath + "/pkg/defaults":                    false,
		ciliumModulePath + "/pkg/ip":                          false,
		ciliumModulePath + "/pkg/ipam/option":                 false,
		ciliumModulePath + "/pkg/k8s/slim/k8s/apis/labels":    false,
		ciliumModulePath + "/pkg/k8s/slim/k8s/apis/meta/v1":   false,
		ciliumModulePath + "/pkg/k8s/slim/k8s/apis/selection": false,
		ciliumModulePath + "/pkg/kpr":                         false,
		ciliumModulePath + "/pkg/labels":                      false,
		ciliumModulePath + "/pkg/lock":                        false,
		ciliumModulePath + "/pkg/logging":                     false,
		ciliumModulePath + "/pkg/logging/logfields":           false,
		ciliumModulePath + "/pkg/option":                      false,
		ciliumModulePath + "/pkg/time":                        false,
		ciliumModulePath + "/pkg/util":                        false,
		ciliumModulePath + "/pkg/version":                     false,
		ciliumModulePath + "/pkg/versioncheck":                false,
	}

	for _, pkg := range packages {
		if pkg.ImportPath != ciliumModulePath && !strings.HasPrefix(pkg.ImportPath, ciliumModulePath+"/") {
			continue
		}
		if _, allowed := expected[pkg.ImportPath]; !allowed {
			return fmt.Sprintf("unaudited Cilium package %q; re-establish the GO-2026-6596 disposition", pkg.ImportPath)
		}
		if pkg.Module == nil || pkg.Module.Path != ciliumModulePath || pkg.Module.Version != "v1.20.2" || pkg.Module.Replace != nil {
			return fmt.Sprintf("unaudited module for %q; require unreplaced Cilium v1.20.2", pkg.ImportPath)
		}
		expected[pkg.ImportPath] = true
	}

	for path, linked := range expected {
		if !linked {
			return fmt.Sprintf("missing audited Cilium package %q; the complete build graph is required", path)
		}
	}

	return ""
}

func TestCiliumDispositionRejectsUnauditedGraph(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"controller", "version", "replacement", "missing-module", "wrong-module", "empty", "partial"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			packages := ciliumGraphFixture()
			switch name {
			case "controller":
				packages = append(packages, goListPackage{ImportPath: ciliumModulePath + "/operator/pkg/gateway-api"})
			case "version":
				packages[0].Module.Version = "v1.20.3"
			case "replacement":
				packages[0].Module.Replace = &goListModule{Path: "example.test/cilium", Version: "v1.20.2"}
			case "missing-module":
				packages[0].Module = nil
			case "wrong-module":
				packages[0].Module.Path = "example.test/cilium"
			case "empty":
				packages = nil
			case "partial":
				packages = packages[:1]
			}

			if failure := validateCiliumLinkedPackages(packages); failure == "" {
				t.Fatal("unaudited graph must invalidate the advisory disposition")
			}
		})
	}
}

func TestCiliumDispositionAcceptsCompleteAuditedGraph(t *testing.T) {
	t.Parallel()

	if failure := validateCiliumLinkedPackages(ciliumGraphFixture()); failure != "" {
		t.Fatal(failure)
	}
}

func ciliumGraphFixture() []goListPackage {
	var packages []goListPackage
	for _, name := range []string{
		"api/v1/flow", "api/v1/models", "api/v1/observer", "api/v1/relay",
		"pkg/cidr", "pkg/clustermesh/types", "pkg/command", "pkg/container/cache",
		"pkg/defaults", "pkg/ip", "pkg/ipam/option", "pkg/k8s/slim/k8s/apis/labels",
		"pkg/k8s/slim/k8s/apis/meta/v1", "pkg/k8s/slim/k8s/apis/selection", "pkg/kpr",
		"pkg/labels", "pkg/lock", "pkg/logging", "pkg/logging/logfields", "pkg/option",
		"pkg/time", "pkg/util", "pkg/version", "pkg/versioncheck",
	} {
		packages = append(packages, goListPackage{
			ImportPath: ciliumModulePath + "/" + name,
			Module:     &goListModule{Path: ciliumModulePath, Version: "v1.20.2"},
		})
	}

	return packages
}
