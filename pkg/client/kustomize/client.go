package kustomize

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	fluxkustomize "github.com/fluxcd/pkg/kustomize"
	fluxfilesys "github.com/fluxcd/pkg/kustomize/filesys"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// Client provides kustomize build functionality.
type Client struct{}

// NewClient creates a new kustomize client.
func NewClient() *Client {
	return &Client{}
}

// Build runs kustomize build on the specified directory and returns the output.
func (c *Client) Build(ctx context.Context, path string) (*bytes.Buffer, error) {
	return build(ctx, filesys.MakeFsOnDisk(), path)
}

// buildGate admits one build at a time ahead of Flux's own lock. Flux acquires
// that lock without a context, so a caller canceled while it waits would still
// build once the lock frees; waiting here instead lets it give up.
//
//nolint:gochecknoglobals // guards Flux's process-wide build lock
var buildGate = make(chan struct{}, 1)

// admitBuild waits for the build gate or for ctx to end, and returns the
// release function on admission.
func admitBuild(ctx context.Context) (func(), error) {
	select {
	case buildGate <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for kustomize build: %w", ctx.Err())
	}

	// The gate and ctx can both be ready; never start a build for a canceled caller.
	err := ctx.Err()
	if err != nil {
		<-buildGate

		return nil, fmt.Errorf("wait for kustomize build: %w", err)
	}

	return func() { <-buildGate }, nil
}

// build runs one kustomize build through Flux's own Build, the one
// kustomize-controller uses. kustomize keeps its OpenAPI schema in package
// globals that a kustomization's `openapi` field replaces and that are not safe
// for concurrent use (#5858), so Flux serializes every build behind one lock
// and resets the schema to the built-in one before and after each build. Every
// build here goes through it, so one folder's custom schema never reaches
// another folder's build and two builds never touch the schema at once.
// Callers are admitted through buildGate first, so a canceled one stops
// waiting instead of building late.
func build(ctx context.Context, fSys filesys.FileSystem, path string) (*bytes.Buffer, error) {
	release, err := admitBuild(ctx)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", path, err)
	}
	defer release()

	resMap, err := fluxkustomize.Build(fSys, path)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", path, err)
	}

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

	fSys, dir, err := fluxBuildFS(root, dir, kustomization)
	if err != nil {
		return nil, err
	}

	return build(ctx, fSys, dir)
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
