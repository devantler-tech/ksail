package cluster

import (
	"errors"
	"fmt"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
)

// errStopIteration is a sentinel error used to stop container iteration early.
var errStopIteration = errors.New("stop iteration")

// withDockerClient executes an operation with the Docker client, handling locking and invoker retrieval.
// This is the canonical way to access Docker in this package, ensuring thread-safe access to the invoker.
func withDockerClient(cmd *cobra.Command, operation func(dockerclient.Client) error) error {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	return invoker(cmd, operation)
}

// forEachContainerName lists all Docker containers and calls the provided function for each container name.
// The function receives the normalized container name (without leading slash).
// Container processing stops early if the callback returns true (indicating done).
func forEachContainerName(
	cmd *cobra.Command,
	callback func(containerName string) (done bool),
) error {
	return forEachContainer(
		cmd,
		func(_ dockerclient.Client, _ container.Summary, name string) error {
			if callback(name) {
				return errStopIteration
			}

			return nil
		},
	)
}

// forEachContainer lists all Docker containers and calls the callback for each container name.
// The callback receives the docker client, container info, and normalized container name.
// Return an error to stop iteration (use errStopIteration for normal early exit).
func forEachContainer(
	cmd *cobra.Command,
	callback func(dockerClient dockerclient.Client, ctr container.Summary, name string) error,
) error {
	return withDockerClient(cmd, func(dockerClient dockerclient.Client) error {
		containers, err := dockerClient.ContainerList(cmd.Context(), container.ListOptions{
			All: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		for _, ctr := range containers {
			for _, name := range ctr.Names {
				containerName := strings.TrimPrefix(name, "/")

				err := callback(dockerClient, ctr, containerName)
				if err != nil {
					if errors.Is(err, errStopIteration) {
						return nil // Normal early exit
					}

					return err
				}
			}
		}

		return nil
	})
}
