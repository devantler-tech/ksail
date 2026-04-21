package configmanager_test

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variables are required to prevent compiler optimization.
var (
	benchNewManagerSink   *configmanager.ConfigManager
	benchLoadNoFileSink   any
	benchLoadWithFileSink any
)

// BenchmarkInitializeViper measures the cost of creating a fresh ConfigManager,
// which includes Viper initialisation (file settings, config paths,
// parent-directory traversal, and environment variable binding).
// This is the first operation executed on every KSail command invocation.
func BenchmarkInitializeViper(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchNewManagerSink = configmanager.NewConfigManager(io.Discard, "")
	}
}

// BenchmarkNewConfigManager_WithSelectors measures ConfigManager construction
// cost when typical production field selectors are registered (the normal path
// for cluster lifecycle commands).
func BenchmarkNewConfigManager_WithSelectors(b *testing.B) {
	selectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultDistributionFieldSelector(),
		configmanager.DefaultProviderFieldSelector(),
		configmanager.StandardSourceDirectoryFieldSelector(),
		configmanager.DefaultDistributionConfigFieldSelector(),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchNewManagerSink = configmanager.NewConfigManager(io.Discard, "", selectors...)
	}
}

// BenchmarkLoad_NoConfigFile measures the full Load() cycle when no ksail.yaml
// is present. This is the path followed by commands run outside a KSail project
// directory (e.g., `ksail cluster init`). The cycle includes Viper ReadInConfig
// (miss), Unmarshal with mapstructure, and field-selector default application.
func BenchmarkLoad_NoConfigFile(b *testing.B) {
	tmpDir := b.TempDir()

	b.Chdir(tmpDir)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		mgr := configmanager.NewConfigManager(
			io.Discard,
			"",
			configmanager.DefaultClusterFieldSelectors()...,
		)

		cfg, loadErr := mgr.Load(configmanagerinterface.LoadOptions{
			Silent:         true,
			SkipValidation: true,
		})
		if loadErr != nil {
			b.Fatal(loadErr)
		}

		benchLoadNoFileSink = cfg
	}
}

// BenchmarkLoad_WithConfigFile measures the full Load() cycle when a valid
// ksail.yaml is present in the working directory. This is the hot path for all
// operational commands (create, update, delete, etc.). The cycle includes
// Viper ReadInConfig (hit), YAML decode, mapstructure Unmarshal,
// environment-variable expansion, path absolutisation, and field-selector
// default application.
func BenchmarkLoad_WithConfigFile(b *testing.B) {
	tmpDir := b.TempDir()
	writeConfigFiles(b, tmpDir)

	b.Chdir(tmpDir)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		mgr := configmanager.NewConfigManager(
			io.Discard,
			"",
			configmanager.DefaultClusterFieldSelectors()...,
		)

		cfg, loadErr := mgr.Load(configmanagerinterface.LoadOptions{
			Silent:         true,
			SkipValidation: true,
		})
		if loadErr != nil {
			b.Fatal(loadErr)
		}

		benchLoadWithFileSink = cfg
	}
}

// BenchmarkLoad_WithConfigFile_DeepTree measures Load() from a deeply nested
// working directory (10 levels). The parent-directory traversal in
// InitializeViper walks every ancestor looking for ksail.yaml, so directory
// depth directly affects the number of os.Stat() calls issued before the
// manager is fully initialised.
func BenchmarkLoad_WithConfigFile_DeepTree(b *testing.B) {
	tmpDir := b.TempDir()
	writeConfigFiles(b, tmpDir)

	// Build a 10-level deep subdirectory structure below the project root.
	deepDir := tmpDir
	for i := range 10 {
		deepDir = filepath.Join(deepDir, "level"+strconv.Itoa(i))

		err := os.MkdirAll(deepDir, 0o750)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.Chdir(deepDir)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		mgr := configmanager.NewConfigManager(
			io.Discard,
			"",
			configmanager.DefaultClusterFieldSelectors()...,
		)

		cfg, loadErr := mgr.Load(configmanagerinterface.LoadOptions{
			Silent:         true,
			SkipValidation: true,
		})
		if loadErr != nil {
			b.Fatal(loadErr)
		}

		benchLoadWithFileSink = cfg
	}
}

// BenchmarkLoad_Cached measures the cost of a second Load() call on the same
// manager. After the first load the result is cached; subsequent calls should
// return immediately without re-reading files or decoding YAML.
func BenchmarkLoad_Cached(b *testing.B) {
	tmpDir := b.TempDir()
	writeConfigFiles(b, tmpDir)

	b.Chdir(tmpDir)

	mgr := configmanager.NewConfigManager(
		io.Discard,
		"",
		configmanager.DefaultClusterFieldSelectors()...,
	)

	// Prime the cache with a first load.
	_, err := mgr.Load(configmanagerinterface.LoadOptions{
		Silent:         true,
		SkipValidation: true,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		cfg, loadErr := mgr.Load(configmanagerinterface.LoadOptions{
			Silent:         true,
			SkipValidation: true,
		})
		if loadErr != nil {
			b.Fatal(loadErr)
		}

		benchLoadWithFileSink = cfg
	}
}

// writeConfigFiles creates a minimal valid ksail.yaml + kind.yaml pair in dir,
// matching the pattern used throughout manager_test.go.
func writeConfigFiles(tb testing.TB, dir string) {
	tb.Helper()

	err := os.MkdirAll(filepath.Join(dir, "k8s"), 0o750)
	if err != nil {
		tb.Fatal(err)
	}

	ksailContent := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  cluster:\n" +
		"    distribution: Vanilla\n" +
		"    distributionConfig: kind.yaml\n" +
		"  workload:\n" +
		"    sourceDirectory: k8s\n"

	kindContent := "apiVersion: kind.x-k8s.io/v1alpha4\nkind: Cluster\nname: kind\n"

	err = os.WriteFile(filepath.Join(dir, "ksail.yaml"), []byte(ksailContent), 0o600)
	if err != nil {
		tb.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(dir, "kind.yaml"), []byte(kindContent), 0o600)
	if err != nil {
		tb.Fatal(err)
	}
}
