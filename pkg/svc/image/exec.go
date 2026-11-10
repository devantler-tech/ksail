package image

import (
	"bytes"
	"context"
	"fmt"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ContainerExecutor provides methods for executing commands in containers.
type ContainerExecutor struct {
	dockerClient dockerclient.Client
}

// NewContainerExecutor creates a new container executor.
func NewContainerExecutor(dockerClient dockerclient.Client) *ContainerExecutor {
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

	// Closing the hijacked connection when ctx ends unblocks a stalled stream
	// read, so a deadline on ctx bounds the whole exec rather than only its setup.
	stopClose := context.AfterFunc(ctx, resp.Close)
	defer stopClose()

	var stdout, stderr bytes.Buffer

	_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)

	// A read cut short by cancellation holds partial output; never report it
	// as a completed command.
	err = ctx.Err()
	if err != nil {
		return "", fmt.Errorf("exec interrupted: %w", err)
	}

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

// removeNodeArchive deletes an export or import archive from a node. It always
// runs on a context detached from the caller and bounded by nodeCleanupTimeout,
// so a cancellation that lands while removal is starting cannot abort it and
// leave the archive behind.
func (e *ContainerExecutor) removeNodeArchive(ctx context.Context, nodeName, tmpPath string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nodeCleanupTimeout)
	defer cancel()

	_, _ = e.ExecInContainer(cleanupCtx, nodeName, []string{"rm", "-f", tmpPath})
}
