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
	"slices"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/google/go-containerregistry/pkg/authn"
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

// Push retry constants.
const (
	pushMaxAttempts   = 3
	pushRetryBaseWait = 2 * time.Second
	pushRetryMaxWait  = 10 * time.Second
)

// pushFn is the function signature for pushing an OCI image to a registry.
type pushFn func(ref name.Reference, img v1.Image, options ...remote.Option) error

// Builder implementation.

// Builder packages Kubernetes manifests into OCI artifacts and pushes them to a registry.
//
// It validates build options, collects manifest files from the source directory,
// packages them into an OCI-compliant image layer, and pushes the resulting artifact
// to the specified registry endpoint.
type Builder struct{}

// NewWorkloadArtifactBuilder returns a workload artifact builder backed by go-containerregistry.
//
// The returned builder uses the go-containerregistry library to package manifests
// into OCI artifacts and push them to container registries.
func NewWorkloadArtifactBuilder() *Builder {
	return &Builder{}
}

// parseOCIReference creates an OCI reference from endpoint, repository, and version.
func parseOCIReference(endpoint, repository, version string) (name.Reference, error) {
	ref, err := name.ParseReference(
		fmt.Sprintf("%s/%s:%s", endpoint, repository, version),
		name.WeakValidation,
		name.Insecure,
	)
	if err != nil {
		return nil, fmt.Errorf("parse reference: %w", err)
	}

	return ref, nil
}

// pushImage pushes an OCI image to the registry with optional authentication.
// Retries transient errors (redirect loops, network timeouts, 5xx) with exponential backoff.
func pushImage(
	ctx context.Context,
	ref name.Reference,
	img v1.Image,
	username, password string,
) error {
	remoteOpts := []remote.Option{remote.WithContext(ctx)}

	// Require BOTH username and password to be non-empty before setting Basic auth.
	// If only one is set (e.g. GITHUB_ACTOR present but GITHUB_TOKEN missing),
	// GHCR issues a write-less anonymous token instead of returning 401, producing
	// a misleading 403 "permission_denied" rather than an actionable auth error.
	if username != "" && password != "" {
		auth := &authn.Basic{
			Username: username,
			Password: password,
		}
		remoteOpts = append(remoteOpts, remote.WithAuth(auth))
	}

	return pushWithRetry(ctx, ref, img, remoteOpts, remote.Write)
}

// pushWithRetry retries the push operation on transient errors with exponential backoff.
func pushWithRetry(
	ctx context.Context,
	ref name.Reference,
	img v1.Image,
	remoteOpts []remote.Option,
	push pushFn,
) error {
	err := netretry.Do(
		ctx,
		pushMaxAttempts,
		pushRetryBaseWait,
		pushRetryMaxWait,
		func() error {
			return push(ref, img, remoteOpts...)
		},
		netretry.WithCancelError(func(ctxErr error) error {
			return fmt.Errorf("push cancelled: %w", ctxErr)
		}),
	)
	if err == nil {
		return nil
	}

	if netretry.IsCancelled(err) {
		return err //nolint:wrapcheck // return the netretry cancellation error unwrapped (already context-tagged)
	}

	if !netretry.IsRetryable(err) {
		return fmt.Errorf("push failed (non-retryable): %w", err)
	}

	return fmt.Errorf("push failed after %d attempts: %w", pushMaxAttempts, err)
}

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
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (BuildResult, error) {
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

	layer, err := newManifestLayer(validated.SourcePath, manifestFiles)
	if err != nil {
		return BuildResult{}, fmt.Errorf("package manifests: %w", err)
	}

	img, err := buildImage(layer, validated)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build image: %w", err)
	}

	return pushImageToRegistry(ctx, img, artifactInfo{
		Name:             validated.Name,
		Version:          validated.Version,
		RegistryEndpoint: validated.RegistryEndpoint,
		Repository:       validated.Repository,
		SourcePath:       validated.SourcePath,
	}, validated.Username, validated.Password)
}

// BuildEmpty pushes an OCI artifact with an empty kustomization.yaml to the registry.
// This creates a minimal valid Kustomize structure that Flux can reconcile,
// useful when no source directory exists but a valid artifact reference is required.
func (b *Builder) BuildEmpty(ctx context.Context, opts EmptyBuildOptions) (BuildResult, error) {
	validated, err := opts.Validate()
	if err != nil {
		return BuildResult{}, err
	}

	// Create a layer with an empty kustomization.yaml
	layer, err := newEmptyKustomizationLayer()
	if err != nil {
		return BuildResult{}, fmt.Errorf("create empty kustomization layer: %w", err)
	}

	// Build image with the layer
	img, err := buildEmptyImageWithLayer(layer, validated)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build empty image: %w", err)
	}

	return pushImageToRegistry(ctx, img, artifactInfo{
		Name:             validated.Name,
		Version:          validated.Version,
		RegistryEndpoint: validated.RegistryEndpoint,
		Repository:       validated.Repository,
		SourcePath:       "", // No source path for empty artifacts
	}, validated.Username, validated.Password)
}

// pushImageToRegistry handles the common push logic and creates the artifact result.
type artifactInfo struct {
	Name             string
	Version          string
	RegistryEndpoint string
	Repository       string
	SourcePath       string
}

func pushImageToRegistry(
	ctx context.Context,
	img v1.Image,
	info artifactInfo,
	username, password string,
) (BuildResult, error) {
	ref, err := parseOCIReference(info.RegistryEndpoint, info.Repository, info.Version)
	if err != nil {
		return BuildResult{}, err
	}

	err = pushImage(ctx, ref, img, username, password)
	if err != nil {
		return BuildResult{}, fmt.Errorf("push artifact: %w", err)
	}

	artifact := v1alpha1.OCIArtifact{
		Name:             info.Name,
		Version:          info.Version,
		RegistryEndpoint: info.RegistryEndpoint,
		Repository:       info.Repository,
		Tag:              info.Version,
		SourcePath:       info.SourcePath,
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

	slices.Sort(manifests)

	return manifests, nil
}

// OCI layer construction helpers.

// newManifestLayer creates an OCI layer containing all manifest files as a tarball.
//
// Files are added to the tar archive with their relative paths from the root directory.
// File permissions are set to 0o644 for consistency.
//
// Every file is written twice: once at the archive root, and once under a
// directory named after the source directory. Both GitOps engines resolve their
// configured path against the archive ROOT — Flux via the FluxInstance sync path
// (default "./") and Argo CD via the Application source path (argocd.DefaultSourcePath,
// "."). The prefixed copies are retained only so an artifact stays readable by a
// consumer that was pointed at "<sourceDirectory>/...".
//
// Suppressing the root copies for any engine breaks that engine silently: Argo CD
// renders zero resources and still reports Synced/Healthy (issue #6284).
//
// Returns an OCI v1.Layer suitable for inclusion in an OCI image.
func newManifestLayer(
	root string,
	files []string,
) (v1.Layer, error) {
	compressed := bytes.NewBuffer(nil)
	gzipWriter := gzip.NewWriter(compressed)
	// Keep gzip output deterministic-ish (helps tests/snapshots and caching).
	gzipWriter.ModTime = time.Time{}

	tarWriter := tar.NewWriter(gzipWriter)

	// Determine the compatibility prefix for the archive structure.
	prefix := filepath.Base(root)
	if prefix == "." || prefix == string(os.PathSeparator) {
		prefix = ""
	}

	var err error
	for _, path := range files {
		err = addFileToArchive(tarWriter, root, path, prefix)
		if err != nil {
			return nil, err
		}
	}

	// Argo CD requires a single gzip tar layer and will ignore/skip unrecognized
	// layer media types, producing errors like "got 0" layers.
	return finalizeLayer(tarWriter, gzipWriter, compressed)
}

// addFileToArchive adds a single file to the tar archive with its relative path from root.
//
// The file is added with:
//   - Relative path from root (converted to forward slashes)
//   - Fixed permissions of 0o644
//   - Original file content
//
// The file is written at the archive root, and again under prefix when one is set.
func addFileToArchive(
	tarWriter *tar.Writer,
	root, path, prefix string,
) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("get relative path for %s: %w", path, err)
	}

	content, err := os.ReadFile( //nolint:gosec // G304: path from archive walk
		path,
	)
	if err != nil {
		return fmt.Errorf("read file %s: %w", path, err)
	}

	// Write files at the archive root — the path every consumer resolves against.
	err = writeTarEntry(tarWriter, info, path, rel, content)
	if err != nil {
		return err
	}

	// Write files under prefix as well (when prefix is set).
	if prefix != "" {
		err = writeTarEntry(tarWriter, info, path, filepath.Join(prefix, rel), content)
		if err != nil {
			return err
		}
	}

	return nil
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

// newBaseConfigFile creates a base ConfigFile with architecture, OS, and timestamp.
func newBaseConfigFile() *v1.ConfigFile {
	return &v1.ConfigFile{
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
		Created:      v1.Time{Time: time.Now().UTC()},
		Config:       v1.Config{Labels: make(map[string]string)},
	}
}

// buildImageWithConfig creates an OCI image from a layer and config file.
func buildImageWithConfig(layer v1.Layer, cfg *v1.ConfigFile) (v1.Image, error) {
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

func buildImage(
	layer v1.Layer,
	opts ValidatedBuildOptions,
) (v1.Image, error) {
	cfg := newBaseConfigFile()
	cfg.Config.Labels = map[string]string{
		"org.opencontainers.image.title":        opts.Name,
		"org.opencontainers.image.version":      opts.Version,
		"org.opencontainers.image.source":       opts.SourcePath,
		"devantler.tech/ksail/repository":       opts.Repository,
		"devantler.tech/ksail/registryEndpoint": opts.RegistryEndpoint,
	}

	return buildImageWithConfig(layer, cfg)
}

// newEmptyKustomizationLayer creates an OCI layer containing an empty kustomization.yaml file.
// Currently creates a Flux-compatible Kustomization with empty resources array.
func newEmptyKustomizationLayer() (v1.Layer, error) {
	compressed := bytes.NewBuffer(nil)
	gzipWriter := gzip.NewWriter(compressed)
	gzipWriter.ModTime = time.Time{} // Deterministic output

	tarWriter := tar.NewWriter(gzipWriter)

	// Create empty kustomization.yaml content
	kustomizationContent := []byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`)

	// Add kustomization.yaml at root
	err := addContentToArchive(tarWriter, "kustomization.yaml", kustomizationContent)
	if err != nil {
		return nil, fmt.Errorf("add kustomization.yaml: %w", err)
	}

	return finalizeLayer(tarWriter, gzipWriter, compressed)
}

// finalizeLayer closes the tar and gzip writers (in that order) and wraps the compressed bytes as an
// OCI layer — the shared closing sequence for newManifestLayer and newEmptyKustomizationLayer.
func finalizeLayer(
	tarWriter *tar.Writer,
	gzipWriter *gzip.Writer,
	compressed *bytes.Buffer,
) (v1.Layer, error) {
	err := tarWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}

	return static.NewLayer(compressed.Bytes(), types.OCILayer), nil
}

// tarFilePermissions is the standard file mode for files in tar archives.
const tarFilePermissions = 0o644

// addContentToArchive adds content with a given filename to the tar archive.
func addContentToArchive(tarWriter *tar.Writer, filename string, content []byte) error {
	header := &tar.Header{
		Name:    filename,
		Mode:    tarFilePermissions,
		Size:    int64(len(content)),
		ModTime: time.Time{}, // Deterministic output
	}

	err := tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("write header for %s: %w", filename, err)
	}

	_, err = tarWriter.Write(content)
	if err != nil {
		return fmt.Errorf("write content for %s: %w", filename, err)
	}

	return nil
}

// buildEmptyImageWithLayer creates an OCI image with the given layer and labels from opts.
func buildEmptyImageWithLayer(layer v1.Layer, opts ValidatedEmptyBuildOptions) (v1.Image, error) {
	cfg := newBaseConfigFile()
	cfg.Config.Labels = map[string]string{
		"org.opencontainers.image.title":        opts.Name,
		"org.opencontainers.image.version":      opts.Version,
		"devantler.tech/ksail/repository":       opts.Repository,
		"devantler.tech/ksail/registryEndpoint": opts.RegistryEndpoint,
		"devantler.tech/ksail/empty":            "true",
	}

	return buildImageWithConfig(layer, cfg)
}
