package environment

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
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
)

// ErrDiscoverEnvironments is returned by DeriveEnvironments when the workspace
// directory itself cannot be read (a per-environment config that fails to load is
// skipped, not surfaced as this error).
var ErrDiscoverEnvironments = errors.New("failed to discover environments")

// Environment is the read-side model of a cluster environment declared in a ksail
// workspace by a ksail.<name>.yaml root config. It is the enumeration foundation
// for the multi-cluster layout: higher layers list, validate a --from against, or
// (later) reconcile the clusters/ tree from the declared set (issue #5441 item 3).
type Environment struct {
	// Name is the environment identifier — the <name> in ksail.<name>.yaml, a
	// DNS-1123 label. It is also the clusters/<name>/ overlay directory segment.
	Name string
	// ConfigFile is the root config file name (ksail.<name>.yaml), relative to the
	// workspace root.
	ConfigFile string
	// Distribution is the cluster distribution declared by the config (Vanilla,
	// K3s, Talos, …).
	Distribution v1alpha1.Distribution
	// Provider is the cluster provider declared by the config (Docker, Hetzner, …).
	Provider v1alpha1.Provider
}

// ConfigLoader loads the ksail config declared by the given workspace-relative
// configFile (e.g. "ksail.prod.yaml"). It is injected so this package stays free of
// a config-manager dependency and DeriveEnvironments is unit-testable without the
// filesystem; the CLI wires the real ksail config manager.
type ConfigLoader func(configFile string) (*v1alpha1.Cluster, error)

// DeriveEnvironments enumerates the environments declared in repoRoot by their
// ksail.<name>.yaml root configs, loading each through load to read its declared
// distribution and provider. The base ksail.yaml (no <name> segment) is not an
// environment and is excluded, as is any file whose <name> is not a DNS-1123 label.
// A config that fails to load is skipped — a single malformed file must not hide the
// environments that do load — so the result lists every usable declared environment,
// sorted by name. Only an unreadable repoRoot yields an error (ErrDiscoverEnvironments).
func DeriveEnvironments(repoRoot string, load ConfigLoader) ([]Environment, error) {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDiscoverEnvironments, err)
	}

	environments := make([]Environment, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name, ok := environmentNameFromConfigFile(entry.Name())
		if !ok {
			continue
		}

		cfg, loadErr := load(entry.Name())
		if loadErr != nil {
			// A malformed config is not a usable environment and must not hide the
			// rest, but leave a debug trace so a missing environment entry is
			// troubleshootable rather than silently absent.
			slog.Default().Debug(
				"skipping unloadable environment config",
				"file", entry.Name(),
				"error", loadErr,
			)

			continue
		}

		environments = append(environments, Environment{
			Name:         name,
			ConfigFile:   entry.Name(),
			Distribution: cfg.Spec.Cluster.Distribution,
			Provider:     cfg.Spec.Cluster.Provider,
		})
	}

	slices.SortFunc(environments, func(left, right Environment) int {
		return strings.Compare(left.Name, right.Name)
	})

	return environments, nil
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
