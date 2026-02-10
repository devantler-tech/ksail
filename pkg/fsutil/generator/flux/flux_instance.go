package flux

import (
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil/generator"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/svc/detector/gitops"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultInterval is the default reconciliation interval for Instance.
const DefaultInterval = time.Minute

// Instance represents a Flux Operator Instance CR for scaffolding.
// This is a simplified version for YAML generation, without runtime.Object methods.
type Instance struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind"       yaml:"kind"`
	Metadata   InstanceMetadata `json:"metadata"   yaml:"metadata"`
	Spec       InstanceSpec     `json:"spec"       yaml:"spec"`
}

// InstanceMetadata contains the metadata for a Instance.
type InstanceMetadata struct {
	Name      string            `json:"name"             yaml:"name"`
	Namespace string            `json:"namespace"        yaml:"namespace"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// InstanceSpec contains the distribution and sync configuration.
type InstanceSpec struct {
	Distribution Distribution `json:"distribution"   yaml:"distribution"`
	Sync         *Sync        `json:"sync,omitempty" yaml:"sync,omitempty"`
}

// Distribution references the Flux manifests and controller images.
type Distribution struct {
	Version  string `json:"version"  yaml:"version"`
	Registry string `json:"registry" yaml:"registry"`
}

// Sync configures the OCI source that Flux will track and apply.
type Sync struct {
	Kind       string           `json:"kind"                 yaml:"kind"`
	URL        string           `json:"url"                  yaml:"url"`
	Ref        string           `json:"ref"                  yaml:"ref"`
	Path       string           `json:"path"                 yaml:"path"`
	Interval   *metav1.Duration `json:"interval,omitempty"   yaml:"interval,omitempty"`
	PullSecret string           `json:"pullSecret,omitempty" yaml:"pullSecret,omitempty"`
}

// InstanceGeneratorOptions contains options for generating Instance.
type InstanceGeneratorOptions struct {
	yamlgenerator.Options

	// ProjectName is used to construct the OCI registry URL.
	ProjectName string
	// RegistryHost is the host of the local OCI registry.
	RegistryHost string
	// RegistryPort is the port of the local OCI registry.
	RegistryPort int32
	// Ref is the OCI artifact tag/reference (defaults to "dev").
	Ref string
	// Interval is the reconciliation interval.
	Interval time.Duration
	// SecretName is the name of the Kubernetes secret containing registry credentials.
	// If set, a secretRef will be added to the sync configuration.
	SecretName string
}

// InstanceGenerator generates Instance CR manifests.
type InstanceGenerator struct {
	yamlGenerator generator.Generator[Instance, yamlgenerator.Options]
}

// NewInstanceGenerator creates a new InstanceGenerator.
func NewInstanceGenerator() *InstanceGenerator {
	return &InstanceGenerator{
		yamlGenerator: yamlgenerator.NewYAMLGenerator[Instance](),
	}
}

// Generate creates a Instance CR manifest.
func (g *InstanceGenerator) Generate(opts InstanceGeneratorOptions) (string, error) {
	interval := opts.Interval
	if interval == 0 {
		interval = DefaultInterval
	}

	ref := opts.Ref
	if ref == "" {
		ref = "dev"
	}

	sync := &Sync{
		Kind: "OCIRepository",
		URL: generator.BuildOCIURL(
			opts.RegistryHost,
			opts.RegistryPort,
			opts.ProjectName,
		),
		Ref:        ref,
		Path:       ".",
		Interval:   &metav1.Duration{Duration: interval},
		PullSecret: opts.SecretName,
	}

	instance := Instance{
		APIVersion: gitops.FluxInstanceAPIVersion,
		Kind:       gitops.FluxInstanceKind,
		Metadata: InstanceMetadata{
			Name:      gitops.FluxInstanceDefaultName,
			Namespace: gitops.FluxInstanceNamespace,
			Labels: map[string]string{
				gitops.ManagedByLabel: gitops.ManagedByValue,
			},
		},
		Spec: InstanceSpec{
			Distribution: Distribution{
				Version:  "2.x",
				Registry: "ghcr.io/fluxcd",
			},
			Sync: sync,
		},
	}

	output, err := g.yamlGenerator.Generate(instance, opts.Options)
	if err != nil {
		return "", fmt.Errorf("generating Instance manifest: %w", err)
	}

	return output, nil
}
