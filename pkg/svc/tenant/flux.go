package tenant

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
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
	Interval           string                     `json:"interval"`
	RetryInterval      string                     `json:"retryInterval,omitempty"`
	SourceRef          fluxKustomizationSourceRef `json:"sourceRef"`
	Path               string                     `json:"path"`
	Prune              bool                       `json:"prune"`
	Wait               bool                       `json:"wait,omitempty"`
	Timeout            string                     `json:"timeout,omitempty"`
	TargetNamespace    string                     `json:"targetNamespace"`
	ServiceAccountName string                     `json:"serviceAccountName"`
	Decryption         *fluxDecryption            `json:"decryption,omitempty"`
}

type fluxDecryption struct {
	Provider  string            `json:"provider"`
	SecretRef map[string]string `json:"secretRef,omitempty"`
}

type fluxKustomization struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Metadata   fluxMetadata          `json:"metadata"`
	Spec       fluxKustomizationSpec `json:"spec"`
}

// resolveProviderHost delegates to the shared gitprovider helper so
// provider mappings remain centralized and do not drift between tenant integrations.
func resolveProviderHost(provider string) string {
	return gitprovider.ResolveProviderHost(provider)
}

// GenerateFluxSyncManifests generates Flux-specific sync manifests.
// Returns a map of filename -> YAML content.
// Files: sync.yaml (multi-doc: source CR + Kustomization CR).
func GenerateFluxSyncManifests(opts Options) (map[string]string, error) {
	if len(opts.Namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	primaryNS := opts.Namespaces[0]

	srcDir := opts.SourceDirectory
	if srcDir == "" {
		srcDir = DefaultSourceDirectory
	}

	source, err := buildFluxSource(opts, primaryNS)
	if err != nil {
		return nil, err
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
			Path:               "./" + srcDir,
			Prune:              true,
			TargetNamespace:    primaryNS,
			ServiceAccountName: opts.Name,
		},
	}

	applyFluxHardening(&kustomization.Spec, opts)

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

// applyFluxHardening sets the optional production hardening fields on the Flux
// Kustomization spec based on the tenant options. All fields are no-ops when
// their flag is unset, keeping default output unchanged. A non-empty
// FluxTimeout implies waiting, so the timeout takes effect even when callers
// set it via the svc API without FluxWait.
func applyFluxHardening(spec *fluxKustomizationSpec, opts Options) {
	if opts.FluxWait || opts.FluxTimeout != "" {
		spec.Wait = true

		timeout := opts.FluxTimeout
		if timeout == "" {
			timeout = DefaultFluxTimeout
		}

		spec.Timeout = timeout
	}

	if opts.FluxRetryInterval != "" {
		spec.RetryInterval = opts.FluxRetryInterval
	}

	if opts.FluxDecryption {
		spec.Decryption = &fluxDecryption{
			Provider:  "sops",
			SecretRef: map[string]string{"name": "sops-age"},
		}
	}
}

func buildFluxSource(opts Options, primaryNS string) (fluxSource, error) {
	switch opts.SyncSource {
	case SyncSourceOCI:
		return buildFluxOCISource(opts, primaryNS)
	case SyncSourceGit:
		return buildFluxGitSource(opts, primaryNS)
	default:
		return fluxSource{}, fmt.Errorf("%w: %s", ErrUnsupportedSyncSource, opts.SyncSource)
	}
}

func buildFluxOCISource(opts Options, primaryNS string) (fluxSource, error) {
	if opts.Registry == "" {
		return fluxSource{}, fmt.Errorf("%w", ErrRegistryRequired)
	}

	if opts.TenantRepo == "" {
		return fluxSource{}, fmt.Errorf("%w for Flux OCI sync source", ErrTenantRepoRequired)
	}

	owner, repo, err := gitprovider.ParseOwnerRepo(opts.TenantRepo)
	if err != nil {
		return fluxSource{}, fmt.Errorf("parsing git repo for OCI source: %w", err)
	}

	registry := strings.TrimSuffix(opts.Registry, "/")

	url := fmt.Sprintf("%s/%s/%s", registry, owner, repo)
	if ociPath := strings.Trim(opts.OCIPath, "/"); ociPath != "" {
		url += "/" + ociPath
	}

	return fluxSource{
		APIVersion: "source.toolkit.fluxcd.io/v1",
		Kind:       "OCIRepository",
		Metadata: fluxMetadata{
			Name:      opts.Name,
			Namespace: primaryNS,
			Labels:    ManagedByLabels(),
		},
		Spec: fluxSourceSpec{
			Interval: "1m",
			URL:      url,
			Ref:      fluxSourceRef{Tag: "latest"},
		},
	}, nil
}

func buildFluxGitSource(opts Options, primaryNS string) (fluxSource, error) {
	if opts.GitProvider == "" {
		return fluxSource{}, fmt.Errorf("%w for Flux Git sync source", ErrGitProviderRequired)
	}

	if opts.TenantRepo == "" {
		return fluxSource{}, fmt.Errorf("%w for Flux Git sync source", ErrTenantRepoRequired)
	}

	_, _, parseErr := gitprovider.ParseOwnerRepo(opts.TenantRepo)
	if parseErr != nil {
		return fluxSource{}, fmt.Errorf("parsing git repo for Git source: %w", parseErr)
	}

	host := resolveProviderHost(opts.GitProvider)

	return fluxSource{
		APIVersion: "source.toolkit.fluxcd.io/v1",
		Kind:       "GitRepository",
		Metadata: fluxMetadata{
			Name:      opts.Name,
			Namespace: primaryNS,
			Labels:    ManagedByLabels(),
		},
		Spec: fluxSourceSpec{
			Interval: "1m",
			URL:      fmt.Sprintf("https://%s/%s", host, opts.TenantRepo),
			Ref:      fluxSourceRef{Branch: "main"},
		},
	}, nil
}
