// Package flux provides generators for Flux GitOps resources.
package flux

import (
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/io/detector"
	"github.com/devantler-tech/ksail/v5/pkg/io/generator"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultInterval is the default reconciliation interval for Instance.
const DefaultInterval = time.Minute

// Instance represents a Flux Operator FluxInstance CR for scaffolding.
// This is a simplified version for YAML generation, without runtime.Object methods.
type Instance struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind" yaml:"kind"`
	Metadata   InstanceMetadata `json:"metadata" yaml:"metadata"`
	Spec       InstanceSpec     `json:"spec" yaml:"spec"`
}

// InstanceMetadata contains the metadata for a FluxInstance.
type InstanceMetadata struct {
	Name      string            `json:"name" yaml:"name"`
	Namespace string            `json:"namespace" yaml:"namespace"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// InstanceSpec contains the distribution and sync configuration.
type InstanceSpec struct {
	Distribution Distribution `json:"distribution" yaml:"distribution"`
	Sync         *Sync        `json:"sync,omitempty" yaml:"sync,omitempty"`
}

// Distribution references the Flux manifests and controller images.
type Distribution struct {
	Version  string `json:"version" yaml:"version"`
	Registry string `json:"registry" yaml:"registry"`
}

// Sync configures the OCI source that Flux will track and apply.
type Sync struct {
	Kind     string           `json:"kind" yaml:"kind"`
	URL      string           `json:"url" yaml:"url"`
	Ref      string           `json:"ref" yaml:"ref"`
	Path     string           `json:"path" yaml:"path"`
	Interval *metav1.Duration `json:"interval,omitempty" yaml:"interval,omitempty"`
}

// InstanceGeneratorOptions contains options for generating FluxInstance.
type InstanceGeneratorOptions struct {
	yamlgenerator.Options

	// ProjectName is used to construct the OCI registry URL.
	ProjectName string
	// RegistryHost is the host of the local OCI registry.
	RegistryHost string
	// RegistryPort is the port of the local OCI registry.
	RegistryPort int32
	// Interval is the reconciliation interval.
	Interval time.Duration
}

// InstanceGenerator generates FluxInstance CR manifests.
type InstanceGenerator struct {
	yamlGenerator generator.Generator[Instance, yamlgenerator.Options]
}

// NewInstanceGenerator creates a new InstanceGenerator.
func NewInstanceGenerator() *InstanceGenerator {
	return &InstanceGenerator{
		yamlGenerator: yamlgenerator.NewYAMLGenerator[Instance](),
	}
}

// Generate creates a FluxInstance CR manifest.
func (g *InstanceGenerator) Generate(opts InstanceGeneratorOptions) (string, error) {
	interval := opts.Interval
	if interval == 0 {
		interval = DefaultInterval
	}

	instance := Instance{
		APIVersion: detector.FluxInstanceAPIVersion,
		Kind:       detector.FluxInstanceKind,
		Metadata: InstanceMetadata{
			Name:      detector.FluxInstanceDefaultName,
			Namespace: detector.FluxInstanceNamespace,
			Labels: map[string]string{
				detector.ManagedByLabel: detector.ManagedByValue,
			},
		},
		Spec: InstanceSpec{
			Distribution: Distribution{
				Version:  "2.x",
				Registry: "ghcr.io/fluxcd",
			},
			Sync: &Sync{
				Kind: "OCIRepository",
				URL: generator.BuildOCIURL(
					opts.RegistryHost,
					opts.RegistryPort,
					opts.ProjectName,
				),
				Ref:      "latest",
				Path:     ".",
				Interval: &metav1.Duration{Duration: interval},
			},
		},
	}

	output, err := g.yamlGenerator.Generate(instance, opts.Options)
	if err != nil {
		return "", fmt.Errorf("generating FluxInstance manifest: %w", err)
	}

	return output, nil
}
