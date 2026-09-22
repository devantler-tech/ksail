package environment

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

const (
	// configFilePrefix and configFileSuffix bracket a per-environment root config
	// file name: ksail.<name>.yaml (the same convention cluster add-environment's
	// --from resolves against).
	configFilePrefix = "ksail."
	configFileSuffix = ".yaml"

	// BaseConfigFile is the workspace's base root config. It is not named after
	// an environment, but its workload kustomizationFile may sync one
	// clusters/<name> overlay — the initial environment `project init
	// --multi-cluster <name>` scaffolds without a ksail.<name>.yaml — which
	// DeriveEnvironments then reports as a declared environment.
	BaseConfigFile = "ksail.yaml"
)

// ErrDiscoverEnvironments is returned by DeriveEnvironments when the workspace
// directory itself cannot be read (a per-environment config that fails to load is
// skipped, not surfaced as this error).
var ErrDiscoverEnvironments = errors.New("failed to discover environments")

// Environment is the read-side model of a cluster environment declared in a ksail
// workspace — by a ksail.<name>.yaml root config, or by the base ksail.yaml
// syncing its clusters/<name> overlay. It is the enumeration foundation for the
// multi-cluster layout: higher layers list, validate a --from against, or
// reconcile the clusters/ tree from the declared set (issue #5441 item 3).
type Environment struct {
	// Name is the environment identifier — the <name> in ksail.<name>.yaml (or in
	// the base config's clusters/<name> sync path). It is also the
	// clusters/<name>/ overlay directory segment.
	Name string
	// ConfigFile is the root config file that declares the environment, relative
	// to the workspace root: ksail.<name>.yaml, or BaseConfigFile for a
	// base-synced environment.
	ConfigFile string
	// Distribution is the cluster distribution declared by the config (Vanilla,
	// K3s, Talos, …).
	Distribution v1alpha1.Distribution
	// Provider is the cluster provider declared by the config (Docker, Hetzner, …).
	Provider v1alpha1.Provider
}

// IsBaseSynced reports whether the environment is declared by the base
// ksail.yaml's kustomizationFile rather than by a ksail.<name>.yaml of its own.
// Such an environment shares its root config with the whole workspace, so a verb
// that deletes an environment's config must not touch it.
func (e Environment) IsBaseSynced() bool {
	return e.ConfigFile == BaseConfigFile
}

// ConfigLoader loads the ksail config declared by the given workspace-relative
// configFile (e.g. "ksail.prod.yaml"). It is injected so this package stays free of
// a config-manager dependency and DeriveEnvironments is unit-testable without the
// filesystem; the CLI wires the real ksail config manager.
type ConfigLoader func(configFile string) (*v1alpha1.Cluster, error)

// DeriveEnvironments enumerates the environments declared in repoRoot, loading
// each root config through load to read its declared distribution and provider.
// An environment is declared either by its own ksail.<name>.yaml root config or —
// for the initial environment `project init --multi-cluster <name>` scaffolds —
// by the base ksail.yaml's workload kustomizationFile syncing clusters/<name>
// (see [Environment.IsBaseSynced]); a name declared both ways is listed once,
// from its own config. It is the single resolver every env verb (list, add's
// --from, rm, and DerivePlan behind reconcile) shares, so they cannot disagree
// about which environments exist.
//
// A ksail.<name>.yaml whose <name> is not a DNS-1123 label is excluded, as is a
// base config syncing anything other than exactly one environment overlay under
// clusters/ (the shared clusters/base overlay included). A config that fails to
// load is skipped — a single malformed file must not hide the environments that
// do load — so the result lists every usable declared environment, sorted by
// name. Only an unreadable repoRoot yields an error (ErrDiscoverEnvironments).
func DeriveEnvironments(repoRoot string, load ConfigLoader) ([]Environment, error) {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDiscoverEnvironments, err)
	}

	environments := make([]Environment, 0, len(entries))
	hasBaseConfig := false

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if entry.Name() == BaseConfigFile {
			hasBaseConfig = true

			continue
		}

		env, ok := loadDeclaredEnvironment(entry.Name(), load)
		if ok {
			environments = append(environments, env)
		}
	}

	// Only a base config actually present in the workspace declares an
	// environment: the CLI's loader can resolve defaults for a missing file.
	if hasBaseConfig {
		environments = appendBaseSyncedEnvironment(environments, load)
	}

	slices.SortFunc(environments, func(left, right Environment) int {
		return strings.Compare(left.Name, right.Name)
	})

	return environments, nil
}

// loadDeclaredEnvironment reads the environment a ksail.<name>.yaml root config
// declares, reporting ok=false for a file that is not such a config or that fails
// to load.
func loadDeclaredEnvironment(fileName string, load ConfigLoader) (Environment, bool) {
	name, ok := environmentNameFromConfigFile(fileName)
	if !ok {
		return Environment{}, false
	}

	cfg, err := load(fileName)
	if err != nil || cfg == nil {
		// A malformed config is not a usable environment and must not hide the
		// rest, but leave a debug trace so a missing environment entry is
		// troubleshootable rather than silently absent.
		slog.Default().Debug(
			"skipping unloadable environment config",
			"file", fileName,
			"error", err,
		)

		return Environment{}, false
	}

	return Environment{
		Name:         name,
		ConfigFile:   fileName,
		Distribution: cfg.Spec.Cluster.Distribution,
		Provider:     cfg.Spec.Cluster.Provider,
	}, true
}

// appendBaseSyncedEnvironment adds the environment the base ksail.yaml declares
// through its kustomizationFile, unless a ksail.<name>.yaml already declares that
// name (its own config wins, so the environment is listed once).
func appendBaseSyncedEnvironment(environments []Environment, load ConfigLoader) []Environment {
	baseEnv, ok := baseConfigEnvironment(load)
	if !ok {
		return environments
	}

	if slices.ContainsFunc(environments, func(env Environment) bool {
		return env.Name == baseEnv.Name
	}) {
		return environments
	}

	return append(environments, baseEnv)
}

// baseConfigEnvironment derives the environment the workspace's base ksail.yaml
// declares through its workload kustomizationFile (clusters/<name>), reporting
// ok=false when there is no loadable base config, its sync path is not exactly
// one directory under clusters/, or that directory is the shared clusters/base
// overlay every environment builds on (which declares no environment).
func baseConfigEnvironment(load ConfigLoader) (Environment, bool) {
	cfg, err := load(BaseConfigFile)
	if err != nil || cfg == nil {
		slog.Default().Debug(
			"skipping unloadable base config",
			"file", BaseConfigFile,
			"error", err,
		)

		return Environment{}, false
	}

	sync := path.Clean(filepath.ToSlash(cfg.Spec.Workload.KustomizationFile))

	dir, name := path.Split(sync)
	if path.Clean(dir) != ClustersDir || name == "" || name == BaseEnvName {
		return Environment{}, false
	}

	return Environment{
		Name:         name,
		ConfigFile:   BaseConfigFile,
		Distribution: cfg.Spec.Cluster.Distribution,
		Provider:     cfg.Spec.Cluster.Provider,
	}, true
}

// environmentNameFromConfigFile extracts the environment <name> from a
// ksail.<name>.yaml file name. It returns ok=false for the base ksail.yaml (empty
// <name>), any file not matching the prefix/suffix, and any <name> that is not a
// DNS-1123 label (e.g. ksail.prod.backup.yaml, whose "prod.backup" contains a dot),
// mirroring the naming rule add-environment enforces on --from.
func environmentNameFromConfigFile(fileName string) (string, bool) {
	if !strings.HasPrefix(fileName, configFilePrefix) ||
		!strings.HasSuffix(fileName, configFileSuffix) {
		return "", false
	}

	// The prefix and suffix must not overlap: the base ksail.yaml satisfies both
	// but leaves no <name> between them (len 10 <= len("ksail.")+len(".yaml")=11),
	// so slicing it out excludes the base config rather than reading it as "yaml".
	if len(fileName) <= len(configFilePrefix)+len(configFileSuffix) {
		return "", false
	}

	name := fileName[len(configFilePrefix) : len(fileName)-len(configFileSuffix)]

	if v1alpha1.ValidateClusterName(name) != nil {
		return "", false
	}

	return name, true
}
