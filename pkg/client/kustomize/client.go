package kustomize

import (
	"bytes"
	"context"
	"fmt"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// Client provides kustomize build functionality.
type Client struct{}

// NewClient creates a new kustomize client.
func NewClient() *Client {
	return &Client{}
}

// Build runs kustomize build on the specified directory and returns the output.
func (c *Client) Build(_ context.Context, path string) (*bytes.Buffer, error) {
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
