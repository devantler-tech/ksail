package kustomize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"sigs.k8s.io/kustomize/api/konfig"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/provider"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// ErrNonLocalComponent is returned when a Flux Kustomization names a component
// that is not a local, relative path, which kustomize-controller refuses too.
var ErrNonLocalComponent = errors.New("component path must be local and relative")

// placeholderNamespace keeps a generated kustomization that lists no resources
// buildable, as kustomize-controller does.
const placeholderNamespace = "_placeholder"

// FluxComposition is what a Flux Kustomization adds to the folder it applies
// before the folder is built: the spec fields kustomize-controller merges into
// the folder's kustomization file.
type FluxComposition struct {
	TargetNamespace         string
	NamePrefix              string
	NameSuffix              string
	Patches                 []types.Patch
	Images                  []types.Image
	Components              []string
	IgnoreMissingComponents bool
	BuildMetadata           []string
}

// BuildFlux builds dir the way Flux's kustomize-controller builds the folder a
// Flux Kustomization applies. The composition is merged into the folder's
// kustomization file, or into one generated from the folder's manifests when it
// has none, and a single kustomize build then runs every transformer in
// kustomize's own order, so a Flux patch sees exactly the resources it sees in
// the cluster. The merged file is served from memory: nothing is written.
func (c *Client) BuildFlux(
	_ context.Context,
	dir string,
	composition FluxComposition,
) (*bytes.Buffer, error) {
	warmSchema()

	// kustomize resolves the folder to an absolute path without symlinks, so
	// the overlaid file must be named by the same canonical path.
	canonical, err := fsutil.EvalCanonicalPath(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}

	kustFile, kust, err := loadOrGenerateKustomization(canonical)
	if err != nil {
		return nil, err
	}

	err = mergeFluxComposition(canonical, kust, composition)
	if err != nil {
		return nil, err
	}

	manifest, err := kyaml.Marshal(kust)
	if err != nil {
		return nil, fmt.Errorf("marshal Flux kustomization for %s: %w", canonical, err)
	}

	fSys := overlayFS{FileSystem: filesys.MakeFsOnDisk(), path: kustFile, data: manifest}

	opts := krusty.MakeDefaultOptions()
	opts.LoadRestrictions = types.LoadRestrictionsNone

	resMap, err := krusty.MakeKustomizer(opts).Run(fSys, canonical)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", canonical, err)
	}

	yaml, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("convert resources to yaml: %w", err)
	}

	return bytes.NewBuffer(yaml), nil
}

// loadOrGenerateKustomization returns the path and content of dir's
// kustomization file, or of the one Flux generates when dir has none.
func loadOrGenerateKustomization(dir string) (string, *types.Kustomization, error) {
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		path := filepath.Join(dir, name)

		data, err := fsutil.ReadFileSafe(dir, path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", path, err)
		}

		var kust types.Kustomization

		err = kust.Unmarshal(data)
		if err != nil {
			return "", nil, fmt.Errorf("parse %s: %w", path, err)
		}

		if len(kust.Resources) == 0 {
			// An empty kustomization still builds in kustomize-controller.
			kust.BuildMetadata = []string{types.OriginAnnotations}
		}

		return path, &kust, nil
	}

	resources, err := scanFluxManifests(dir)
	if err != nil {
		return "", nil, err
	}

	kust := &types.Kustomization{
		APIVersion: types.KustomizationVersion, Kind: types.KustomizationKind,
		Resources: resources,
	}
	if len(resources) == 0 {
		kust.Namespace = placeholderNamespace
	}

	return filepath.Join(dir, konfig.DefaultKustomizationFileName()), kust, nil
}

// scanFluxManifests lists what Flux's generated kustomization includes: every
// sub-folder holding a kustomization file (not descended into) and every YAML
// file that decodes as Kubernetes resources, as paths relative to dir.
func scanFluxManifests(dir string) ([]string, error) {
	factory := provider.NewDefaultDepProvider().GetResourceFactory()

	var resources []string

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == dir {
			return nil
		}

		rel := "./" + filepath.ToSlash(strings.TrimPrefix(path, dir+string(filepath.Separator)))

		if entry.IsDir() {
			if hasKustomizationFile(path) {
				resources = append(resources, rel)

				return filepath.SkipDir
			}

			return nil
		}

		manifest, err := isFluxManifest(factory, dir, path)
		if manifest {
			resources = append(resources, rel)
		}

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("generate kustomization for %s: %w", dir, err)
	}

	return resources, nil
}

// hasKustomizationFile reports whether dir holds a kustomization file.
func hasKustomizationFile(dir string) bool {
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}

	return false
}

// isFluxManifest reports whether path, under root, is a YAML file Flux's
// generated kustomization lists. A YAML file that does not decode as
// Kubernetes resources fails, as it does in kustomize-controller.
func isFluxManifest(factory *resource.Factory, root, path string) (bool, error) {
	if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
		return false, nil
	}

	data, err := fsutil.ReadFileSafe(root, path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	_, err = factory.SliceFromBytes(data)
	if err != nil {
		return false, fmt.Errorf("decode Kubernetes YAML from %s: %w", path, err)
	}

	return true, nil
}

// fluxComponents returns the components Flux adds: each must be a local,
// relative path, and a missing one is dropped when the Kustomization ignores
// missing components.
func fluxComponents(dir string, composition FluxComposition) ([]string, error) {
	var components []string

	for _, component := range composition.Components {
		if filepath.IsAbs(component) || strings.Contains(component, "://") {
			return nil, fmt.Errorf("%w: %q", ErrNonLocalComponent, component)
		}

		_, err := os.Stat(filepath.Join(dir, component))
		if err != nil && composition.IgnoreMissingComponents {
			continue
		}

		components = append(components, component)
	}

	return components, nil
}

// mergeFluxComposition applies composition to kust the way kustomize-controller
// does: it sets the namespace and name affixes, appends the patches and
// components, and merges the image overrides.
func mergeFluxComposition(
	dir string,
	kust *types.Kustomization,
	composition FluxComposition,
) error {
	if composition.TargetNamespace != "" {
		kust.Namespace = composition.TargetNamespace
	}

	if composition.NamePrefix != "" {
		kust.NamePrefix = composition.NamePrefix
	}

	if composition.NameSuffix != "" {
		kust.NameSuffix = composition.NameSuffix
	}

	kust.Patches = append(kust.Patches, composition.Patches...)

	components, err := fluxComponents(dir, composition)
	if err != nil {
		return err
	}

	kust.Components = append(kust.Components, components...)

	for _, image := range composition.Images {
		kust.Images = mergeFluxImage(kust.Images, image)
	}

	if len(composition.BuildMetadata) > 0 {
		kust.BuildMetadata = composition.BuildMetadata
	}

	return nil
}

// mergeFluxImage merges one Flux image override into images: fields it sets
// replace those of an entry for the same image, a tag and a digest replace each
// other as one version, and an image with no entry is appended.
func mergeFluxImage(images []types.Image, image types.Image) []types.Image {
	index := slices.IndexFunc(images, func(existing types.Image) bool {
		return existing.Name == image.Name
	})
	if index < 0 {
		return append(images, image)
	}

	if image.NewName != "" {
		images[index].NewName = image.NewName
	}

	if image.NewTag != "" || image.Digest != "" {
		images[index].NewTag = image.NewTag
		images[index].Digest = image.Digest
	}

	return images
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
