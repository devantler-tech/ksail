package kubescape_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/semver"
)

// TestOTelLoggingModulesUseFixedSDK requires the patched upstream implementation
// in every release graph, including the tagged desktop build. A replacement or
// partial package listing cannot claim the fixed upstream version.
func TestOTelLoggingModulesUseFixedSDK(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	_, err := os.Stat(filepath.Join(root, "vendor", "modules.txt"))
	if !os.IsNotExist(err) {
		t.Fatal("vendored logging sources require verification against the actual fixed implementation")
	}
	for _, name := range []string{"root", "desktop"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, target := range shippedPlatforms() {
				packages := listDependencyPackages(t, root, target, moduleCGO()[name], moduleBuildTags()[name])
				if problem := validateFixedOTelModules(packages); problem != "" {
					t.Fatalf("%s/%s: %s", name, target, problem)
				}
			}
		})
	}
}

func validateFixedOTelModules(packages []goListPackage) string {
	required := map[string]bool{
		"go.opentelemetry.io/otel/log":                                false,
		"go.opentelemetry.io/otel/sdk/log":                            false,
		"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp": false,
	}
	for _, pkg := range packages {
		if _, expected := required[pkg.ImportPath]; !expected {
			continue
		}
		if !fixedOTelModule(pkg) {
			return fmt.Sprintf(
				"%s is not supplied by an unreplaced fixed logging module: %+v",
				pkg.ImportPath, pkg.Module,
			)
		}
		required[pkg.ImportPath] = true
	}
	for path, linked := range required {
		if !linked {
			return "the complete release graph must include " + path
		}
	}
	return ""
}

func fixedOTelModule(pkg goListPackage) bool {
	module := pkg.Module
	return module != nil && module.Path == pkg.ImportPath && module.Replace == nil &&
		semver.IsValid(module.Version) && semver.Compare(module.Version, "v0.21.0") >= 0
}

func TestFixedOTelModulesRejectUnpatchedOrUnauditedSources(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"old", "replacement", "missing", "wrong-module", "invalid-version", "partial"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			packages := fixedOTelFixture()
			switch name {
			case "old":
				packages[0].Module.Version = "v0.19.0"
			case "replacement":
				packages[0].Module.Replace = &goListModule{Path: "example.invalid/sdk"}
			case "missing":
				packages[0].Module = nil
			case "wrong-module":
				packages[0].Module.Path += "/other"
			case "invalid-version":
				packages[0].Module.Version = "unknown"
			case "partial":
				packages = packages[:1]
			}
			if problem := validateFixedOTelModules(packages); problem == "" {
				t.Fatal("an unaudited or incomplete graph claimed the fixed SDK")
			}
		})
	}
	if problem := validateFixedOTelModules(fixedOTelFixture()); problem != "" {
		t.Fatal(problem)
	}
}

func fixedOTelFixture() []goListPackage {
	paths := []string{
		"go.opentelemetry.io/otel/log",
		"go.opentelemetry.io/otel/sdk/log",
		"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp",
	}
	packages := make([]goListPackage, 0, len(paths))
	for _, path := range paths {
		packages = append(packages, goListPackage{
			ImportPath: path, Module: &goListModule{Path: path, Version: "v0.21.0"},
		})
	}
	return packages
}
