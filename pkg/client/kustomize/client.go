package kustomize

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Client provides kustomize build functionality.
type Client struct{}

// NewClient creates a new kustomize client.
func NewClient() *Client {
	return &Client{}
}

// Build runs kustomize build on the specified directory and returns the output.
func (c *Client) Build(ctx context.Context, path string) (*bytes.Buffer, error) {
	args := []string{"build", path, "--load-restrictor=LoadRestrictionsNone"}

	cmd := exec.CommandContext(ctx, "kustomize", args...) //nolint:gosec // kustomize is a trusted tool

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w\n%s", path, err, stderr.String())
	}

	return &stdout, nil
}
