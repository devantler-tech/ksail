package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	k8sutil "github.com/devantler-tech/ksail/v7/pkg/k8s"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultDebugImage = "docker.io/library/alpine:latest"

// NewDebugCmd creates the workload debug command.
//
// Without --host it wraps kubectl debug (ephemeral containers, node debugging).
// With --host <node-name> it performs host-level debugging routed per distribution:
//   - Vanilla/K3s/VCluster (Docker): interactive docker exec into the node container
//   - Talos (all providers): Talos SDK DebugClient.ContainerRun()
func NewDebugCmd() *cobra.Command {
	var hostNode string

	kubectlDebugCmd := newKubectlCommand(
		func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
			return client.CreateDebugCommand(kubeconfigPath)
		},
	)

	// Preserve the original RunE from kubectl debug.
	originalRunE := kubectlDebugCmd.RunE
	originalRun := kubectlDebugCmd.Run

	kubectlDebugCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if hostNode != "" {
			// In --host mode, only accept args after '--' as the container command.
			// Positional args without '--' or before '--' are likely kubectl-style
			// targets (e.g., node/<name>) that would be misinterpreted.
			dashIdx := cmd.ArgsLenAtDash()
			if dashIdx > 0 || (dashIdx == -1 && len(args) > 0) {
				return ErrHostModePositionalArgs
			}

			return runHostDebug(cmd, hostNode, args)
		}

		// Fall through to kubectl debug.
		if originalRunE != nil {
			return originalRunE(cmd, args)
		}

		if originalRun != nil {
			originalRun(cmd, args)
		}

		return nil
	}

	// Clear Run since we use RunE.
	kubectlDebugCmd.Run = nil

	kubectlDebugCmd.Flags().StringVar(
		&hostNode,
		"host",
		"",
		"Node name for host-level debugging (bypasses Kubernetes, targets the infrastructure node directly)",
	)

	kubectlDebugCmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return kubectlDebugCmd
}

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

// ErrHostModePositionalArgs is returned when kubectl-style positional args are
// used with --host mode.
var ErrHostModePositionalArgs = errors.New(
	"--host mode does not accept kubectl-style positional args; " +
		"place the container command after '--' (e.g., debug --host <node> -- /bin/sh)",
)

// runHostDebug detects the cluster distribution and provider, then routes
// to the appropriate host-level debug mechanism.
func runHostDebug(cmd *cobra.Command, nodeName string, args []string) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	contextName := ""
	if cmd.Flags().Lookup("context") != nil {
		contextName, _ = cmd.Flags().GetString("context")
	}

	info, err := clusterdetector.DetectInfo(kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("detect cluster info: %w", err)
	}

	debugImage, _ := cmd.Flags().GetString("image")
	if debugImage == "" {
		debugImage = defaultDebugImage
	}

	switch info.Distribution {
	case v1alpha1.DistributionTalos:
		return runTalosHostDebugFromInfo(
			cmd,
			info,
			kubeconfigPath,
			contextName,
			nodeName,
			debugImage,
			args,
		)
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

		return runDockerHostDebug(cmd, info, nodeName, args)
	case v1alpha1.DistributionEKS:
		// Host-level debug on EKS nodes is not supported via KSail's host-debug
		// path; users should use SSM/SSH directly against the EC2 instances.
		return fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Distribution)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedHostDebug, info.Distribution)
	}
}

// runTalosHostDebugFromInfo resolves the Talos node endpoint and launches a debug container.
func runTalosHostDebugFromInfo(
	cmd *cobra.Command,
	info *clusterdetector.Info,
	kubeconfigPath string,
	contextName string,
	nodeName string,
	image string,
	args []string,
) error {
	talosconfigPath := os.Getenv("TALOSCONFIG")
	if talosconfigPath == "" {
		talosconfigPath = "~/.talos/config"
	}

	nodeEndpoint, err := resolveTalosNodeEndpoint(
		cmd.Context(),
		info,
		kubeconfigPath,
		contextName,
		nodeName,
	)
	if err != nil {
		return err
	}

	return runTalosHostDebug(cmd.Context(), nodeEndpoint, talosconfigPath, image, args)
}

// resolveTalosNodeEndpoint resolves a node name to a Talos API endpoint.
// For Docker provider, it inspects the container's network settings.
// For Hetzner/Omni, it resolves the node IP from the Kubernetes Node object.
func resolveTalosNodeEndpoint(
	ctx context.Context,
	info *clusterdetector.Info,
	kubeconfigPath string,
	contextName string,
	nodeName string,
) (string, error) {
	switch info.Provider {
	case v1alpha1.ProviderDocker:
		return resolveDockerNodeIP(ctx, info.ClusterName, nodeName, dockerprovider.LabelSchemeTalos)
	case v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni:
		return resolveNodeIPFromKubernetes(
			ctx,
			kubeconfigPath,
			contextName,
			nodeName,
			info.Provider,
		)
	case v1alpha1.ProviderAWS:
		// EKS host-level debug is not supported: nodes are AWS EC2 instances
		// reachable only via SSM/SSH, not via Talos or Docker host debug paths.
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
	cmd *cobra.Command,
	info *clusterdetector.Info,
	nodeName string,
	execArgs []string,
) error {
	scheme := distributionToLabelScheme(info.Distribution)

	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	prov := dockerprovider.NewProvider(dockerClient, scheme)

	nodes, err := prov.ListNodes(cmd.Context(), info.ClusterName)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}

	containerName := ""

	for _, node := range nodes {
		if node.Name == nodeName {
			containerName = node.Name

			break
		}
	}

	if containerName == "" {
		availableNames := make([]string, 0, len(nodes))
		for _, n := range nodes {
			availableNames = append(availableNames, n.Name)
		}

		return fmt.Errorf(
			"%w: %q (available nodes: %v)",
			ErrNodeNotFound,
			nodeName,
			availableNames,
		)
	}

	shellCmd := execArgs
	if len(shellCmd) == 0 {
		shellCmd = []string{"/bin/sh"}
	}

	return runInteractiveDockerExec(cmd.Context(), dockerClient, containerName, shellCmd)
}

// runInteractiveDockerExec runs an interactive exec session in a Docker container
// with stdin, stdout, and TTY attached.
func runInteractiveDockerExec(
	ctx context.Context,
	dockerClient dockerclient.APIClient,
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
	dockerClient dockerclient.APIClient,
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

	prov := dockerprovider.NewProvider(dockerClient, scheme)

	nodes, err := prov.ListNodes(ctx, clusterName)
	if err != nil {
		return "", fmt.Errorf("list nodes: %w", err)
	}

	for _, node := range nodes {
		if node.Name == nodeName {
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
	}

	availableNames := make([]string, 0, len(nodes))
	for _, n := range nodes {
		availableNames = append(availableNames, n.Name)
	}

	return "", fmt.Errorf(
		"%w: %q (available nodes: %v)",
		ErrNodeNotFound,
		nodeName,
		availableNames,
	)
}

// distributionToLabelScheme maps a distribution to its Docker provider label scheme.
func distributionToLabelScheme(distribution v1alpha1.Distribution) dockerprovider.LabelScheme {
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
	provider v1alpha1.Provider,
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
	preferExternal := provider == v1alpha1.ProviderHetzner || provider == v1alpha1.ProviderOmni

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
