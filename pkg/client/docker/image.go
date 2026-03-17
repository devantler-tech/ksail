package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// PullImage performs a single Docker image pull and consumes the output stream.
// This is the shared implementation used by both the registry manager and the Talos provisioner.
func PullImage(ctx context.Context, dockerClient client.APIClient, imageName string) error {
	reader, err := dockerClient.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image pull request: %w", err)
	}

	// Consume pull output to complete the download
	_, err = io.Copy(io.Discard, reader)
	closeErr := reader.Close()

	if err != nil {
		return fmt.Errorf("reading image pull output: %w", err)
	}

	if closeErr != nil {
		return fmt.Errorf("closing image pull reader: %w", closeErr)
	}

	return nil
}
