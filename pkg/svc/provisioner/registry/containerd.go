package registry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// randomDelimiterBytes is the number of random bytes used to generate heredoc delimiters.
// 8 bytes produces 16 hex characters, making collisions with user content extremely unlikely.
const randomDelimiterBytes = 8

// ErrExecFailed is returned when a docker exec command exits with a non-zero status.
var ErrExecFailed = errors.New("exec failed")

// InjectHostsTomlIntoNodes injects hosts.toml files into all cluster nodes for the given entries.
// This is the shared implementation used by Kind and VCluster provisioners.
func InjectHostsTomlIntoNodes(
	ctx context.Context,
	dockerClient client.APIClient,
	nodes []string,
	entries []MirrorEntry,
) error {
	for _, entry := range entries {
		hostsTomlContent := GenerateHostsToml(entry)

		for _, node := range nodes {
			err := InjectHostsToml(ctx, dockerClient, node, entry.Host, hostsTomlContent)
			if err != nil {
				return fmt.Errorf(
					"failed to inject hosts.toml for %s into node %s: %w",
					entry.Host,
					node,
					err,
				)
			}
		}
	}

	return nil
}

// InjectHostsToml creates the hosts directory and writes the hosts.toml file inside a cluster node
// using docker exec. It generates a random heredoc delimiter to prevent injection attacks.
func InjectHostsToml(
	ctx context.Context,
	dockerClient client.APIClient,
	nodeName string,
	registryHost string,
	hostsTomlContent string,
) error {
	certsDir := "/etc/containerd/certs.d/" + registryHost
	escapedCertsDir := EscapeShellArg(certsDir)

	delimiter, err := GenerateRandomDelimiter()
	if err != nil {
		return err
	}

	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("mkdir -p %s && cat > %s/hosts.toml << '%s'\n%s\n%s",
			escapedCertsDir, escapedCertsDir, delimiter, hostsTomlContent, delimiter),
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := dockerClient.ContainerExecCreate(ctx, nodeName, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := dockerClient.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer

	_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)

	inspectResp, err := dockerClient.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf(
			"%w with exit code %d: %s",
			ErrExecFailed,
			inspectResp.ExitCode,
			stderr.String(),
		)
	}

	return nil
}

// GenerateRandomDelimiter creates a random heredoc delimiter to prevent injection attacks.
// The delimiter is prefixed with "EOF_" and followed by 16 random hex characters.
func GenerateRandomDelimiter() (string, error) {
	randomBytes := make([]byte, randomDelimiterBytes)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random delimiter: %w", err)
	}

	return "EOF_" + hex.EncodeToString(randomBytes), nil
}

// EscapeShellArg escapes a string for safe use in POSIX shell commands.
// It wraps the string in single quotes and escapes any single quotes within.
func EscapeShellArg(arg string) string {
	escaped := strings.ReplaceAll(arg, "'", "'\\''")

	return "'" + escaped + "'"
}

// PrepareRegistryManagerFromSpecs creates a registry manager and builds registry infos
// from mirror specifications. Returns nil manager if mirrorSpecs is empty.
// The clusterName is used as prefix for container names to ensure uniqueness.
func PrepareRegistryManagerFromSpecs(
	ctx context.Context,
	mirrorSpecs []MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
) (Backend, []Info, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	upstreams := BuildUpstreamLookup(mirrorSpecs)

	registryMgr, infos, err := PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []Info {
			return BuildRegistryInfosFromSpecs(
				mirrorSpecs,
				upstreams,
				usedPorts,
				clusterName,
			)
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to prepare registry manager: %w", err)
	}

	return registryMgr, infos, nil
}
