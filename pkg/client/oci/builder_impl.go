package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Manifest file extensions.
//
//nolint:gochecknoglobals // static set of valid manifest extensions
var manifestExtensions = map[string]struct{}{
	".yaml": {},
	".yml":  {},
	".json": {},
}

// Registry push operations.

// Builder implementation.

// NewWorkloadArtifactBuilder returns a concrete implementation backed by go-containerregistry.
//
// The returned builder uses the go-containerregistry library to package manifests
// into OCI artifacts and push them to container registries.
func NewWorkloadArtifactBuilder() WorkloadArtifactBuilder {
	return &builder{}
}

type builder struct{}

// Build collects manifests from the source path, packages them into an OCI artifact, and pushes it to the registry.
//
// The build process follows these steps:
//  1. Validates build options and normalizes inputs
//  2. Discovers and collects manifest files from the source directory
//  3. Packages manifests into a tarball layer
//  4. Builds an OCI image with the layer and metadata labels
//  5. Constructs a registry reference from endpoint, repository, and version
//  6. Pushes the image to the registry
//  7. Returns artifact metadata on success
//
// Returns BuildResult with complete artifact metadata, or an error if any step fails.
func (b *builder) Build(ctx context.Context, opts BuildOptions) (BuildResult, error) {
	validated, err := opts.Validate()
	if err != nil {
		return BuildResult{}, err
	}

	manifestFiles, err := collectManifestFiles(validated.SourcePath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("discover manifests: %w", err)
	}

	if len(manifestFiles) == 0 {
		return BuildResult{}, ErrNoManifestFiles
	}

	layer, err := newManifestLayer(validated.SourcePath, manifestFiles, validated.GitOpsEngine)
	if err != nil {
		return BuildResult{}, fmt.Errorf("package manifests: %w", err)
	}

	img, err := buildImage(layer, validated)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build image: %w", err)
	}

	ref, err := name.ParseReference(
		fmt.Sprintf(
			"%s/%s:%s",
			validated.RegistryEndpoint,
			validated.Repository,
			validated.Version,
		),
		name.WeakValidation,
		name.Insecure,
	)
	if err != nil {
		return BuildResult{}, fmt.Errorf("parse reference: %w", err)
	}

	err = remote.Write(ref, img, remote.WithContext(ctx))
	if err != nil {
		return BuildResult{}, fmt.Errorf("push artifact: %w", err)
	}

	artifact := v1alpha1.OCIArtifact{
		Name:             validated.Name,
		Version:          validated.Version,
		RegistryEndpoint: validated.RegistryEndpoint,
		Repository:       validated.Repository,
		Tag:              validated.Version,
		SourcePath:       validated.SourcePath,
		CreatedAt:        metav1.NewTime(time.Now().UTC()),
	}

	return BuildResult{Artifact: artifact}, nil
}

// Manifest collection helpers.

// collectManifestFiles walks the source directory and returns paths to all valid manifest files.
//
// A file is considered a valid manifest if:
//   - It has a .yaml, .yml, or .json extension
//   - It is not empty (size > 0)
//
// Returns a sorted list of absolute file paths, or an error if directory traversal fails.
func collectManifestFiles(root string) ([]string, error) {
	var manifests []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if _, ok := manifestExtensions[ext]; !ok {
			return nil
		}

		info, statErr := entry.Info()
		if statErr != nil {
			return fmt.Errorf("get file info for %s: %w", path, statErr)
		}

		if info.Size() == 0 {
			//nolint:err113 // includes dynamic file path for debugging
			return fmt.Errorf("manifest file %s is empty", path)
		}

		manifests = append(manifests, path)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", root, err)
	}

	sort.Strings(manifests)

	return manifests, nil
}

// OCI layer construction helpers.

// newManifestLayer creates an OCI layer containing all manifest files as a tarball.
//
// Files are added to the tar archive with their relative paths from the root directory.
// File permissions are set to 0o644 for consistency.
//
// The gitOpsEngine parameter controls the archive structure:
//   - GitOpsEngineFlux: files at root + under prefix directory (Flux uses root, prefix for compatibility)
//   - GitOpsEngineArgoCD: files under prefix directory only (optimization)
//   - Empty/GitOpsEngineNone: files at both root and under prefix for compatibility
//
// Returns an OCI v1.Layer suitable for inclusion in an OCI image.
//

func newManifestLayer(
	root string,
	files []string,
	gitOpsEngine v1alpha1.GitOpsEngine,
) (v1.Layer, error) {
	compressed := bytes.NewBuffer(nil)
	gzipWriter := gzip.NewWriter(compressed)
	// Keep gzip output deterministic-ish (helps tests/snapshots and caching).
	gzipWriter.ModTime = time.Time{}

	tarWriter := tar.NewWriter(gzipWriter)

	// Determine the prefix for the archive structure.
	// All engines use a prefix directory to maintain compatibility with both Flux and ArgoCD.
	// ArgoCD only uses the prefixed files, while Flux uses both root and prefixed files.
	prefix := filepath.Base(root)
	if prefix == "." || prefix == string(os.PathSeparator) {
		prefix = ""
	}

	var err error
	for _, path := range files {
		err = addFileToArchive(tarWriter, root, path, prefix, gitOpsEngine)
		if err != nil {
			return nil, err
		}
	}

	err = tarWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}

	// Argo CD requires a single gzip tar layer and will ignore/skip unrecognized
	// layer media types, producing errors like "got 0" layers.
	return static.NewLayer(compressed.Bytes(), types.OCILayer), nil
}

// addFileToArchive adds a single file to the tar archive with its relative path from root.
//
// The file is added with:
//   - Relative path from root (converted to forward slashes)
//   - Fixed permissions of 0o644
//   - Original file content
//
// The gitOpsEngine parameter controls how files are written:
//   - GitOpsEngineFlux: files at root + under prefix (Flux uses root, prefix for compatibility)
//   - GitOpsEngineArgoCD: files under prefix directory only (optimization)
//   - Empty/GitOpsEngineNone: files at both locations for compatibility

func addFileToArchive(
	tarWriter *tar.Writer,
	root, path, prefix string,
	gitOpsEngine v1alpha1.GitOpsEngine,
) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("get relative path for %s: %w", path, err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}

	// Write files at root for all engines except ArgoCD.
	if shouldWriteAtRoot(gitOpsEngine) {
		err = writeTarEntry(tarWriter, info, path, rel, content)
		if err != nil {
			return err
		}
	}

	// Write files under prefix for all engines (when prefix is set).
	if prefix != "" {
		err = writeTarEntry(tarWriter, info, path, filepath.Join(prefix, rel), content)
		if err != nil {
			return err
		}
	}

	return nil
}

// shouldWriteAtRoot determines if files should be written at the root of the archive.
// ArgoCD does not need files at root (optimization), all other engines do.
func shouldWriteAtRoot(gitOpsEngine v1alpha1.GitOpsEngine) bool {
	return gitOpsEngine != v1alpha1.GitOpsEngineArgoCD
}

// writeTarEntry writes a single tar entry with the given name and content.
func writeTarEntry(
	tarWriter *tar.Writer,
	info os.FileInfo,
	path, entryName string,
	content []byte,
) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("create tar header for %s: %w", path, err)
	}

	header.Name = filepath.ToSlash(entryName)
	header.Mode = 0o644
	header.Size = int64(len(content))

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("write tar header for %s: %w", path, err)
	}

	_, err = tarWriter.Write(content)
	if err != nil {
		return fmt.Errorf("write file %s to tar: %w", path, err)
	}

	return nil
}

// OCI image construction helpers.

// buildImage creates an OCI image from a manifest layer with appropriate metadata labels.
//
// The image is constructed with:
//   - Current OS and architecture
//   - Creation timestamp
//   - OCI standard labels (title, version, source)
//   - KSail-specific labels (repository, registry endpoint)
//
// Returns a complete OCI v1.Image ready for push to a registry.
//

func buildImage(
	layer v1.Layer,
	opts ValidatedBuildOptions,
) (v1.Image, error) {
	cfg := &v1.ConfigFile{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
		Created:      v1.Time{Time: time.Now().UTC()},
		Config: v1.Config{
			Labels: map[string]string{
				"org.opencontainers.image.title":        opts.Name,
				"org.opencontainers.image.version":      opts.Version,
				"org.opencontainers.image.source":       opts.SourcePath,
				"devantler.tech/ksail/repository":       opts.Repository,
				"devantler.tech/ksail/registryEndpoint": opts.RegistryEndpoint,
			},
		},
	}

	img, err := mutate.ConfigFile(empty.Image, cfg)
	if err != nil {
		return nil, fmt.Errorf("set config file: %w", err)
	}

	finalImg, err := mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, fmt.Errorf("append layer: %w", err)
	}

	return finalImg, nil
}
