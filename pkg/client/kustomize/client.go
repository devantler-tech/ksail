package kustomize

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"

	fluxkustomize "github.com/fluxcd/pkg/kustomize"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// Client provides kustomize build functionality.
type Client struct{}

// NewClient creates a new kustomize client.
func NewClient() *Client {
	return &Client{}
}

// schemaWarmOnce serializes the first kyaml openapi builtin-schema
// initialization across concurrent builds. See warmSchema and
// https://github.com/devantler-tech/ksail/issues/5858.
var schemaWarmOnce sync.Once //nolint:gochecknoglobals

// warmSchema forces kustomize's kyaml openapi builtin-schema package globals to
// initialize once, serially, before any concurrent build reads them.
//
// The first kustomize build lazily initializes those globals (schema parse plus
// the namespaceability map). That init is not goroutine-safe: kyaml writes the
// namespaceability map under a lock inside findNamespaceability, but
// IsNamespaceScoped reads the same map without the lock — so a second concurrent
// build racing the first's init trips the race detector (and can read a
// half-populated map). Running one SchemaForResourceType call under a sync.Once
// forces the full init (findNamespaceability included) exactly once; after it
// every concurrent build only ever reads a settled, no-longer-mutated global.
// This covers the default built-in schema that ksail always uses.
func warmSchema() {
	schemaWarmOnce.Do(func() {
		_ = openapi.SchemaForResourceType(
			kyaml.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		)
	})
}

// Build runs kustomize build on the specified directory and returns the output.
func (c *Client) Build(_ context.Context, path string) (*bytes.Buffer, error) {
	// Initialize the shared kyaml openapi schema once before building, so
	// concurrent Build calls never race its lazy first-init (#5858).
	warmSchema()

	// Create a file system abstraction
	fSys := filesys.MakeFsOnDisk()

	// Create kustomize options with load restrictions disabled
	opts := krusty.MakeDefaultOptions()
	opts.LoadRestrictions = types.LoadRestrictionsNone

	// Create a kustomizer
	k := krusty.MakeKustomizer(opts)

	// Run the build
	resMap, err := k.Run(fSys, path)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", path, err)
	}

	// Convert resource map to YAML
	yaml, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("convert resources to yaml: %w", err)
	}

	return bytes.NewBuffer(yaml), nil
}

// BuildFlux builds dir the way Flux's kustomize-controller builds the folder a
// Flux Kustomization applies. Flux's own generator merges the Kustomization's
// spec (targetNamespace, namePrefix, patches, images, components, ...) into the
// folder's kustomization file, or generates one listing the folder's manifests
// when it has none, and that single kustomize build then runs every transformer
// in kustomize's own order. The merged file is served from memory, so nothing on
// disk is read outside the build or written.
func (c *Client) BuildFlux(
	_ context.Context,
	dir string,
	kustomization map[string]any,
) (*bytes.Buffer, error) {
	warmSchema()

	// kustomize resolves the folder through symlinks, so the overlaid file must
	// be named by the same resolved path.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}

	dir = resolved

	generator := fluxkustomize.NewGenerator("", unstructured.Unstructured{Object: kustomization})

	manifest, kustFile, _, err := generator.GenerateManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("generate Flux kustomization for %s: %w", dir, err)
	}

	fSys := overlayFS{
		FileSystem: filesys.MakeFsOnDisk(),
		path:       filepath.Clean(kustFile),
		data:       manifest,
	}

	opts := krusty.MakeDefaultOptions()
	opts.LoadRestrictions = types.LoadRestrictionsNone

	resMap, err := krusty.MakeKustomizer(opts).Run(fSys, dir)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", dir, err)
	}

	yaml, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("convert resources to yaml: %w", err)
	}

	return bytes.NewBuffer(yaml), nil
}

// overlayFS is the on-disk filesystem with one file, the merged kustomization
// file, served from memory in place of whatever is (or is not) on disk.
type overlayFS struct {
	filesys.FileSystem

	path string
	data []byte
}

// ReadFile returns the overlaid file's content for its path.
func (o overlayFS) ReadFile(path string) ([]byte, error) {
	if filepath.Clean(path) == o.path {
		return bytes.Clone(o.data), nil
	}

	data, err := o.FileSystem.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return data, nil
}

// Exists reports the overlaid file as present.
func (o overlayFS) Exists(path string) bool {
	return filepath.Clean(path) == o.path || o.FileSystem.Exists(path)
}

// IsDir reports the overlaid file as a file.
func (o overlayFS) IsDir(path string) bool {
	return filepath.Clean(path) != o.path && o.FileSystem.IsDir(path)
}
