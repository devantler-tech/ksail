package scaffolder

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
)

// DevcontainerDir is the directory holding the generated Dev Container definition.
const DevcontainerDir = ".devcontainer"

// DevcontainerConfigFile is the filename of the generated Dev Container definition.
const DevcontainerConfigFile = "devcontainer.json"

// defaultDevcontainerName is the Dev Container display name used when no cluster
// name override is provided.
const defaultDevcontainerName = "ksail"

// devcontainerJSONTemplate is the scaffolded Dev Container definition for a KSail
// project. It targets GitHub Codespaces / VS Code Dev Containers and other
// OCI-compatible ephemeral environments so a contributor can go from "open repo"
// to "ksail cluster create" with zero local setup. The single %s is the JSON-quoted
// container name.
//
// The definition deliberately covers the three things a KSail project needs at
// runtime: Docker-in-Docker (KSail runs Kind/K3d/Talos node containers inside the
// dev container), kubectl + Helm to interact with the provisioned cluster, and the
// KSail CLI itself installed via `go install` (the cross-platform install method —
// the Homebrew cask is macOS-only) so it lands on PATH.
const devcontainerJSONTemplate = `{
  "name": %s,
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "features": {
    "ghcr.io/devcontainers/features/docker-in-docker:2": {},
    "ghcr.io/devcontainers/features/go:1": {},
    "ghcr.io/devcontainers/features/kubectl-helm-minikube:1": {
      "minikube": "none"
    }
  },
  "postCreateCommand": "go install github.com/devantler-tech/ksail/v7@latest",
  "remoteEnv": {
    "PATH": "${containerEnv:PATH}:${containerEnv:HOME}/go/bin"
  }
}
`

// devcontainerGenerator emits the static Dev Container definition. It satisfies the
// generator.Generator contract used by generateWithFileHandling so Dev Container
// scaffolding shares the same force/skip/notify handling as the other files.
type devcontainerGenerator struct {
	content string
}

// Generate writes the Dev Container definition to opts.Output (or returns it when
// no output path is set), mirroring schemaHeaderGenerator's write semantics.
func (g *devcontainerGenerator) Generate(
	_ struct{},
	opts yamlgenerator.Options,
) (string, error) {
	if opts.Output == "" {
		return g.content, nil
	}

	result, err := fsutil.TryWriteFile(g.content, opts.Output, opts.Force)
	if err != nil {
		return "", fmt.Errorf("failed to write devcontainer.json: %w", err)
	}

	return result, nil
}

// generateDevcontainerConfig generates .devcontainer/devcontainer.json so the
// scaffolded project is immediately usable in Codespaces / Dev Containers.
func (s *Scaffolder) generateDevcontainerConfig(output string, force bool) error {
	content := fmt.Sprintf(devcontainerJSONTemplate, strconv.Quote(s.devcontainerName()))

	displayName := filepath.Join(DevcontainerDir, DevcontainerConfigFile)
	opts := yamlgenerator.Options{
		Output: filepath.Join(output, DevcontainerDir, DevcontainerConfigFile),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[struct{}]{
			Gen:         &devcontainerGenerator{content: content},
			Model:       struct{}{},
			Opts:        opts,
			DisplayName: displayName,
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrDevcontainerGeneration, err)
			},
		},
	)
}

// devcontainerName returns the Dev Container display name: the cluster name override
// when set, otherwise the default. It is cosmetic (shown in the editor UI).
func (s *Scaffolder) devcontainerName() string {
	if s.ClusterName != "" {
		return s.ClusterName
	}

	return defaultDevcontainerName
}
