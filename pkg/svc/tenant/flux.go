package tenant

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

type fluxMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels"`
}

type fluxSourceRef struct {
	Tag    string `json:"tag,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type fluxSourceSpec struct {
	Interval string        `json:"interval"`
	URL      string        `json:"url"`
	Ref      fluxSourceRef `json:"ref"`
}

type fluxSource struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   fluxMetadata   `json:"metadata"`
	Spec       fluxSourceSpec `json:"spec"`
}

type fluxKustomizationSourceRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type fluxKustomizationSpec struct {
	Interval        string                     `json:"interval"`
	SourceRef       fluxKustomizationSourceRef `json:"sourceRef"`
	Path            string                     `json:"path"`
	Prune           bool                       `json:"prune"`
	TargetNamespace string                     `json:"targetNamespace"`
}

type fluxKustomization struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Metadata   fluxMetadata          `json:"metadata"`
	Spec       fluxKustomizationSpec `json:"spec"`
}

// resolveProviderHost maps a git provider name to its hostname.
func resolveProviderHost(provider string) string {
	switch strings.ToLower(provider) {
	case "github":
		return "github.com"
	case "gitlab":
		return "gitlab.com"
	case "gitea":
		return "gitea.com"
	default:
		return provider
	}
}

// GenerateFluxSyncManifests generates Flux-specific sync manifests.
// Returns a map of filename -> YAML content.
// Files: sync.yaml (multi-doc: source CR + Kustomization CR)
func GenerateFluxSyncManifests(opts Options) (map[string]string, error) {
	primaryNS := opts.Namespaces[0]

	var source fluxSource
	switch opts.SyncSource {
	case SyncSourceOCI:
		owner, repo, err := parseOwnerRepo(opts.GitRepo)
		if err != nil {
			return nil, fmt.Errorf("parsing git repo for OCI source: %w", err)
		}
		registry := strings.TrimSuffix(opts.Registry, "/")
		source = fluxSource{
			APIVersion: "source.toolkit.fluxcd.io/v1",
			Kind:       "OCIRepository",
			Metadata: fluxMetadata{
				Name:      opts.Name,
				Namespace: primaryNS,
				Labels:    ManagedByLabels(),
			},
			Spec: fluxSourceSpec{
				Interval: "1m",
				URL:      fmt.Sprintf("%s/%s/%s", registry, owner, repo),
				Ref:      fluxSourceRef{Tag: "latest"},
			},
		}
	case SyncSourceGit:
		host := resolveProviderHost(opts.GitProvider)
		source = fluxSource{
			APIVersion: "source.toolkit.fluxcd.io/v1",
			Kind:       "GitRepository",
			Metadata: fluxMetadata{
				Name:      opts.Name,
				Namespace: primaryNS,
				Labels:    ManagedByLabels(),
			},
			Spec: fluxSourceSpec{
				Interval: "1m",
				URL:      fmt.Sprintf("https://%s/%s", host, opts.GitRepo),
				Ref:      fluxSourceRef{Branch: "main"},
			},
		}
	default:
		return nil, fmt.Errorf("unsupported sync source: %s", opts.SyncSource)
	}

	kustomization := fluxKustomization{
		APIVersion: "kustomize.toolkit.fluxcd.io/v1",
		Kind:       "Kustomization",
		Metadata: fluxMetadata{
			Name:      opts.Name,
			Namespace: primaryNS,
			Labels:    ManagedByLabels(),
		},
		Spec: fluxKustomizationSpec{
			Interval: "1m",
			SourceRef: fluxKustomizationSourceRef{
				Kind: source.Kind,
				Name: opts.Name,
			},
			Path:            "./k8s",
			Prune:           true,
			TargetNamespace: primaryNS,
		},
	}

	sourceYAML, err := yaml.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("marshaling flux source: %w", err)
	}
	kustomizationYAML, err := yaml.Marshal(kustomization)
	if err != nil {
		return nil, fmt.Errorf("marshaling flux kustomization: %w", err)
	}

	syncYAML := string(sourceYAML) + "---\n" + string(kustomizationYAML)

	return map[string]string{
		"sync.yaml": syncYAML,
	}, nil
}

// parseOwnerRepo splits "owner/repo-name" into owner and repo.
func parseOwnerRepo(gitRepo string) (string, string, error) {
	parts := strings.SplitN(gitRepo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid git-repo format: %q (expected owner/repo-name)", gitRepo)
	}
	return parts[0], parts[1], nil
}
