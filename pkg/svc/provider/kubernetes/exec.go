package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecResult holds the output from an exec operation.
type ExecResult struct {
	Stdout string
	Stderr string
}

// ExecInDinD executes a command inside the DinD container.
// The command is passed to "sh -c" for shell interpretation.
func (p *Provider) ExecInDinD(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	command string,
) (*ExecResult, error) {
	return p.ExecInPod(ctx, restConfig, clusterName, DinDPodName, DinDContainerName, command, nil)
}

// ExecInDinDWithStdin executes a command inside the DinD container with stdin data.
func (p *Provider) ExecInDinDWithStdin(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	command string,
	stdin io.Reader,
) (*ExecResult, error) {
	return p.ExecInPod(ctx, restConfig, clusterName, DinDPodName, DinDContainerName, command, stdin)
}

// ExecInPod executes a command inside a specific container in a pod.
func (p *Provider) ExecInPod(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	podName string,
	containerName string,
	command string,
	stdin io.Reader,
) (*ExecResult, error) {
	ns := NamespaceName(clusterName)

	req := p.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(ns).
		SubResource("exec")

	execOpts := &corev1.PodExecOptions{
		Container: containerName,
		Command:   []string{"sh", "-c", command},
		Stdout:    true,
		Stderr:    true,
	}

	if stdin != nil {
		execOpts.Stdin = true
	}

	req.VersionedParams(execOpts, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer

	streamOpts := remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	if stdin != nil {
		streamOpts.Stdin = stdin
	}

	err = exec.StreamWithContext(ctx, streamOpts)
	if err != nil {
		return &ExecResult{
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}, fmt.Errorf("exec command %q: %w\nstderr: %s", command, err, stderr.String())
	}

	return &ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, nil
}

// InstallKindInDinD downloads and installs the Kind binary inside the DinD pod.
func (p *Provider) InstallKindInDinD(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	kindVersion string,
) error {
	// Detect architecture
	result, err := p.ExecInDinD(ctx, restConfig, clusterName,
		"uname -m | sed 's/aarch64/arm64/;s/x86_64/amd64/'")
	if err != nil {
		return fmt.Errorf("detect architecture: %w", err)
	}

	arch := strings.TrimSpace(result.Stdout)
	if arch == "" {
		arch = "amd64"
	}

	downloadURL := fmt.Sprintf(
		"https://github.com/kubernetes-sigs/kind/releases/download/%s/kind-linux-%s",
		kindVersion, arch,
	)

	cmd := fmt.Sprintf(
		"wget -q %s -O /usr/local/bin/kind && chmod +x /usr/local/bin/kind && kind version",
		downloadURL,
	)

	result, err = p.ExecInDinD(ctx, restConfig, clusterName, cmd)
	if err != nil {
		return fmt.Errorf("install kind binary: %w", err)
	}

	if !strings.Contains(result.Stdout, "kind v") {
		return fmt.Errorf("kind binary verification failed: %s", result.Stdout)
	}

	return nil
}

// WriteFileInDinD writes content to a file inside the DinD container.
func (p *Provider) WriteFileInDinD(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	path string,
	content string,
) error {
	cmd := fmt.Sprintf("cat > %s", path)
	stdin := strings.NewReader(content)

	_, err := p.ExecInDinDWithStdin(ctx, restConfig, clusterName, cmd, stdin)
	if err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}

	return nil
}

// ReadFileFromDinD reads a file from inside the DinD container.
func (p *Provider) ReadFileFromDinD(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	path string,
) (string, error) {
	cmd := fmt.Sprintf("cat %s", path)

	result, err := p.ExecInDinD(ctx, restConfig, clusterName, cmd)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", path, err)
	}

	return result.Stdout, nil
}

// RunKindCreateInDinD runs `kind create cluster` inside the DinD pod.
// It writes the config, runs the create command, and returns the kubeconfig.
func (p *Provider) RunKindCreateInDinD(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	kindClusterName string,
	configYAML string,
) (string, error) {
	// Write Kind config
	configPath := "/tmp/kind-config.yaml"

	err := p.WriteFileInDinD(ctx, restConfig, clusterName, configPath, configYAML)
	if err != nil {
		return "", fmt.Errorf("write kind config: %w", err)
	}

	// Run kind create cluster
	cmd := fmt.Sprintf(
		"kind create cluster --name %s --config %s --wait 5m 2>&1",
		kindClusterName, configPath,
	)

	result, err := p.ExecInDinD(ctx, restConfig, clusterName, cmd)
	if err != nil {
		return "", fmt.Errorf("kind create cluster: %w\nOutput: %s", err, result.Stdout)
	}

	// Read the kubeconfig generated by Kind
	kubeconfigContent, err := p.ReadFileFromDinD(
		ctx, restConfig, clusterName,
		"/root/.kube/config",
	)
	if err != nil {
		return "", fmt.Errorf("read kubeconfig from DinD: %w", err)
	}

	return kubeconfigContent, nil
}

// RunKindDeleteInDinD runs `kind delete cluster` inside the DinD pod.
func (p *Provider) RunKindDeleteInDinD(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	kindClusterName string,
) error {
	cmd := fmt.Sprintf("kind delete cluster --name %s 2>&1", kindClusterName)

	_, err := p.ExecInDinD(ctx, restConfig, clusterName, cmd)
	if err != nil {
		return fmt.Errorf("kind delete cluster: %w", err)
	}

	return nil
}
