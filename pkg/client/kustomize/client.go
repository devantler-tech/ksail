package kustomize

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	fluxkustomize "github.com/fluxcd/pkg/kustomize"
	fluxfilesys "github.com/fluxcd/pkg/kustomize/filesys"
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
// disk is read outside the build or written. As in kustomize-controller,
// generation and every file the build reads are confined to root, the source
// the Kustomization reads, so a component or resource outside it fails here as
// it fails in the cluster.
func (c *Client) BuildFlux(
	ctx context.Context,
	root string,
	dir string,
	kustomization map[string]any,
) (*bytes.Buffer, error) {
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", dir, err)
	}

	warmSchema()

	fSys, dir, err := fluxBuildFS(root, dir, kustomization)
	if err != nil {
		return nil, err
	}

	err = ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", dir, err)
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

// fluxBuildFS generates the kustomization Flux builds for dir and returns the
// filesystem serving it, confined to root, with dir resolved to the absolute
// path kustomize builds.
func fluxBuildFS(
	root string,
	dir string,
	kustomization map[string]any,
) (filesys.FileSystem, string, error) {
	sourceRoot, err := fsutil.EvalCanonicalPath(root)
	if err != nil {
		return nil, "", fmt.Errorf("resolve source root %s: %w", root, err)
	}

	// kustomize resolves the folder to an absolute path through symlinks, so the
	// overlaid file must be named by the same absolute, resolved path; a relative
	// dir such as "." would otherwise leave the overlay unmatched.
	resolved, err := fsutil.EvalCanonicalPath(dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve %s: %w", dir, err)
	}

	generator := fluxkustomize.NewGenerator(
		sourceRoot, unstructured.Unstructured{Object: kustomization},
	)

	manifest, kustFile, _, err := generator.GenerateManifest(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("generate Flux kustomization for %s: %w", resolved, err)
	}

	secure, err := fluxfilesys.MakeFsOnDiskSecureBuild(sourceRoot)
	if err != nil {
		return nil, "", fmt.Errorf("confine build to %s: %w", sourceRoot, err)
	}

	return overlayFS{
		FileSystem: secure,
		path:       filepath.Clean(kustFile),
		data:       manifest,
	}, resolved, nil
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
