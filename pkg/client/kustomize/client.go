package kustomize

import (
	"bytes"
	"context"
	"fmt"
	"sync"

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

// composeDir is the in-memory directory Compose builds in.
const composeDir = "/compose"

// Compose applies a namespace and patches on top of already-built manifests,
// the way Flux's kustomize-controller composes the folder a Flux Kustomization
// applies: the patches run first, then every namespaced resource is moved into
// namespace. An empty namespace leaves namespaces unchanged. The build runs on
// an in-memory filesystem, so nothing on disk is read or written.
func (c *Client) Compose(
	_ context.Context,
	manifests []byte,
	namespace string,
	patches []types.Patch,
) (*bytes.Buffer, error) {
	warmSchema()

	fSys := filesys.MakeFsInMemory()

	err := fSys.WriteFile(composeDir+"/resources.yaml", manifests)
	if err != nil {
		return nil, fmt.Errorf("write composed resources: %w", err)
	}

	kustomization := types.Kustomization{
		Resources: []string{"resources.yaml"},
		Namespace: namespace,
		Patches:   patches,

		APIVersion: types.KustomizationVersion,
		Kind:       types.KustomizationKind,
	}

	data, err := kyaml.Marshal(kustomization)
	if err != nil {
		return nil, fmt.Errorf("marshal composed kustomization: %w", err)
	}

	err = fSys.WriteFile(composeDir+"/kustomization.yaml", data)
	if err != nil {
		return nil, fmt.Errorf("write composed kustomization: %w", err)
	}

	resMap, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(fSys, composeDir)
	if err != nil {
		return nil, fmt.Errorf("compose manifests: %w", err)
	}

	yaml, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("convert composed resources to yaml: %w", err)
	}

	return bytes.NewBuffer(yaml), nil
}
