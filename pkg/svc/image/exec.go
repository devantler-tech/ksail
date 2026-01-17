package image

import (
	"bytes"
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// ContainerExecutor provides methods for executing commands in containers.
type ContainerExecutor struct {
	dockerClient client.APIClient
}

// NewContainerExecutor creates a new container executor.
func NewContainerExecutor(dockerClient client.APIClient) *ContainerExecutor {
	return &ContainerExecutor{
		dockerClient: dockerClient,
	}
}

// ExecInContainer executes a command inside a container and returns stdout.
func (e *ContainerExecutor) ExecInContainer(
	ctx context.Context,
	containerName string,
	cmd []string,
) (string, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := e.dockerClient.ContainerExecCreate(ctx, containerName, execConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := e.dockerClient.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer

	_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)

	// Check exit code
	inspectResp, err := e.dockerClient.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return "", fmt.Errorf(
			"%w with exit code %d: %s",
			ErrExecFailed,
			inspectResp.ExitCode,
			stderr.String(),
		)
	}

	return stdout.String(), nil
}
