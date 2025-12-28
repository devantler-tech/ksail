// Package argocd provides generators for ArgoCD GitOps resources.
package argocd

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/io/detector"
	"github.com/devantler-tech/ksail/v5/pkg/io/generator"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
)

// Application represents an ArgoCD Application CR for scaffolding.
type Application struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Metadata   ApplicationMetadata `yaml:"metadata"`
	Spec       ApplicationSpec     `yaml:"spec"`
}

// ApplicationMetadata contains the metadata for an ArgoCD Application.
type ApplicationMetadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels,omitempty"`
}

// ApplicationSpec contains the source and destination configuration.
type ApplicationSpec struct {
	Project     string                 `yaml:"project"`
	Source      ApplicationSource      `yaml:"source"`
	Destination ApplicationDestination `yaml:"destination"`
	SyncPolicy  *SyncPolicy            `yaml:"syncPolicy,omitempty"`
}

// ApplicationSource defines where ArgoCD should fetch manifests from.
type ApplicationSource struct {
	RepoURL        string         `yaml:"repoUrl"`
	TargetRevision string         `yaml:"targetRevision"`
	Path           string         `yaml:"path,omitempty"`
	Directory      *DirectorySpec `yaml:"directory,omitempty"`
}

// DirectorySpec configures directory-based source options.
type DirectorySpec struct {
	Recurse bool `yaml:"recurse"`
}

// ApplicationDestination defines where ArgoCD should deploy resources.
type ApplicationDestination struct {
	Server    string `yaml:"server"`
	Namespace string `yaml:"namespace"`
}

// SyncPolicy defines automated sync behavior.
type SyncPolicy struct {
	Automated *AutomatedSync `yaml:"automated,omitempty"`
}

// AutomatedSync enables automatic syncing.
type AutomatedSync struct {
	Prune    bool `yaml:"prune"`
	SelfHeal bool `yaml:"selfHeal"`
}

// ApplicationGeneratorOptions contains options for generating ArgoCD Application.
type ApplicationGeneratorOptions struct {
	yamlgenerator.Options

	// ProjectName is used to construct the OCI registry URL.
	ProjectName string
	// RegistryHost is the host of the local OCI registry.
	RegistryHost string
	// RegistryPort is the port of the local OCI registry.
	RegistryPort int32
}

// ApplicationGenerator generates ArgoCD Application CR manifests.
type ApplicationGenerator struct {
	yamlGenerator generator.Generator[Application, yamlgenerator.Options]
}

// NewApplicationGenerator creates a new ApplicationGenerator.
func NewApplicationGenerator() *ApplicationGenerator {
	return &ApplicationGenerator{
		yamlGenerator: yamlgenerator.NewYAMLGenerator[Application](),
	}
}

// Generate creates an ArgoCD Application CR manifest.
func (g *ApplicationGenerator) Generate(opts ApplicationGeneratorOptions) (string, error) {
	app := Application{
		APIVersion: detector.ArgoCDApplicationAPIVersion,
		Kind:       detector.ArgoCDApplicationKind,
		Metadata: ApplicationMetadata{
			Name:      detector.ArgoCDApplicationDefaultName,
			Namespace: detector.ArgoCDNamespace,
			Labels: map[string]string{
				detector.ManagedByLabel: detector.ManagedByValue,
			},
		},
		Spec: ApplicationSpec{
			Project: "default",
			Source: ApplicationSource{
				RepoURL:        generator.BuildOCIURL(opts.RegistryHost, opts.RegistryPort, opts.ProjectName),
				TargetRevision: "latest",
				Path:           ".",
				Directory: &DirectorySpec{
					Recurse: true,
				},
			},
			Destination: ApplicationDestination{
				Server:    "https://kubernetes.default.svc",
				Namespace: "default",
			},
			SyncPolicy: &SyncPolicy{
				Automated: &AutomatedSync{
					Prune:    true,
					SelfHeal: true,
				},
			},
		},
	}

	output, err := g.yamlGenerator.Generate(app, opts.Options)
	if err != nil {
		return "", fmt.Errorf("generating ArgoCD Application manifest: %w", err)
	}

	return output, nil
}
