// Package hostdebug implements host-level (infrastructure node) debugging for
// the `ksail workload debug --host` command, routed per distribution/provider:
//
//   - Vanilla/K3s/VCluster/KWOK on Docker: an interactive `docker exec` into the
//     node container.
//   - Talos (Docker/Hetzner/Omni): the Talos gRPC DebugClient.ContainerRun.
//
// It bypasses the Kubernetes API and targets the underlying node directly, so it
// lives in the service layer rather than the CLI command package.
package hostdebug

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	k8sutil "github.com/devantler-tech/ksail/v7/pkg/k8s"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrNodeNotFound is returned when the specified node name cannot be resolved.
var ErrNodeNotFound = errors.New("node not found")

// ErrUnsupportedHostDebug is returned for unsupported distribution/provider combinations.
var ErrUnsupportedHostDebug = errors.New(
	"host-level debugging is not supported for this distribution/provider combination",
)

// ErrNonZeroExitCode is returned when a container process exits with a non-zero exit code.
var ErrNonZeroExitCode = errors.New("container process exited with non-zero code")

// ErrNoIPAddress is returned when a container has no IP address.
var ErrNoIPAddress = errors.New("no IP address found for container")

// errStdinFDOverflow is returned when the stdin file descriptor overflows int.
var errStdinFDOverflow = errors.New("stdin file descriptor overflows int")

// Options carries the resolved inputs for a host-level debug session.
type Options struct {
	// Info is the detected cluster (distribution, provider, cluster name).
	Info *clusterdetector.Info
	// KubeconfigPath is the resolved kubeconfig path (used for cloud-provider
	// node IP resolution via the Kubernetes API).
	KubeconfigPath string
	// ContextName is the kubeconfig context, if any.
	ContextName string
	// NodeName is the target infrastructure node.
	NodeName string
	// Image is the debug container image (Talos path only).
	Image string
	// Args is the command to run inside the debug container/shell.
	Args []string
}

// Run detects the cluster distribution and provider from opts.Info and routes to
// the appropriate host-level debug mechanism.
func Run(ctx context.Context, opts Options) error {
	info := opts.Info

	switch info.Distribution {
	case v1alpha1.DistributionTalos:
		return runTalosHostDebugFromInfo(ctx, opts)
	case v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK:
		if info.Provider != v1alpha1.ProviderDocker {
			return fmt.Errorf(
				"%w: %s with %s provider",
				ErrUnsupportedHostDebug,
				info.Distribution,
				info.Provider,
			)
		}

		return runDockerHostDebug(ctx, info, opts.NodeName, opts.Args)
	case v1alpha1.DistributionEKS:
		// Host-level debug on EKS nodes is not supported via KSail's host-debug
		// path; users should use SSM/SSH directly against the EC2 instances.
		return fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Distribution)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Distribution)
	}
}

// findClusterNode lists the Docker nodes for clusterName under the given label
// scheme and returns the node matching nodeName, or an ErrNodeNotFound error
// that lists the available node names. The caller owns the docker client
// lifecycle.
func findClusterNode(
	ctx context.Context,
	dockerClient docker.Client,
	scheme dockerprovider.LabelScheme,
	clusterName string,
	nodeName string,
) (provider.NodeInfo, error) {
	prov := dockerprovider.NewProvider(dockerClient, scheme)

	nodes, err := prov.ListNodes(ctx, clusterName)
	if err != nil {
		return provider.NodeInfo{}, fmt.Errorf("list nodes: %w", err)
	}

	for _, node := range nodes {
		if node.Name == nodeName {
			return node, nil
		}
	}

	availableNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		availableNames = append(availableNames, node.Name)
	}

	return provider.NodeInfo{}, fmt.Errorf(
		"%w: %q (available nodes: %v)",
		ErrNodeNotFound,
		nodeName,
		availableNames,
	)
}

// runTalosHostDebugFromInfo resolves the Talos node endpoint and launches a debug container.
func runTalosHostDebugFromInfo(ctx context.Context, opts Options) error {
	talosconfigPath := os.Getenv("TALOSCONFIG")
	if talosconfigPath == "" {
		talosconfigPath = "~/.talos/config"
	}

	nodeEndpoint, err := resolveTalosNodeEndpoint(ctx, opts)
	if err != nil {
		return err
	}

	return runTalosHostDebug(ctx, nodeEndpoint, talosconfigPath, opts.Image, opts.Args)
}

// resolveTalosNodeEndpoint resolves a node name to a Talos API endpoint.
// For Docker provider, it inspects the container's network settings.
// For Hetzner/Omni, it resolves the node IP from the Kubernetes Node object.
func resolveTalosNodeEndpoint(ctx context.Context, opts Options) (string, error) {
	info := opts.Info

	switch info.Provider {
	case v1alpha1.ProviderDocker:
		return resolveDockerNodeIP(
			ctx,
			info.ClusterName,
			opts.NodeName,
			dockerprovider.LabelSchemeTalos,
		)
	case v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni:
		return resolveNodeIPFromKubernetes(
			ctx,
			opts.KubeconfigPath,
			opts.ContextName,
			opts.NodeName,
			info.Provider,
		)
	case v1alpha1.ProviderAWS, v1alpha1.ProviderGCP:
		// EKS/GKE host-level debug is not supported: nodes are cloud VMs
		// reachable only via the cloud's own access paths, not via Talos or
		// Docker host debug.
		return "", fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Provider)
	case v1alpha1.ProviderKubernetes:
		// Kubernetes provider host-level debug is not yet supported.
		return "", fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Provider)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Provider)
	}
}

// runDockerHostDebug runs an interactive shell inside a Docker container node.
func runDockerHostDebug(
	ctx context.Context,
	info *clusterdetector.Info,
	nodeName string,
	execArgs []string,
) error {
	scheme := DistributionToLabelScheme(info.Distribution)

	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	node, err := findClusterNode(ctx, dockerClient, scheme, info.ClusterName, nodeName)
	if err != nil {
		return err
	}

	shellCmd := execArgs
	if len(shellCmd) == 0 {
		shellCmd = []string{"/bin/sh"}
	}

	return runInteractiveDockerExec(ctx, dockerClient, node.Name, shellCmd)
}

// runInteractiveDockerExec runs an interactive exec session in a Docker container
// with stdin, stdout, and TTY attached.
func runInteractiveDockerExec(
	ctx context.Context,
	dockerClient docker.Client,
	containerName string,
	cmdArgs []string,
) error {
	stdinFd, err := stdinFD()
	if err != nil {
		return err
	}

	isTTY := term.IsTerminal(stdinFd)

	execID, err := dockerClient.ContainerExecCreate(ctx, containerName, dockercontainer.ExecOptions{
		Cmd:          cmdArgs,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          isTTY,
	})
	if err != nil {
		return fmt.Errorf("create exec in container %q: %w", containerName, err)
	}

	resp, err := dockerClient.ContainerExecAttach(ctx, execID.ID, dockercontainer.ExecAttachOptions{
		Tty: isTTY,
	})
	if err != nil {
		return fmt.Errorf("attach to exec in container %q: %w", containerName, err)
	}
	defer resp.Close()

	if isTTY {
		restoreFunc, termErr := setupRawTerminal()
		if termErr != nil {
			return termErr
		}

		defer restoreFunc()
	}

	pipeDockerExecStreams(&resp, isTTY)

	return checkExecExitCode(ctx, dockerClient, execID.ID)
}

func stdinFD() (int, error) {
	stdinFDValue := os.Stdin.Fd()
	if stdinFDValue > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("%w: %d", errStdinFDOverflow, stdinFDValue)
	}

	// #nosec G115 -- value is bounds-checked against math.MaxInt above.
	return int(stdinFDValue), nil
}

// setupRawTerminal sets the terminal to raw mode and returns a restore function.
func setupRawTerminal() (func(), error) {
	stdinFd, err := stdinFD()
	if err != nil {
		return nil, err
	}

	if !term.IsTerminal(stdinFd) {
		return func() {}, nil
	}

	oldState, termErr := term.MakeRaw(stdinFd)
	if termErr != nil {
		return nil, fmt.Errorf("set terminal to raw mode: %w", termErr)
	}

	return func() {
		if oldState != nil {
			_ = term.Restore(stdinFd, oldState)
		}
	}, nil
}

// pipeDockerExecStreams pipes stdin/stdout between the terminal and Docker exec.
// When isTTY is true, raw copy is used. When false, stdcopy demuxes stdout/stderr.
func pipeDockerExecStreams(resp *dockertypes.HijackedResponse, isTTY bool) {
	doneCh := make(chan error, 1)
	stdinReader, stdinWriter := io.Pipe()

	go func() {
		_, copyErr := io.Copy(stdinWriter, os.Stdin)
		_ = stdinWriter.CloseWithError(copyErr)
	}()

	go func() {
		_, copyErr := io.Copy(resp.Conn, stdinReader)
		if errors.Is(copyErr, io.ErrClosedPipe) || errors.Is(copyErr, context.Canceled) {
			doneCh <- nil

			return
		}

		doneCh <- copyErr
	}()

	if isTTY {
		_, _ = io.Copy(os.Stdout, resp.Reader)
	} else {
		_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, resp.Reader)
	}

	// Cancel stdin forwarding now that the attach stream has ended.
	_ = stdinReader.CloseWithError(context.Canceled)

	// Wait for stdin forwarding to finish.
	<-doneCh
}

// checkExecExitCode inspects the exec session and returns an error for non-zero exit codes.
func checkExecExitCode(
	ctx context.Context,
	dockerClient docker.Client,
	execID string,
) error {
	inspectResp, err := dockerClient.ContainerExecInspect(ctx, execID)
	if err != nil {
		return fmt.Errorf("inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf("%w: %d", ErrNonZeroExitCode, inspectResp.ExitCode)
	}

	return nil
}

// resolveDockerNodeIP resolves a node name to its Docker container IP address.
func resolveDockerNodeIP(
	ctx context.Context,
	clusterName string,
	nodeName string,
	scheme dockerprovider.LabelScheme,
) (string, error) {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return "", fmt.Errorf("create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	node, err := findClusterNode(ctx, dockerClient, scheme, clusterName, nodeName)
	if err != nil {
		return "", err
	}

	// Inspect the container to get its IP address.
	containerJSON, inspectErr := dockerClient.ContainerInspect(ctx, node.Name)
	if inspectErr != nil {
		return "", fmt.Errorf("inspect container %q: %w", node.Name, inspectErr)
	}

	for _, network := range containerJSON.NetworkSettings.Networks {
		if network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}

	return "", fmt.Errorf("%w: %q", ErrNoIPAddress, node.Name)
}

// DistributionToLabelScheme maps a distribution to its Docker provider label scheme.
func DistributionToLabelScheme(distribution v1alpha1.Distribution) dockerprovider.LabelScheme {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return dockerprovider.LabelSchemeKind
	case v1alpha1.DistributionK3s:
		return dockerprovider.LabelSchemeK3d
	case v1alpha1.DistributionTalos:
		return dockerprovider.LabelSchemeTalos
	case v1alpha1.DistributionVCluster:
		return dockerprovider.LabelSchemeVCluster
	case v1alpha1.DistributionKWOK:
		return dockerprovider.LabelSchemeKWOK
	case v1alpha1.DistributionEKS:
		// EKS nodes are EC2 instances without Docker labels; fall back to the
		// default scheme — this path is not used for EKS in practice.
		return dockerprovider.LabelSchemeKind
	default:
		return dockerprovider.LabelSchemeKind
	}
}

// resolveNodeIPFromKubernetes resolves a Kubernetes node name to its IP address
// by querying the Node object. For cloud providers (Hetzner), prefers ExternalIP
// since InternalIP may not be reachable. Otherwise prefers InternalIP.
func resolveNodeIPFromKubernetes(
	ctx context.Context,
	kubeconfigPath string,
	contextName string,
	nodeName string,
	nodeProvider v1alpha1.Provider,
) (string, error) {
	clientset, err := k8sutil.NewClientset(kubeconfigPath, contextName)
	if err != nil {
		return "", fmt.Errorf("create Kubernetes client: %w", err)
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get node %q: %w", nodeName, err)
	}

	// Cloud providers expose nodes via public IPs; prefer ExternalIP.
	preferExternal := nodeProvider == v1alpha1.ProviderHetzner ||
		nodeProvider == v1alpha1.ProviderOmni

	primary := corev1.NodeExternalIP
	fallback := corev1.NodeInternalIP

	if !preferExternal {
		primary = corev1.NodeInternalIP
		fallback = corev1.NodeExternalIP
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == primary {
			return addr.Address, nil
		}
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == fallback {
			return addr.Address, nil
		}
	}

	return "", fmt.Errorf("%w: %q", ErrNoIPAddress, nodeName)
}
