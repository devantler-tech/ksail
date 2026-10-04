// Package eksupgrade prepares the disposable EKS upgrade evaluation project.
package eksupgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

var errProject = errors.New("upgrade trial requires a scaffolded EKS project and a 1.MINOR version")

const privateFileMode = 0o600

// Prepare pins the creation version and enables control-plane upgrades while
// retaining the project's IAM boundaries, node groups, and GitOps configuration.
func Prepare(dir, version string) error {
	if !regexp.MustCompile(`^1\.[1-9][0-9]?$`).MatchString(version) {
		return errProject
	}

	config, err := readDocument(filepath.Join(dir, "ksail.yaml"))
	if err != nil {
		return err
	}

	spec, _ := config["spec"].(map[string]any)

	cluster, _ := spec["cluster"].(map[string]any)
	if cluster["distribution"] != "EKS" {
		return errProject
	}

	eks, err := readDocument(filepath.Join(dir, "eks.yaml"))
	if err != nil {
		return err
	}

	metadata, _ := eks["metadata"].(map[string]any)
	if metadata == nil {
		return errProject
	}

	options, ok := cluster["eks"].(map[string]any)
	if !ok {
		if cluster["eks"] != nil {
			return errProject
		}

		options = make(map[string]any)
		cluster["eks"] = options
	}

	metadata["version"] = version
	options["experimentalControlPlaneUpgrade"] = true

	err = writeDocument(filepath.Join(dir, "eks.yaml"), eks)
	if err != nil {
		return err
	}

	return writeDocument(filepath.Join(dir, "ksail.yaml"), config)
}

// readDocument validates the full input before either project file is changed.
func readDocument(path string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read trial configuration: %w", err)
	}

	var document map[string]any

	err = yaml.Unmarshal(data, &document)
	if err != nil {
		return nil, fmt.Errorf("decode trial configuration: %w", err)
	}

	return document, nil
}

// writeDocument persists the edited YAML with owner-only access.
func writeDocument(path string, document map[string]any) error {
	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode trial configuration: %w", err)
	}

	err = os.WriteFile(path, data, privateFileMode)
	if err != nil {
		return fmt.Errorf("write trial configuration: %w", err)
	}

	return nil
}
