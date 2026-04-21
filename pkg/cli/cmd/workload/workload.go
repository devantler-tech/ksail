package workload

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v7/pkg/cli/editor"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	reconcilerclient "github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	k8sutil "github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	imagesvc "github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	registryhelpers "github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlio "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/yaml"
)

// NewApplyCmd creates the workload apply command.
func NewApplyCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateApplyCommand(kubeconfigPath)
	})

	// Mark as requiring permission for edit operations.
	// Note: kubectl/helm may have their own confirmation prompts for certain operations.
	// The permission system here is for AI tool execution confirmation.
	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// NewCreateCmd creates the workload create command.
// The runtime parameter is kept for consistency with other workload command constructors,
// though it's currently unused as this command wraps kubectl and flux directly.
func NewCreateCmd(_ *di.Runtime) *cobra.Command {
	// Use a placeholder during command construction.
	// Kubeconfig will be re-resolved in PersistentPreRunE after flags are parsed.
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(nil)

	// Create IO streams for kubectl and flux
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the create command directly
	kubectlClient := kubectl.NewClient(ioStreams)
	createCmd := kubectlClient.CreateCreateCommand(kubeconfigPath)

	// Create flux client and add flux create sub-commands
	fluxClient := flux.NewClient(ioStreams, kubeconfigPath)
	fluxCreateCmd := fluxClient.CreateCreateCommand(kubeconfigPath)

	// Add all flux create sub-commands to the main create command
	for _, subCmd := range fluxCreateCmd.Commands() {
		createCmd.AddCommand(subCmd)
	}

	// Add permission annotation
	if createCmd.Annotations == nil {
		createCmd.Annotations = make(map[string]string)
	}

	createCmd.Annotations[annotations.AnnotationPermission] = "write"

	// Re-resolve kubeconfig after flags are parsed, honoring --config.
	wrapWithKubeconfigResolution(createCmd)

	return createCmd
}

// debounceInterval is the time to wait after the last file event before
// triggering an apply. This prevents redundant reconciles during batch saves.
const debounceInterval = 500 * time.Millisecond

// pollInterval is the time between file modification time scans. Acts as a
// safety net for environments where inotify may miss events (CI runners under
// high I/O load, editors using atomic save via create+rename, etc.).
const pollInterval = 3 * time.Second

// fileSnapshot maps file paths to their last-known modification time.
// Used by the polling fallback to detect changes missed by fsnotify.
type fileSnapshot map[string]time.Time

// debounceState holds the mutable state shared between the event loop and
// debounce timer callbacks.
type debounceState struct {
	timer      *time.Timer
	mutex      sync.Mutex
	lastFile   string
	generation uint64
}

// cancelPendingDebounce increments the generation counter to invalidate any
// pending timer callback and stops the timer if active.
func cancelPendingDebounce(state *debounceState) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation++

	if state.timer != nil {
		state.timer.Stop()
	}
}

// scheduleApply updates the debounce state and (re)starts the timer.
func scheduleApply(state *debounceState, file string, applyCh chan string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.lastFile = file
	state.generation++

	currentGen := state.generation

	if state.timer != nil {
		state.timer.Stop()
	}

	state.timer = time.AfterFunc(debounceInterval, func() {
		enqueueIfCurrent(state, currentGen, applyCh)
	})
}

// enqueueIfCurrent checks whether the generation is still current and, if so,
// coalesces any stale pending apply and enqueues the latest file.
func enqueueIfCurrent(state *debounceState, expectedGen uint64, applyCh chan string) {
	state.mutex.Lock()

	if expectedGen != state.generation {
		state.mutex.Unlock()

		return
	}

	file := state.lastFile
	state.mutex.Unlock()

	// Coalesce: drain any stale pending apply, then enqueue latest.
	// NOTE: safe because the generation guard above ensures only one
	// timer callback is active at any time (single sender).
	select {
	case <-applyCh:
	default:
	}

	select {
	case applyCh <- file:
	default:
	}
}

// NewDeleteCmd creates the workload delete command.
func NewDeleteCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateDeleteCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// NewDescribeCmd creates the workload describe command.
func NewDescribeCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateDescribeCommand(kubeconfigPath)
	})
}

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
		annotations.AnnotationPermission: "write",
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
	isTTY := term.IsTerminal(int(os.Stdin.Fd())) //nolint:gosec

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

// setupRawTerminal sets the terminal to raw mode and returns a restore function.
func setupRawTerminal() (func(), error) {
	stdinFd := int(os.Stdin.Fd()) //nolint:gosec

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

// NewEditCmd creates the workload edit command.
func NewEditCmd() *cobra.Command {
	var editorFlag string

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a resource",
		Long: `Edit a Kubernetes resource from the default editor.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. KUBE_EDITOR, EDITOR, or VISUAL environment variables
  4. Fallback to vim, nano, or vi

Example:
  ksail workload edit deployment/my-deployment
  ksail workload edit --editor "code --wait" deployment/my-deployment`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Set up editor environment variables before edit
			cleanup := editor.SetupEditorEnv(cmd, editorFlag, "workload")
			defer cleanup()

			// Try to load config silently to get kubeconfig path
			kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

			// Create IO streams for kubectl
			ioStreams := genericiooptions.IOStreams{
				In:     os.Stdin,
				Out:    os.Stdout,
				ErrOut: os.Stderr,
			}

			// Create kubectl client and get the edit command directly
			client := kubectl.NewClient(ioStreams)
			editCmd := client.CreateEditCommand(kubeconfigPath)

			// Transfer the context from parent command
			editCmd.SetContext(cmd.Context())

			// Set the args that were passed through
			editCmd.SetArgs(args)

			// Execute kubectl edit command
			return kubectl.ExecuteSafely(cmd.Context(), editCmd)
		},
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().StringVar(
		&editorFlag,
		"editor",
		"",
		"editor command to use (e.g., 'code --wait', 'vim', 'nano')",
	)

	return cmd
}

// NewExecCmd creates the workload exec command.
func NewExecCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateExecCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// NewExplainCmd creates the workload explain command.
func NewExplainCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateExplainCommand(kubeconfigPath)
	})
}

const exportCmdLong = `Export container images from the cluster's containerd runtime to a tar archive.

The exported archive can be used to:
  - Share image sets between development machines
  - Pre-load images for offline development
  - Speed up cluster recreation by avoiding registry pulls

Examples:
  # Export all images from cluster to images.tar (default)
  ksail workload export

  # Export all images to a specific file
  ksail workload export ./backups/my-images.tar

  # Export specific images from cluster
  ksail workload export --image=nginx:latest --image=redis:7

  # Export from a specific kubeconfig context
  ksail workload export --context=kind-dev --kubeconfig=~/.kube/config`

// NewExportCmd creates the image export command.
func NewExportCmd(_ *di.Runtime) *cobra.Command {
	var images []string

	cmd := &cobra.Command{
		Use:          "export [<output>]",
		Short:        "Export container images from the cluster",
		Long:         exportCmdLong,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}

	// Create config manager during command setup to register flags
	// This enables --context, --kubeconfig, and other standard flags
	cfgManager := createImageConfigManager(cmd)

	cmd.Flags().StringArrayVar(&images, "image", nil,
		"Image(s) to export (repeatable); if not specified, all images are exported")

	_ = cfgManager.Viper.BindPFlag("image", cmd.Flags().Lookup("image"))

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runExportCommand(cmd, args, cfgManager, images)
	}

	return cmd
}

func runExportCommand(
	cmd *cobra.Command,
	args []string,
	cfgManager *configmanager.ConfigManager,
	images []string,
) error {
	ctx, err := initImageCommandContext(cmd, cfgManager)
	if err != nil {
		return err
	}

	outputPath := "images.tar"
	if len(args) > 0 {
		outputPath = args[0]
	}

	if len(images) == 0 {
		images = cfgManager.Viper.GetStringSlice("image")
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📤",
		Content: "Export Container Images...",
		Writer:  cmd.OutOrStdout(),
	})

	err = ctx.detectClusterInfo()
	if err != nil {
		return err
	}

	return executeExport(cmd, ctx, images, outputPath)
}

func executeExport(
	cmd *cobra.Command,
	ctx *imageCommandContext,
	images []string,
	outputPath string,
) error {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	exporter := imagesvc.NewExporter(dockerClient)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "exporting images from cluster %s",
		Args:    []any{ctx.ClusterInfo.ClusterName},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = exporter.Export(
		cmd.Context(),
		ctx.ClusterInfo.ClusterName,
		ctx.ClusterInfo.Distribution,
		ctx.ClusterInfo.Provider,
		imagesvc.ExportOptions{
			OutputPath: outputPath,
			Images:     images,
		},
	)
	if err != nil {
		return fmt.Errorf("export images: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "images exported to %s",
		Args:    []any{outputPath},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// NewExposeCmd creates the workload expose command.
func NewExposeCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateExposeCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

// NewGetCmd creates the workload get command.
func NewGetCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateGetCommand(kubeconfigPath)
	})
}

// commandContext holds common command execution context.
type commandContext struct {
	Timer       timer.Timer
	OutputTimer timer.Timer
	ClusterCfg  *v1alpha1.Cluster
}

// initCommandContext initializes common command context (timer, config manager, config loading).
func initCommandContext(cmd *cobra.Command) (*commandContext, error) {
	tmr := timer.New()
	tmr.Start()

	fieldSelectors := configmanager.DefaultClusterFieldSelectors()
	cfgManager := configmanager.NewCommandConfigManager(cmd, fieldSelectors)
	outputTimer := flags.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.Load(configmanagerinterface.LoadOptions{Timer: outputTimer})
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &commandContext{
		Timer:       tmr,
		OutputTimer: outputTimer,
		ClusterCfg:  clusterCfg,
	}, nil
}

// resolveSourceDir determines the source directory from flag, config, or default.
func resolveSourceDir(cfg *v1alpha1.Cluster, pathFlag string) string {
	if dir := strings.TrimSpace(pathFlag); dir != "" {
		return dir
	}

	if dir := strings.TrimSpace(cfg.Spec.Workload.SourceDirectory); dir != "" {
		return dir
	}

	return v1alpha1.DefaultSourceDirectory
}

// writeActivityNotification writes an activity notification message.
func writeActivityNotification(
	content string,
	outputTimer timer.Timer,
	writer io.Writer,
) {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: content,
		Timer:   outputTimer,
		Writer:  writer,
	})
}

// imageCommandContext holds shared state for image commands.
type imageCommandContext struct {
	Timer       timer.Timer
	OutputTimer timer.Timer
	ClusterCfg  *v1alpha1.Cluster
	ClusterInfo *clusterdetector.Info
}

// createImageConfigManager creates a config manager for image commands.
// Only includes --context and --kubeconfig flags since image commands
// detect the distribution from the running cluster.
func createImageConfigManager(cmd *cobra.Command) *configmanager.ConfigManager {
	fieldSelectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultContextFieldSelector(),
		configmanager.DefaultKubeconfigFieldSelector(),
	}

	return configmanager.NewCommandConfigManager(cmd, fieldSelectors)
}

// initImageCommandContext initializes the shared context for image commands.
// It loads the config using the provided config manager, skipping validation
// since image commands detect cluster info from the running cluster.
func initImageCommandContext(
	cmd *cobra.Command,
	cfgManager *configmanager.ConfigManager,
) (*imageCommandContext, error) {
	tmr := timer.New()
	tmr.Start()

	outputTimer := flags.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.Load(
		configmanagerinterface.LoadOptions{Silent: true, SkipValidation: true},
	)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &imageCommandContext{
		Timer:       tmr,
		OutputTimer: outputTimer,
		ClusterCfg:  clusterCfg,
	}, nil
}

// detectClusterInfo detects the cluster info after printing the header.
// This should be called after initImageCommandContext and after printing the title.
func (ctx *imageCommandContext) detectClusterInfo() error {
	ctx.Timer.NewStage()

	clusterInfo, err := clusterdetector.DetectInfo(
		ctx.ClusterCfg.Spec.Cluster.Connection.Kubeconfig,
		ctx.ClusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("detect cluster info: %w", err)
	}

	ctx.ClusterInfo = clusterInfo

	return nil
}

// ErrUnknownOutputFormat is returned when an unrecognized output format is specified.
var ErrUnknownOutputFormat = errors.New("unknown output format")

const imagesCmdLong = `List container images required by the configured cluster components.

The image list is derived from the ksail.yaml configuration and includes
images for all enabled components (GitOps engine, CNI, policy engine, etc.).

This command is useful for:
  - Pre-pulling images before cluster creation
  - Creating offline image archives
  - Understanding infrastructure image requirements
  - CI/CD caching strategies

Output formats:
  - plain: One image per line (default, suitable for scripting)
  - json: JSON array of image strings

Examples:
  # List all images for current ksail.yaml config
  ksail workload images

  # List images as JSON array
  ksail workload images --output=json

  # Pipe to docker pull
  ksail workload images | xargs -n1 docker pull

  # Save to file for CI caching
  ksail workload images > required-images.txt`

// NewImagesCmd creates the command to list required container images.
func NewImagesCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:          "images",
		Short:        "List container images required by cluster components",
		Long:         imagesCmdLong,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}

	// Create config manager with full field selectors to detect all components
	cfgManager := createImagesConfigManager(cmd)

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "plain",
		"Output format: plain, json")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runImagesCommand(cmd, cfgManager, outputFormat)
	}

	return cmd
}

// createImagesConfigManager creates a config manager for the images command.
// It needs to load the full cluster config to understand which components are enabled.
func createImagesConfigManager(cmd *cobra.Command) *configmanager.ConfigManager {
	fieldSelectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultDistributionFieldSelector(),
		configmanager.DefaultProviderFieldSelector(),
		configmanager.DefaultCNIFieldSelector(),
		configmanager.DefaultCSIFieldSelector(),
		configmanager.DefaultMetricsServerFieldSelector(),
		configmanager.DefaultLoadBalancerFieldSelector(),
		configmanager.DefaultCertManagerFieldSelector(),
		configmanager.DefaultPolicyEngineFieldSelector(),
		configmanager.DefaultGitOpsEngineFieldSelector(),
	}

	return configmanager.NewCommandConfigManager(cmd, fieldSelectors)
}

func runImagesCommand(
	cmd *cobra.Command,
	cfgManager *configmanager.ConfigManager,
	outputFormat string,
) error {
	tmr := timer.New()
	tmr.Start()

	outputTimer := flags.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.Load(configmanagerinterface.LoadOptions{
		Silent:         true,
		SkipValidation: true,
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create Helm client for dynamic image extraction from Helm charts
	// Uses template-only client that doesn't require kubeconfig
	helmClient, err := helm.NewTemplateOnlyClient()
	if err != nil {
		return fmt.Errorf("create helm client: %w", err)
	}

	// Use Factory to get images dynamically from Helm charts
	factory := installer.NewFactory(
		helmClient,
		nil, // dockerClient not needed for image extraction
		"",  // kubeconfig not needed for image extraction
		"",  // kubecontext not needed for image extraction
		0,
		clusterCfg.Spec.Cluster.Distribution,
	)

	images, err := factory.GetImagesForCluster(cmd.Context(), clusterCfg)
	if err != nil {
		return fmt.Errorf("extract images from installers: %w", err)
	}

	// Sort and deduplicate for consistent, stable output
	slices.Sort(images)
	images = slices.Compact(images)

	// Output based on format
	switch strings.ToLower(outputFormat) {
	case "json":
		return outputJSON(cmd, images, outputTimer)
	case "plain", "":
		return outputPlain(cmd, images, outputTimer)
	default:
		return fmt.Errorf("%w: %s (valid: plain, json)", ErrUnknownOutputFormat, outputFormat)
	}
}

func outputPlain(cmd *cobra.Command, images []string, tmr timer.Timer) error {
	if len(images) == 0 {
		// Write warning to stderr to keep stdout clean for scripting (e.g., xargs docker pull)
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "no images required for current configuration",
			Timer:   tmr,
			Writer:  cmd.ErrOrStderr(),
		})

		return nil
	}

	for _, img := range images {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), img)
		if err != nil {
			return fmt.Errorf("write image to stdout: %w", err)
		}
	}

	return nil
}

func outputJSON(cmd *cobra.Command, images []string, _ timer.Timer) error {
	data, err := json.Marshal(images)
	if err != nil {
		return fmt.Errorf("marshal images to JSON: %w", err)
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	if err != nil {
		return fmt.Errorf("write JSON to stdout: %w", err)
	}

	return nil
}

const importCmdLong = `Import container images from a tar archive to the cluster's containerd runtime.

Images are imported to all nodes in the cluster, making them available for
pod scheduling without requiring registry pulls.

Examples:
  # Import images from images.tar (default)
  ksail workload import

  # Import images from a specific file
  ksail workload import ./backups/my-images.tar

  # Import to a specific kubeconfig context
  ksail workload import --context=kind-dev --kubeconfig=~/.kube/config`

// NewImportCmd creates the image import command.
func NewImportCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "import [<input>]",
		Short:        "Import container images to the cluster",
		Long:         importCmdLong,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	// Create config manager during command setup to register flags
	// This enables --context, --kubeconfig, and other standard flags
	cfgManager := createImageConfigManager(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runImportCommand(cmd, args, cfgManager)
	}

	return cmd
}

func runImportCommand(
	cmd *cobra.Command,
	args []string,
	cfgManager *configmanager.ConfigManager,
) error {
	ctx, err := initImageCommandContext(cmd, cfgManager)
	if err != nil {
		return err
	}

	inputPath := "images.tar"
	if len(args) > 0 {
		inputPath = args[0]
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📥",
		Content: "Import Container Images...",
		Writer:  cmd.OutOrStdout(),
	})

	err = ctx.detectClusterInfo()
	if err != nil {
		return err
	}

	return executeImport(cmd, ctx, inputPath)
}

func executeImport(
	cmd *cobra.Command,
	ctx *imageCommandContext,
	inputPath string,
) error {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	importer := imagesvc.NewImporter(dockerClient)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "importing images to cluster %s",
		Args:    []any{ctx.ClusterInfo.ClusterName},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = importer.Import(
		cmd.Context(),
		ctx.ClusterInfo.ClusterName,
		ctx.ClusterInfo.Distribution,
		ctx.ClusterInfo.Provider,
		imagesvc.ImportOptions{
			InputPath: inputPath,
		},
	)
	if err != nil {
		return fmt.Errorf("import images: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "images imported from %s",
		Args:    []any{inputPath},
		Timer:   ctx.OutputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

const requiredInstallArgs = 2

// NewInstallCmd creates the workload install command.
func NewInstallCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [NAME] [CHART]",
		Short: "Install Helm charts",
		Long: "Install Helm charts to provision workloads through KSail. " +
			"This command provides native Helm chart installation capabilities.",
		Args: cobra.ExactArgs(requiredInstallArgs),
		RunE: runInstall,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	flags := cmd.Flags()
	flags.StringP("namespace", "n", "default", "namespace scope for the request")
	flags.String("version", "", "chart version constraint (default: latest)")
	flags.Duration(
		"timeout",
		helm.DefaultTimeout,
		"time to wait for any individual Kubernetes operation",
	)
	flags.Bool("create-namespace", false, "create the release namespace if not present")
	flags.Bool("wait", false, "wait until resources are ready")
	flags.Bool("atomic", false, "if set, the installation deletes on failure")

	return cmd
}

func runInstall(cmd *cobra.Command, args []string) error {
	releaseName := args[0]
	chartName := args[1]

	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	client, err := helm.NewClient(kubeconfigPath, "")
	if err != nil {
		return fmt.Errorf("create helm client: %w", err)
	}

	spec := buildChartSpec(cmd, releaseName, chartName)

	_, err = client.InstallChart(cmd.Context(), spec)
	if err != nil {
		return fmt.Errorf("install chart %q: %w", chartName, err)
	}

	return nil
}

func buildChartSpec(cmd *cobra.Command, releaseName, chartName string) *helm.ChartSpec {
	namespace, _ := cmd.Flags().GetString("namespace")
	if namespace == "" {
		namespace = "default"
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	version, _ := cmd.Flags().GetString("version")
	createNamespace, _ := cmd.Flags().GetBool("create-namespace")
	wait, _ := cmd.Flags().GetBool("wait")
	atomic, _ := cmd.Flags().GetBool("atomic")

	return &helm.ChartSpec{
		ReleaseName:     releaseName,
		ChartName:       chartName,
		Namespace:       namespace,
		Timeout:         timeout,
		Version:         version,
		CreateNamespace: createNamespace,
		Wait:            wait,
		Atomic:          atomic,
	}
}

// kubectlCommandCreator is a function that creates a kubectl command given a client and kubeconfig path.
type kubectlCommandCreator func(client *kubectl.Client, kubeconfigPath string) *cobra.Command

// newKubectlCommand creates a kubectl wrapper command using the provided command creator.
// The kubeconfig path is resolved lazily via a PersistentPreRunE hook so that the
// --config persistent flag is honored after cobra has parsed all flags.
func newKubectlCommand(creator kubectlCommandCreator) *cobra.Command {
	// Use a placeholder during command construction so cobra can build the
	// command tree.  The actual kubeconfig path will be resolved in
	// PersistentPreRunE before the command runs.
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	cmd := creator(client, kubeconfig.GetKubeconfigPathSilently(nil))

	wrapWithKubeconfigResolution(cmd)

	return cmd
}

// wrapWithKubeconfigResolution adds a PersistentPreRunE hook that re-resolves the
// kubeconfig path after cobra has parsed all flags, honoring the --config flag.
// It chains to any existing PersistentPreRunE or PersistentPreRun on the command.
func wrapWithKubeconfigResolution(cmd *cobra.Command) {
	origPersistentPreRunE := cmd.PersistentPreRunE
	origPersistentPreRun := cmd.PersistentPreRun

	cmd.PersistentPreRunE = func(child *cobra.Command, args []string) error {
		// Refresh expired Omni kubeconfig tokens before resolving the path,
		// so path resolution picks up the freshly written kubeconfig.
		kubeconfighook.MaybeRefreshOmniKubeconfig(child)

		resolvedPath := kubeconfig.GetKubeconfigPathSilently(child)

		kubeconfigFlag := child.Flags().Lookup("kubeconfig")
		if kubeconfigFlag != nil && !child.Flags().Changed("kubeconfig") {
			err := kubeconfigFlag.Value.Set(resolvedPath)
			if err != nil {
				return fmt.Errorf("failed to set kubeconfig flag: %w", err)
			}

			kubeconfigFlag.DefValue = resolvedPath
		}

		if origPersistentPreRunE != nil {
			return origPersistentPreRunE(child, args)
		}

		if origPersistentPreRun != nil {
			origPersistentPreRun(child, args)
		}

		return nil
	}

	cmd.PersistentPreRun = nil
}

// NewLogsCmd creates the workload logs command.
func NewLogsCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateLogsCommand(kubeconfigPath)
	})
}

// NewPushCmd creates the workload push command.
func NewPushCmd(_ *di.Runtime) *cobra.Command {
	var (
		validate bool
		pathFlag string
	)

	// Create viper instance for registry flag/env binding (local to closure)
	viperInstance := viper.New()
	viperInstance.SetEnvPrefix(configmanager.EnvPrefix)
	viperInstance.AutomaticEnv()

	cmd := &cobra.Command{
		Use:          "push [oci://<host>:<port>/<repository>[/<variant>]:<ref>]",
		Short:        "Package and push an OCI artifact to a registry",
		Long:         pushCommandLongDescription(),
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runPushCommand(cmd, args, pathFlag, validate, viperInstance)
	}

	configurePushFlags(cmd, &validate, &pathFlag, viperInstance)

	return cmd
}

// pushCommandLongDescription returns the long description for the push command.
func pushCommandLongDescription() string {
	return `Build and push local workloads as an OCI artifact to a registry.

The OCI reference format is: oci://<host>:<port>/<repository>[/<variant>]:<ref>

Examples:
  # Push to auto-detected local registry with defaults
  ksail workload push

  # Push specific directory to auto-detected registry
  ksail workload push --path=./manifests

  # Push to explicit registry endpoint
  ksail workload push oci://localhost:5050/k8s:dev

  # Push to external registry with credentials
  ksail workload push --registry='$USER:$TOKEN@ghcr.io/org/repo'

  # Push using KSAIL_REGISTRY environment variable
  KSAIL_REGISTRY='ghcr.io/org/repo' ksail workload push

  # Push with variant (subdirectory in repository)
  ksail workload push oci://localhost:5050/my-app/base:v1.0.0 --path=./k8s

All parts of the OCI reference are optional and will be inferred:
  - host:port: Auto-detected from running local-registry container
  - repository: Derived from source directory name
  - ref: Defaults to "dev"`
}

// configurePushFlags configures flags for the push command.
func configurePushFlags(
	cmd *cobra.Command,
	validate *bool,
	pathFlag *string,
	viperInstance *viper.Viper,
) {
	cmd.Flags().BoolVar(validate, "validate", false, "Validate manifests before pushing")
	cmd.Flags().StringVar(pathFlag, "path", "", "Source directory containing manifests to push")
	cmd.Flags().String(
		"registry",
		"",
		"Registry to push to (format: [user:pass@]host[:port][/path]), can also be set via KSAIL_REGISTRY env var",
	)

	// Bind registry flag to viper for env var support (KSAIL_REGISTRY)
	_ = viperInstance.BindPFlag(registryhelpers.ViperRegistryKey, cmd.Flags().Lookup("registry"))
}

// runPushCommand executes the push logic with the provided parameters.
func runPushCommand(
	cmd *cobra.Command,
	args []string,
	pathFlag string,
	validate bool,
	viperInstance *viper.Viper,
) error {
	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	clusterCfg := cmdCtx.ClusterCfg
	outputTimer := cmdCtx.OutputTimer
	tmr := cmdCtx.Timer

	// Parse OCI reference if provided
	var ociRef *oci.Reference
	if len(args) > 0 {
		ociRef, err = oci.ParseReference(args[0])
		if err != nil {
			return fmt.Errorf("parse OCI reference: %w", err)
		}
	}

	// Resolve all parameters: host, port, repository, ref, source directory
	params, err := resolvePushParams(
		cmd, clusterCfg, ociRef, pathFlag, viperInstance, tmr, outputTimer,
	)
	if err != nil {
		return err
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "📦",
		Content: "Build and Push OCI Artifact...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	// Validate if flag is set or config option is enabled
	if validate || clusterCfg.Spec.Workload.ValidateOnPush {
		validateErr := validateManifests(cmd, params.SourceDir, outputTimer)
		if validateErr != nil {
			return validateErr
		}
	}

	return buildAndPushArtifact(cmd, params, outputTimer)
}

// validateManifests runs manifest validation and reports progress.
func validateManifests(cmd *cobra.Command, sourceDir string, outputTimer timer.Timer) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "validating manifests",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err := runValidateCmd(
		cmd.Context(),
		cmd,
		[]string{sourceDir},
		true, // skipSecrets
		true, // strict
		true, // ignoreMissingSchemas
	)
	if err != nil {
		return fmt.Errorf("validate manifests: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "manifests validated",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// buildAndPushArtifact builds an OCI artifact from the source directory and pushes it.
func buildAndPushArtifact(
	cmd *cobra.Command,
	params *pushParams,
	outputTimer timer.Timer,
) error {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "building oci artifact",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	registryDisplay, registryEndpoint := formatRegistryEndpoints(params)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing to %s",
		Args:    []any{registryDisplay},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	builder := oci.NewWorkloadArtifactBuilder()

	_, err := builder.Build(cmd.Context(), oci.BuildOptions{
		Name:             params.Repository,
		SourcePath:       params.SourceDir,
		RegistryEndpoint: registryEndpoint,
		Repository:       params.Repository,
		Version:          params.Ref,
		GitOpsEngine:     params.GitOpsEngine,
		Username:         params.Username,
		Password:         params.Password,
	})
	if err != nil {
		return fmt.Errorf("build and push oci artifact: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "oci artifact pushed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// formatRegistryEndpoints returns display and endpoint strings for a registry.
func formatRegistryEndpoints(params *pushParams) (string, string) {
	if params.Port > 0 {
		return fmt.Sprintf(
				"%s:%d/%s:%s", params.Host, params.Port, params.Repository, params.Ref,
			),
			fmt.Sprintf("%s:%d", params.Host, params.Port)
	}

	// External registry - no port (HTTPS implicit)
	return fmt.Sprintf("%s/%s:%s", params.Host, params.Repository, params.Ref),
		params.Host
}

// pushParams holds all resolved parameters for the push operation.
type pushParams struct {
	Host         string
	Port         int32
	Repository   string
	Ref          string
	SourceDir    string
	GitOpsEngine v1alpha1.GitOpsEngine
	Username     string

	Password   string
	IsExternal bool // True if this is an external registry (no auto-detection needed)
}

// resolvePushParams resolves all push parameters using priority-based detection.
//
// Priority order for registry resolution:
// 1. CLI flag or env var via Viper (--registry / KSAIL_REGISTRY)
// 2. Config file (ksail.yaml localRegistry)
// 3. Cluster GitOps resources (FluxInstance or ArgoCD Application)
// 4. Docker containers (matching cluster name)
// 5. Error (no registry found).
func resolvePushParams(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	ociRef *oci.Reference,
	pathFlag string,
	viperInstance *viper.Viper,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (*pushParams, error) {
	// If OCI reference is fully specified, use it directly without detection
	if ociRef != nil && ociRef.Host != "" && ociRef.Port > 0 {
		return newPushParamsFromOCIRef(cfg, ociRef, pathFlag)
	}

	registryInfo, err := detectRegistry(cmd, cfg, viperInstance, tmr, outputTimer)
	if err != nil {
		return nil, err
	}

	// Build params from detected registry info
	sourceDir := resolveSourceDir(cfg, pathFlag)

	// Canonicalize source directory (resolve symlinks + absolute) so that
	// the OCI builder targets the real directory and symlink-escape attacks
	// are prevented in CI pipelines processing external manifests.
	canonDir, canonErr := fsutil.EvalCanonicalPath(sourceDir)
	if canonErr != nil {
		return nil, fmt.Errorf("resolve source directory %q: %w", sourceDir, canonErr)
	}

	params := &pushParams{
		Host:       registryInfo.Host,
		Port:       registryInfo.Port,
		Repository: registryInfo.Repository,
		Username:   registryInfo.Username,
		Password:   registryInfo.Password,
		IsExternal: registryInfo.IsExternal,
		SourceDir:  canonDir,
		Ref:        resolveRef(ociRef, cfg.Spec.Workload.Tag, registryInfo.Tag),
	}

	// Override with OCI reference values if provided
	applyOCIRefOverrides(params, ociRef)

	// Fallback repository from source directory if not set.
	// Use the original (pre-canonicalized) sourceDir so the repo name
	// reflects the user-supplied relative path, not the absolute filesystem path.
	if params.Repository == "" {
		params.Repository = registry.SanitizeRepoName(sourceDir)
	}

	// Resolve GitOps engine
	params.GitOpsEngine = resolveGitOpsEngine(cfg)

	// Show success message with source using proper host:port formatting
	displayURL := registryhelpers.FormatRegistryURL(params.Host, params.Port, params.Repository)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "%s (from %s)",
		Args:    []any{displayURL, registryInfo.Source},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return params, nil
}

// detectRegistry shows the detection UI and resolves registry information.
func detectRegistry(
	cmd *cobra.Command,
	cfg *v1alpha1.Cluster,
	viperInstance *viper.Viper,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (*registryhelpers.Info, error) {
	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔎",
		Content: "Get registry details...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "resolving registry configuration",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	registryInfo, err := registryhelpers.ResolveRegistry(
		cmd.Context(),
		registryhelpers.ResolveRegistryOptions{
			Viper:         viperInstance,
			ClusterConfig: cfg,
			ClusterName:   cfg.Spec.Cluster.Connection.Context,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("resolve registry: %w", err)
	}

	return registryInfo, nil
}

// applyOCIRefOverrides applies OCI reference values to params if provided.
func applyOCIRefOverrides(params *pushParams, ociRef *oci.Reference) {
	if ociRef == nil {
		return
	}

	if ociRef.Host != "" {
		params.Host = ociRef.Host
	}

	if ociRef.Port > 0 {
		params.Port = ociRef.Port
	}

	if ociRef.FullRepository() != "" {
		params.Repository = ociRef.FullRepository()
	}

	if ociRef.Ref != "" {
		params.Ref = ociRef.Ref
	}
}

// newPushParamsFromOCIRef creates push params when a complete OCI reference is provided.
func newPushParamsFromOCIRef(
	cfg *v1alpha1.Cluster,
	ociRef *oci.Reference,
	pathFlag string,
) (*pushParams, error) {
	sourceDir := resolveSourceDir(cfg, pathFlag)

	// Canonicalize source directory (resolve symlinks + absolute) so that
	// the OCI builder targets the real directory and symlink-escape attacks
	// are prevented in CI pipelines processing external manifests.
	canonDir, err := fsutil.EvalCanonicalPath(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve source directory %q: %w", sourceDir, err)
	}

	return &pushParams{
		Host:         ociRef.Host,
		Port:         ociRef.Port,
		Repository:   ociRef.FullRepository(),
		Ref:          ociRef.Ref,
		SourceDir:    canonDir,
		GitOpsEngine: resolveGitOpsEngine(cfg),
		IsExternal:   false,
	}, nil
}

// resolveRef determines the artifact ref/tag from the OCI ref, workload tag, registry-embedded tag, or default.
// Priority: OCI ref > workload tag > registry-embedded tag > default.
func resolveRef(ociRef *oci.Reference, workloadTag string, registryTag string) string {
	if ociRef != nil && ociRef.Ref != "" {
		return ociRef.Ref
	}

	if workloadTag != "" {
		return workloadTag
	}

	if registryTag != "" {
		return registryTag
	}

	return registry.DefaultLocalArtifactTag
}

// resolveGitOpsEngine determines GitOps engine from config.
func resolveGitOpsEngine(cfg *v1alpha1.Cluster) v1alpha1.GitOpsEngine {
	if cfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineNone {
		return cfg.Spec.Cluster.GitOpsEngine
	}

	return v1alpha1.GitOpsEngineNone
}

// Shared errors.
//
//nolint:staticcheck // "GitOps" is a proper noun and must be capitalized
var errGitOpsEngineRequired = errors.New(
	"A GitOps engine must be enabled to reconcile workloads; " +
		"enable it with '--gitops-engine Flux|ArgoCD' during cluster init or " +
		"set 'spec.gitOpsEngine: Flux|ArgoCD' in ksail.yaml",
)

// Shared constants for reconciliation.
const (
	defaultReconcileTimeout       = 5 * time.Minute
	fluxKustomizationPollInterval = 500 * time.Millisecond
	argoCDApplicationPollInterval = 500 * time.Millisecond
	reconcileConcurrency          = 5
	reconcileCmdLong              = "Trigger reconciliation/sync and wait for completion. " +
		"For Flux, tracks the OCIRepository and each Kustomization individually. " +
		"For ArgoCD, tracks each Application until synced and healthy."
	// kwokReconcileSkipMsg is emitted when reconciliation is skipped for KWOK.
	// KWOK simulates GitOps controller pods as Running at the API level, but the
	// actual controller processes are not running and cannot sync any resources.
	kwokReconcileSkipMsg = "KWOK distribution: GitOps controllers are simulated and cannot sync — reconciliation skipped"
)

// getKubeconfigPath returns the kubeconfig path from config or default.
func getKubeconfigPath(clusterCfg *v1alpha1.Cluster) (string, error) {
	kubeconfigPath := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Kubeconfig)
	if kubeconfigPath == "" {
		kubeconfigPath = v1alpha1.DefaultKubeconfigPath
	}

	expanded, err := fsutil.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("expand kubeconfig path: %w", err)
	}

	return expanded, nil
}

// NewReconcileCmd creates the workload reconcile command.
func NewReconcileCmd(_ *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Trigger reconciliation for GitOps workloads",
		Long:         reconcileCmdLong,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().Duration(
		"timeout",
		0,
		"timeout for waiting for reconciliation to complete (overrides config timeout)",
	)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runReconcile(cmd)
	}

	return cmd
}

// runReconcile executes the reconcile command logic.
func runReconcile(cmd *cobra.Command) error {
	ctx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	clusterCfg := ctx.ClusterCfg
	outputTimer := ctx.OutputTimer
	tmr := ctx.Timer

	// Determine GitOps engine - use config if set, otherwise auto-detect
	gitOpsEngine := clusterCfg.Spec.Cluster.GitOpsEngine
	if gitOpsEngine == v1alpha1.GitOpsEngineNone || gitOpsEngine == "" {
		detected, detectErr := autoDetectGitOpsEngine(cmd, tmr, outputTimer)
		if detectErr != nil {
			return detectErr
		}

		gitOpsEngine = detected
	}

	timeout, err := getReconcileTimeout(cmd, clusterCfg)
	if err != nil {
		return err
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔄",
		Content: "Trigger Reconciliation...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	err = executeReconciliation(cmd, clusterCfg, gitOpsEngine, timeout, outputTimer)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "reconciliation completed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// autoDetectGitOpsEngine detects the GitOps engine from the cluster.
func autoDetectGitOpsEngine(
	cmd *cobra.Command,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (v1alpha1.GitOpsEngine, error) {
	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔎",
		Content: "Auto-detect GitOps engine...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "detecting gitops engine in cluster",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	engine, err := registryhelpers.DetectGitOpsEngine(cmd.Context())
	if err != nil {
		return v1alpha1.GitOpsEngineNone, fmt.Errorf("detect gitops engine: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "%s detected",
		Args:    []any{engine},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return engine, nil
}

// getReconcileTimeout determines the timeout from flag, config, or default.
func getReconcileTimeout(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) (time.Duration, error) {
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return 0, fmt.Errorf("get timeout flag: %w", err)
	}

	if timeout == 0 {
		if clusterCfg.Spec.Cluster.Connection.Timeout.Duration > 0 {
			timeout = clusterCfg.Spec.Cluster.Connection.Timeout.Duration
		} else {
			timeout = defaultReconcileTimeout
		}
	}

	return timeout, nil
}

// executeReconciliation runs the appropriate reconciliation based on GitOps engine.
func executeReconciliation(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	gitOpsEngine v1alpha1.GitOpsEngine,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	kubeconfigPath, err := getKubeconfigPath(clusterCfg)
	if err != nil {
		return err
	}

	// Check both config and detected distribution for KWOK.
	// When no ksail.yaml exists (e.g. init=false), clusterCfg.Spec.Cluster.Distribution
	// defaults to empty; fall back to detecting from the active kubeconfig context.
	// Only detect when dist is unspecified so an explicitly-configured distribution
	// is never overridden by kubeconfig-based detection.
	dist := clusterCfg.Spec.Cluster.Distribution
	if dist == "" {
		info, detectErr := clusterdetector.DetectInfo(kubeconfigPath, "")
		if detectErr == nil {
			dist = info.Distribution
		}
	}

	if dist == v1alpha1.DistributionKWOK {
		notify.Warningf(cmd.OutOrStdout(), kwokReconcileSkipMsg)

		return nil
	}

	switch gitOpsEngine {
	case v1alpha1.GitOpsEngineArgoCD:
		return reconcileArgoCD(cmd, kubeconfigPath, timeout, outputTimer)
	case v1alpha1.GitOpsEngineFlux:
		return reconcileFlux(cmd, kubeconfigPath, timeout, outputTimer)
	case v1alpha1.GitOpsEngineNone:
		return errGitOpsEngineRequired
	default:
		return errGitOpsEngineRequired
	}
}

// reconcileFlux triggers and waits for Flux reconciliation using the client reconciler.
// It uses ProgressGroup to show per-resource reconciliation status in real-time.
func reconcileFlux(
	cmd *cobra.Command,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	fluxReconciler, err := flux.NewReconciler(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create flux reconciler: %w", err)
	}

	writer := cmd.OutOrStdout()

	deadlineCtx, deadlineCancel := context.WithTimeout(cmd.Context(), timeout)
	defer deadlineCancel()

	deadline, _ := deadlineCtx.Deadline()

	// Sub-phase 1: OCI source reconciliation
	writeActivityNotification("reconciling oci source...", outputTimer, writer)

	err = fluxReconciler.TriggerOCIRepositoryReconciliation(deadlineCtx)
	if err != nil {
		return fmt.Errorf("trigger oci repository reconciliation: %w", err)
	}

	ociGroup := notify.NewProgressGroup(
		"",
		"",
		writer,
		notify.WithLabels(notify.ReconcilingLabels()),
		notify.WithTimer(outputTimer),
		notify.WithAppendOnly(),
	)

	err = ociGroup.Run(deadlineCtx, notify.ProgressTask{
		Name: "flux-system",
		Fn: func(ctx context.Context) error {
			return fluxReconciler.WaitForOCIRepositoryReady(ctx, time.Until(deadline))
		},
	})
	if err != nil {
		return fmt.Errorf("reconcile oci source: %w", err)
	}

	// Sub-phase 2: Kustomization reconciliation with per-resource tracking
	writeActivityNotification("reconciling kustomizations...", outputTimer, writer)

	err = fluxReconciler.TriggerKustomizationReconciliation(deadlineCtx)
	if err != nil {
		return fmt.Errorf("trigger kustomization reconciliation: %w", err)
	}

	err = reconcileFluxKustomizationsWithProgress(deadlineCtx, cmd, fluxReconciler, outputTimer)
	if err != nil {
		return err
	}

	return nil
}

// failedKustomizations tracks kustomizations that have permanently failed
// during reconciliation. This enables fail-fast for dependent kustomizations:
// when an upstream kustomization fails, all dependents fail immediately
// instead of waiting for the full timeout.
type failedKustomizations struct {
	m sync.Map
}

// record stores a permanent failure for the named kustomization.
func (f *failedKustomizations) record(name string, err error) {
	f.m.Store(name, err)
}

// checkDependencies returns an error if any dependency has permanently failed.
// Returns nil if all dependencies are still healthy or pending.
func (f *failedKustomizations) checkDependencies(dependsOn []string) error {
	for _, dep := range dependsOn {
		if val, ok := f.m.Load(dep); ok {
			depErr, ok := val.(error)
			if !ok {
				continue
			}

			return fmt.Errorf(
				"dependency %q failed: %w - fix the upstream kustomization first",
				dep, depErr,
			)
		}
	}

	return nil
}

// reconcileFluxKustomizationsWithProgress lists all Flux Kustomizations, sorts
// them in topological (dependency) order, and monitors each individually using
// a ProgressGroup. Flux's controller handles the actual dependency-driven
// triggering; we just poll and display status.
//
// A shared failure tracker propagates permanent failures to dependents: when an
// upstream kustomization fails, all downstream dependents fail immediately
// instead of waiting for the full timeout.
func reconcileFluxKustomizationsWithProgress(
	deadlineCtx context.Context,
	cmd *cobra.Command,
	fluxReconciler *flux.Reconciler,
	outputTimer timer.Timer,
) error {
	kustomizations, err := fluxReconciler.ListKustomizations(deadlineCtx)
	if err != nil {
		return fmt.Errorf("list kustomizations: %w", err)
	}

	if len(kustomizations) == 0 {
		return nil
	}

	sorted := topologicalSortKustomizations(kustomizations)

	var failed failedKustomizations

	tasks := make([]notify.ProgressTask, 0, len(sorted))
	for _, kustomization := range sorted {
		name := kustomization.Name
		deps := kustomization.DependsOn

		tasks = append(tasks, notify.ProgressTask{
			Name: name,
			Fn: func(ctx context.Context) error {
				return pollUntilKustomizationReady(ctx, fluxReconciler, name, deps, &failed)
			},
		})
	}

	ksGroup := notify.NewProgressGroup(
		"",
		"",
		cmd.OutOrStdout(),
		notify.WithLabels(notify.ReconcilingLabels()),
		notify.WithTimer(outputTimer),
		notify.WithContinueOnError(),
		notify.WithAppendOnly(),
		notify.WithCountLabel("kustomizations"),
		notify.WithConcurrency(reconcileConcurrency),
	)

	err = ksGroup.Run(deadlineCtx, tasks...)
	if err != nil {
		return fmt.Errorf("reconcile kustomizations: %w", err)
	}

	return nil
}

// pollUntilKustomizationReady polls a named Flux Kustomization until it is
// ready or the context's deadline expires. On permanent failure, it returns an
// actionable error including the resource name and failure reason, and records
// the failure in the shared tracker so dependents can fail-fast.
//
// On each poll iteration, it first checks whether any dependency has already
// permanently failed (tracked via the shared failedKustomizations). If so, the
// polling stops immediately instead of waiting for the timeout.
func pollUntilKustomizationReady(
	ctx context.Context,
	fluxReconciler *flux.Reconciler,
	name string,
	dependsOn []string,
	failed *failedKustomizations,
) error {
	ticker := time.NewTicker(fluxKustomizationPollInterval)
	defer ticker.Stop()

	var lastStatus string

	for {
		// Fail-fast: check if any dependency has permanently failed.
		depErr := failed.checkDependencies(dependsOn)
		if depErr != nil {
			// Record cascaded failure so further dependents also fail-fast.
			failed.record(name, depErr)

			return depErr
		}

		ready, status, err := fluxReconciler.CheckNamedKustomizationReady(ctx, name)
		if err != nil {
			if reconcilerclient.IsContextError(err) {
				if errors.Is(ctx.Err(), context.Canceled) {
					return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
				}

				return kustomizationReadinessTimeoutError(name, lastStatus)
			}

			// Record permanent failure so dependents can fail-fast.
			failed.record(name, err)

			return fmt.Errorf("permanent failure: %w", err)
		}

		if ready {
			return nil
		}

		lastStatus = status

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
			}

			return kustomizationReadinessTimeoutError(name, lastStatus)
		case <-ticker.C:
		}
	}
}

// kustomizationReadinessTimeoutError returns an actionable error for a
// kustomization that did not become ready within the timeout.
func kustomizationReadinessTimeoutError(name, lastStatus string) error {
	if lastStatus != "" {
		return fmt.Errorf(
			"%w (last status: %s) — "+
				"run 'ksail workload get kustomizations.kustomize.toolkit.fluxcd.io %s -n flux-system' to inspect",
			flux.ErrReconcileTimeout, lastStatus, name,
		)
	}

	return fmt.Errorf(
		"%w — "+
			"run 'ksail workload get kustomizations.kustomize.toolkit.fluxcd.io %s -n flux-system' to inspect",
		flux.ErrReconcileTimeout, name,
	)
}

// topologicalSortKustomizations returns kustomizations in topological order
// (dependencies before dependents) for display purposes.
// Uses Kahn's algorithm. If cycles are detected, remaining items are appended
// in their original order so the ProgressGroup still shows all resources.
//
//nolint:cyclop // Kahn's algorithm has inherent branching; complexity is structural, not avoidable.
func topologicalSortKustomizations(
	kustomizations []flux.KustomizationInfo,
) []flux.KustomizationInfo {
	if len(kustomizations) <= 1 {
		return kustomizations
	}

	byName := make(map[string]flux.KustomizationInfo, len(kustomizations))
	inDegree := make(map[string]int, len(kustomizations))

	dependents := make(map[string][]string, len(kustomizations))
	for _, kust := range kustomizations {
		byName[kust.Name] = kust
		inDegree[kust.Name] = 0
	}

	for _, kust := range kustomizations {
		seen := make(map[string]struct{}, len(kust.DependsOn))
		for _, dep := range kust.DependsOn {
			if _, dup := seen[dep]; dup {
				continue
			}

			seen[dep] = struct{}{}
			if _, exists := byName[dep]; exists {
				inDegree[kust.Name]++
				dependents[dep] = append(dependents[dep], kust.Name)
			}
		}
	}

	queue := make([]string, 0, len(kustomizations))
	for _, kust := range kustomizations {
		if inDegree[kust.Name] == 0 {
			queue = append(queue, kust.Name)
		}
	}

	sorted := make([]flux.KustomizationInfo, 0, len(kustomizations))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		sorted = append(sorted, byName[name])
		for _, child := range dependents[name] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(sorted) < len(kustomizations) {
		for _, kust := range kustomizations {
			if inDegree[kust.Name] > 0 {
				sorted = append(sorted, kust)
			}
		}
	}

	return sorted
}

// reconcileArgoCD triggers and waits for ArgoCD application sync using the client reconciler.
// It uses ProgressGroup to show per-application reconciliation status in real-time.
func reconcileArgoCD(
	cmd *cobra.Command,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	argoReconciler, err := argocd.NewReconciler(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create argocd reconciler: %w", err)
	}

	writer := cmd.OutOrStdout()

	deadlineCtx, deadlineCancel := context.WithTimeout(cmd.Context(), timeout)
	defer deadlineCancel()

	writeActivityNotification("triggering argocd refresh...", outputTimer, writer)

	err = argoReconciler.TriggerRefresh(deadlineCtx, true)
	if err != nil {
		return fmt.Errorf("trigger argocd refresh: %w", err)
	}

	writeActivityNotification("reconciling argocd applications...", outputTimer, writer)

	apps, err := argoReconciler.ListApplications(deadlineCtx)
	if err != nil {
		return fmt.Errorf("list argocd applications: %w", err)
	}

	if len(apps) == 0 {
		return nil
	}

	tasks := make([]notify.ProgressTask, 0, len(apps))
	for _, app := range apps {
		name := app.Name
		tasks = append(tasks, notify.ProgressTask{
			Name: name,
			Fn: func(ctx context.Context) error {
				return pollUntilApplicationReady(ctx, argoReconciler, name)
			},
		})
	}

	appGroup := notify.NewProgressGroup("", "", writer,
		notify.WithLabels(notify.ReconcilingLabels()),
		notify.WithTimer(outputTimer),
		notify.WithContinueOnError(),
		notify.WithAppendOnly(),
		notify.WithCountLabel("applications"),
		notify.WithConcurrency(reconcileConcurrency),
	)

	err = appGroup.Run(deadlineCtx, tasks...)
	if err != nil {
		return fmt.Errorf("reconcile argocd applications: %w", err)
	}

	return nil
}

// pollUntilApplicationReady polls a named ArgoCD Application until it is
// synced and healthy, or the context's deadline expires. On permanent failure,
// it returns an actionable error including the resource name and failure details.
// The caller is expected to provide a context with a deadline (shared across all
// application tasks) so that the total reconcile time is bounded.
func pollUntilApplicationReady(
	ctx context.Context,
	argoReconciler *argocd.Reconciler,
	name string,
) error {
	ticker := time.NewTicker(argoCDApplicationPollInterval)
	defer ticker.Stop()

	for {
		ready, err := argoReconciler.CheckNamedApplicationReady(ctx, name)
		if err != nil {
			if reconcilerclient.IsContextError(err) {
				if errors.Is(ctx.Err(), context.Canceled) {
					return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
				}

				return fmt.Errorf(
					"%w — "+
						"run 'ksail workload get applications.argoproj.io %s -n argocd' to inspect",
					argocd.ErrReconcileTimeout, name,
				)
			}

			return fmt.Errorf("permanent failure: %w", err)
		}

		if ready {
			return nil
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
			}

			return fmt.Errorf(
				"%w — "+
					"run 'ksail workload get applications.argoproj.io %s -n argocd' to inspect",
				argocd.ErrReconcileTimeout, name,
			)
		case <-ticker.C:
		}
	}
}

// NewRolloutCmd creates the workload rollout command.
func NewRolloutCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateRolloutCommand(kubeconfigPath)
	})

	// Add permission annotation
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}

	cmd.Annotations[annotations.AnnotationPermission] = "write"

	return cmd
}

// NewScaleCmd creates the workload scale command.
func NewScaleCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateScaleCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

const (
	kustomizationFileName = "kustomization.yaml"
	validationConcurrency = 5
)

// kustomizationFileNames lists all kustomization filenames recognized by kubectl.
// Used by hasKustomizationFile, findKustomizationDir, findKustomizations,
// findYAMLFiles, and collectPatchPathsFromDir for consistent detection.
//
//nolint:gochecknoglobals // package-level constant slice; Go does not support const slices
var kustomizationFileNames = []string{kustomizationFileName, "kustomization.yml", "Kustomization"}

// ErrBuildFailed is returned when a kustomize build or manifest validation fails.
var ErrBuildFailed = errors.New("build failed")

// NewValidateCmd creates the workload validate command.
func NewValidateCmd() *cobra.Command {
	var (
		skipSecrets          bool
		strict               bool
		ignoreMissingSchemas bool
	)

	cmd := &cobra.Command{
		Use:   "validate [PATH]",
		Short: "Validate Kubernetes manifests and kustomizations",
		Long: `Validate Kubernetes manifest files and kustomizations using kubeconform.

This command validates individual YAML files and kustomizations in the specified path.
If no path is provided, the path is resolved in order:
  1. spec.workload.sourceDirectory from ksail.yaml (if a config file is found and the field is set)
  2. The default source directory when spec.workload.sourceDirectory is unset ("k8s" directory)
  3. The current directory (fallback when no ksail.yaml config file is found)

The validation process:
1. Validates individual YAML files (patch files referenced in a kustomization file via patches,
   patchesStrategicMerge, or patchesJson6902 are excluded — they are not valid standalone
   Kubernetes resources and are validated as part of the kustomize build output instead)
2. Validates kustomizations by building them with kustomize and validating the output

	Flux variable substitutions are resolved before validation using type-aware placeholders:
  - ${VAR} (bare, no default): when a JSON schema type is available, substitutes a typed
    placeholder derived from the schema for the field ("placeholder" for strings, 0 for
    integers, true for booleans); when no schema type is available, it falls back to the
    string value "placeholder"
  - ${VAR:-default} / ${VAR:=default}: when a schema type is available, uses the default
    value parsed according to the field schema type (e.g., "3" → int 3 for integer fields);
    when no schema type is available, the default is parsed using YAML-native type inference
  - Mixed text (e.g., "prefix.${VAR}"): substitutes "placeholder" in string context

Schema lookups use a local disk cache and require no network access. When no cached
JSON schema is available, placeholders fall back to strings with YAML-native parsing.

By default, Kubernetes Secrets are skipped to avoid validation failures due to SOPS fields.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidateCmd(
				cmd.Context(),
				cmd,
				args,
				skipSecrets,
				strict,
				ignoreMissingSchemas,
			)
		},
	}

	// Add flags
	cmd.Flags().BoolVar(&skipSecrets, "skip-secrets", true, "Skip validation of Kubernetes Secrets")
	cmd.Flags().BoolVar(&strict, "strict", false, "Enable strict validation mode")
	cmd.Flags().BoolVar(
		&ignoreMissingSchemas,
		"ignore-missing-schemas",
		true,
		"Ignore resources with missing schemas",
	)

	return cmd
}

func runValidateCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	skipSecrets bool,
	strict bool,
	ignoreMissingSchemas bool,
) error {
	path, err := resolveValidatePath(cmd, args)
	if err != nil {
		return err
	}

	// Canonicalize user-supplied path (resolve symlinks + absolute) so that
	// validation targets the real directory and symlink-escape attacks are
	// prevented in CI pipelines processing external manifests.
	canonPath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}

	path = canonPath

	// Create kubeconform client
	kubeconformClient := kubeconform.NewClient()

	// Build validation options
	validationOpts := &kubeconform.ValidationOptions{
		Strict:               strict,
		IgnoreMissingSchemas: ignoreMissingSchemas,
	}

	if skipSecrets {
		validationOpts.SkipKinds = append(validationOpts.SkipKinds, "Secret")
	}

	return validatePath(ctx, cmd, path, kubeconformClient, validationOpts)
}

// resolveValidatePath determines which path to validate.
// If an explicit argument is given, it is returned directly.
// Otherwise, it loads ksail.yaml (honoring --config) and returns the
// configured sourceDirectory. Falls back to "." when no config file is found.
func resolveValidatePath(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	// Resolve --config flag without registering additional flags on cmd.
	// This avoids "flag redefined" panics that NewCommandConfigManager would cause.
	var configFile string

	cfgPath, err := flags.GetConfigPath(cmd)
	if err == nil {
		configFile = cfgPath
	}

	cfgManager := configmanager.NewConfigManager(io.Discard, configFile)

	cfg, loadErr := cfgManager.Load(
		configmanagerinterface.LoadOptions{Silent: true, SkipValidation: true},
	)

	switch {
	case loadErr != nil && cfgManager.IsConfigFileFound():
		return "", fmt.Errorf("load config: %w", loadErr)
	case loadErr == nil && cfgManager.IsConfigFileFound():
		return resolveSourceDir(cfg, ""), nil
	default:
		return ".", nil
	}
}

// validatePath validates all manifests in the given path.
func validatePath(
	ctx context.Context,
	cmd *cobra.Command,
	path string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("access path %s: %w", path, err)
	}

	// If it's a file, validate it directly
	if !info.IsDir() {
		rootDir := filepath.Dir(path)

		return validateFile(ctx, cmd, rootDir, path, kubeconformClient, opts)
	}

	// If it's a directory, walk it to find YAML files and kustomizations
	return validateDirectory(ctx, cmd, path, kubeconformClient, opts)
}

// validateFile validates a single YAML file.
func validateFile(
	ctx context.Context,
	cmd *cobra.Command,
	rootDir string,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Only validate YAML files
	if !isYAMLFile(filePath) {
		return nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "validating %s",
		Args:    []any{filePath},
		Writer:  cmd.OutOrStdout(),
	})

	err := validateFileSilent(ctx, rootDir, filePath, kubeconformClient, opts)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// validateDirectory validates all YAML files and kustomizations in a directory.
// Validation is performed in parallel with live progress display for better UX.
func validateDirectory(
	ctx context.Context,
	cmd *cobra.Command,
	dirPath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Find all kustomizations
	kustomizations, err := findKustomizations(dirPath)
	if err != nil {
		return fmt.Errorf("find kustomizations: %w", err)
	}

	// Find all YAML files
	yamlFiles, err := findYAMLFiles(dirPath)
	if err != nil {
		return fmt.Errorf("find YAML files: %w", err)
	}

	// Exclude patch files — already validated as part of kustomize build output.
	patchPaths := collectPatchPaths(dirPath, kustomizations)
	yamlFiles = filterPatchFiles(yamlFiles, patchPaths)

	progressOpts := []notify.ProgressOption{
		notify.WithAppendOnly(),
		notify.WithConcurrency(validationConcurrency),
		notify.WithContinueOnError(),
	}

	if len(kustomizations) > 0 {
		kustomizeClient := kustomize.NewClient()

		err := runParallelValidation(
			ctx, cmd, kustomizations, dirPath, "Validating kustomizations", "✅",
			buildKustomizationValidator(
				dirPath,
				kubeconformClient,
				kustomizeClient,
				opts,
			),
			append(progressOpts, notify.WithCountLabel("kustomizations"))...,
		)
		if err != nil {
			return fmt.Errorf("kustomization validation failed: %w", err)
		}
	}

	// Validate individual YAML files in parallel with progress display
	if len(yamlFiles) > 0 {
		err := runParallelValidation(
			ctx, cmd, yamlFiles, dirPath, "Validating YAML files", "📄",
			func(taskCtx context.Context, file string) error {
				return validateFileSilent(
					taskCtx, dirPath, file, kubeconformClient, opts,
				)
			},
			append(progressOpts, notify.WithCountLabel("files"))...,
		)
		if err != nil {
			return fmt.Errorf("yaml validation failed: %w", err)
		}
	}

	return nil
}

// runParallelValidation runs a set of validation tasks in parallel with progress display.
func runParallelValidation(
	ctx context.Context,
	cmd *cobra.Command,
	items []string,
	basePath string,
	title string,
	emoji string,
	validateFn func(ctx context.Context, item string) error,
	extraOpts ...notify.ProgressOption,
) error {
	slices.Sort(items)

	tasks := make([]notify.ProgressTask, len(items))
	for taskIdx, item := range items {
		name := filepath.Base(item)

		rel, relErr := filepath.Rel(basePath, item)
		if relErr == nil && rel != "." {
			name = rel
		}

		tasks[taskIdx] = notify.ProgressTask{
			Name: name,
			Fn: func(taskCtx context.Context) error {
				return validateFn(taskCtx, item)
			},
		}
	}

	opts := append(
		[]notify.ProgressOption{notify.WithLabels(notify.ValidatingLabels())},
		extraOpts...)

	err := notify.NewProgressGroup(title, emoji, cmd.OutOrStdout(), opts...).Run(ctx, tasks...)
	if err != nil {
		return fmt.Errorf("run validation group: %w", err)
	}

	return nil
}

// validateKustomizationSilent validates a kustomization without output (for parallel execution).
// Build errors are returned unwrapped so that simplifyBuildError in the caller can strip the
// kustomize client's verbose "kustomize build <path>:" prefix correctly.
func validateKustomizationSilent(
	ctx context.Context,
	kustDir string,
	kubeconformClient *kubeconform.Client,
	kustomizeClient *kustomize.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Build the kustomization — return the raw error so simplifyBuildError can strip its prefix.
	output, err := kustomizeClient.Build(ctx, kustDir)
	if err != nil {
		return err //nolint:wrapcheck // intentionally unwrapped: simplifyBuildError in the caller strips the kustomize prefix
	}

	// Validate the output
	err = kubeconformClient.ValidateBytes(
		ctx,
		kustDir,
		expandFluxSubstitutions(output.Bytes()),
		opts,
	)
	if err != nil {
		return fmt.Errorf("validate manifests: %w", err)
	}

	return nil
}

// buildKustomizationValidator returns a task function that validates a kustomization directory.
// Errors are simplified for readability by stripping verbose kustomize output.
func buildKustomizationValidator(
	dirPath string,
	kubeconformClient *kubeconform.Client,
	kustomizeClient *kustomize.Client,
	opts *kubeconform.ValidationOptions,
) func(context.Context, string) error {
	return func(taskCtx context.Context, kustDir string) error {
		err := validateKustomizationSilent(
			taskCtx,
			kustDir,
			kubeconformClient,
			kustomizeClient,
			opts,
		)
		if err != nil {
			return simplifyBuildError(err, dirPath)
		}

		return nil
	}
}

// validateFileSilent validates a single YAML file without output (for parallel execution).
func validateFileSilent(
	ctx context.Context,
	rootDir string,
	filePath string,
	kubeconformClient *kubeconform.Client,
	opts *kubeconform.ValidationOptions,
) error {
	// Only validate YAML files
	if !isYAMLFile(filePath) {
		return nil
	}

	data, err := fsutil.ReadFileSafe(rootDir, filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	err = kubeconformClient.ValidateBytes(
		ctx,
		filePath,
		expandFluxSubstitutions(data),
		opts,
	)
	if err != nil {
		return fmt.Errorf("validate file %s: %w", filePath, err)
	}

	return nil
}

// simplifyBuildError extracts an actionable error message from a kustomize build error.
// It strips the internal "kustomize build <path>:" wrapper, replaces absolute paths
// with paths relative to basePath, and for deeply nested accumulation chains extracts
// the root cause (e.g. "invalid Kustomization: ...").
func simplifyBuildError(err error, basePath string) error {
	msg := err.Error()

	// Remove "kustomize build <path>: " prefix added by the kustomize client.
	if strings.HasPrefix(msg, "kustomize build ") {
		if i := strings.Index(msg, ": "); i > 0 {
			msg = msg[i+2:]
		}
	}

	// For deeply nested kustomize accumulation errors, extract the root cause.
	if strings.Contains(msg, "accumulating resources") {
		for _, pattern := range []string{
			"invalid Kustomization: ",
			"missing metadata",
		} {
			if idx := strings.LastIndex(msg, pattern); idx >= 0 {
				msg = msg[idx:]

				break
			}
		}
	}

	// Strip absolute paths: replace basePath prefix with relative notation.
	if basePath != "" {
		msg = strings.ReplaceAll(msg, basePath+string(filepath.Separator), "")
		msg = strings.ReplaceAll(msg, basePath, ".")
	}

	return fmt.Errorf("%w: %s", ErrBuildFailed, msg)
}

// walkFiles collects file paths under rootPath that satisfy match.
// match receives the full path and os.FileInfo for each non-directory entry
// and returns the value to collect (empty string means skip).
func walkFiles(rootPath string, match func(string, os.FileInfo) string) ([]string, error) {
	var results []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			if v := match(path, info); v != "" {
				results = append(results, v)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", rootPath, err)
	}

	return results, nil
}

// findKustomizations finds all directories containing a kustomization file
// recognized by kubectl (kustomization.yaml, kustomization.yml, or Kustomization).
func findKustomizations(rootPath string) ([]string, error) {
	return walkFiles(rootPath, func(path string, info os.FileInfo) string {
		if slices.Contains(kustomizationFileNames, info.Name()) {
			return filepath.Dir(path)
		}

		return ""
	})
}

// findYAMLFiles finds all YAML files in a directory, excluding kustomization files
// recognized by kubectl (kustomization.yaml, kustomization.yml, or Kustomization).
func findYAMLFiles(rootPath string) ([]string, error) {
	return walkFiles(rootPath, func(path string, _ os.FileInfo) string {
		base := filepath.Base(path)

		if slices.Contains(kustomizationFileNames, base) {
			return ""
		}

		if isYAMLFile(path) {
			return path
		}

		return ""
	})
}

// isYAMLFile checks if a file has a YAML extension.
func isYAMLFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	return ext == ".yaml" || ext == ".yml"
}

// filterPatchFiles removes from yamlFiles any path present in patchPaths.
// Patch files are not valid standalone resources; they are validated as part of
// the kustomize build output.
func filterPatchFiles(yamlFiles []string, patchPaths map[string]struct{}) []string {
	if len(patchPaths) == 0 {
		return yamlFiles
	}

	filtered := yamlFiles[:0]
	for _, f := range yamlFiles {
		if _, ok := patchPaths[f]; !ok {
			filtered = append(filtered, f)
		}
	}

	return filtered
}

// collectPatchPaths parses each kustomization.yaml and returns the absolute paths
// of files referenced as patches. These files are not valid standalone K8s resources
// and should be excluded from individual file validation (they are already validated
// as part of the kustomize build output).
func collectPatchPaths(rootDir string, kustomizationDirs []string) map[string]struct{} {
	patchPaths := make(map[string]struct{})

	for _, kustDir := range kustomizationDirs {
		collectPatchPathsFromDir(rootDir, kustDir, patchPaths)
	}

	return patchPaths
}

// collectPatchPathsFromDir parses a kustomization file (trying all recognized names)
// and adds the absolute paths of referenced patch files to the provided set.
func collectPatchPathsFromDir(rootDir, kustDir string, patchPaths map[string]struct{}) {
	var data []byte

	var err error

	for _, name := range kustomizationFileNames {
		kustFile := filepath.Join(kustDir, name)

		data, err = fsutil.ReadFileSafe(rootDir, kustFile)
		if err == nil {
			break
		}
	}

	if err != nil {
		return
	}

	var kust kustomizeTypes.Kustomization

	err = kust.Unmarshal(data)
	if err != nil {
		return
	}

	// Modern patches field
	for _, p := range kust.Patches {
		addPatchPath(kustDir, p.Path, patchPaths)
	}

	// Deprecated patchesStrategicMerge (file paths only, skip inline YAML)
	//nolint:staticcheck // SA1019: PatchesStrategicMerge is deprecated; kept to support legacy kustomization files
	for _, psm := range kust.PatchesStrategicMerge {
		s := string(psm)
		if !strings.Contains(s, "\n") {
			addPatchPath(kustDir, s, patchPaths)
		}
	}

	// Deprecated patchesJson6902
	//nolint:staticcheck // SA1019: PatchesJson6902 is deprecated; kept to support legacy kustomization files
	for _, p := range kust.PatchesJson6902 {
		addPatchPath(kustDir, p.Path, patchPaths)
	}
}

// addPatchPath resolves a relative patch file path against a kustomization directory
// and adds the absolute path to the set. Empty paths are ignored.
func addPatchPath(kustDir, relPath string, patchPaths map[string]struct{}) {
	if relPath == "" {
		return
	}

	abs := filepath.Join(kustDir, relPath)

	resolved, err := filepath.Abs(abs)
	if err != nil {
		return
	}

	patchPaths[resolved] = struct{}{}
}

// fluxVarPattern matches Flux postBuild variable references:
// ${VAR}, ${VAR:-default}, and ${VAR:=default}.
// Groups: 1 = variable name, 2 = operator (:- or :=), 3 = default value.
var fluxVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?:(:-|:=)([^}]*))?\}`)

const (
	schemaCacheFileMaxChars = 200

	placeholderString = "placeholder"
)

// schemaRegistry provides thread-safe caching of parsed JSON schemas keyed by "apiVersion/kind".
type schemaRegistry struct {
	cache sync.Map
}

var schemas = &schemaRegistry{} //nolint:gochecknoglobals // singleton schema cache for validation lifecycle

// expandFluxSubstitutions expands Flux postBuild variable references in YAML
// data using type-aware placeholders derived from JSON schemas.
//
// For each YAML document:
//   - ${VAR:-default} / ${VAR:=default} → uses the default value
//   - ${VAR} as entire scalar value → looks up the expected JSON schema type
//     and substitutes a typed placeholder ("placeholder", 0, or true)
//   - ${VAR} mixed with other text → substitutes "placeholder" (string context)
//
// Falls back to regex-based string placeholder expansion when YAML parsing fails.
func expandFluxSubstitutions(data []byte) []byte {
	if !fluxVarPattern.Match(data) {
		return data
	}

	docs := splitYAMLDocuments(data)
	if len(docs) == 0 {
		return data
	}

	expanded := make([][]byte, 0, len(docs))
	for _, doc := range docs {
		expanded = append(expanded, expandDocument(doc))
	}

	return bytes.Join(expanded, []byte("\n---\n"))
}

// splitYAMLDocuments splits multi-document YAML using a YAML-aware reader
// that correctly handles document separators ("---") regardless of position,
// trailing whitespace, or carriage returns.
func splitYAMLDocuments(data []byte) [][]byte {
	reader := yamlio.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	var docs [][]byte

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return [][]byte{data}
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return [][]byte{data}
	}

	return docs
}

// expandDocument expands variable references in a single YAML document.
func expandDocument(doc []byte) []byte {
	if !fluxVarPattern.Match(doc) {
		return doc
	}

	var obj any

	err := yaml.Unmarshal(doc, &obj)
	if err != nil {
		return expandFallback(doc)
	}

	switch typedObj := obj.(type) {
	case map[string]any:
		return expandMapDocument(typedObj, doc)
	case []any:
		return expandListDocument(typedObj, doc)
	default:
		return expandFallback(doc)
	}
}

// expandMapDocument expands variable references in a YAML document with a map root.
func expandMapDocument(obj map[string]any, doc []byte) []byte {
	apiVersion, _ := obj["apiVersion"].(string)
	kind, _ := obj["kind"].(string)
	schema := schemas.load(apiVersion, kind)

	walkAndExpand(obj, "", schema)

	out, err := yaml.Marshal(obj)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandListDocument expands variable references in a YAML document with a list root
// (e.g., JSON6902 patch list). There is no single apiVersion/kind,
// so map elements are walked with a nil schema.
func expandListDocument(list []any, doc []byte) []byte {
	for idx, elem := range list {
		if mapElem, isMap := elem.(map[string]any); isMap {
			walkAndExpand(mapElem, "", nil)
			list[idx] = mapElem
		}
	}

	out, err := yaml.Marshal(list)
	if err != nil {
		return expandFallback(doc)
	}

	return out
}

// expandFallback performs simple regex-based expansion when YAML parsing fails.
// Variable references are expanded using defaults with fallback to placeholder values.
func expandFallback(data []byte) []byte {
	return fluxVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := fluxVarPattern.FindSubmatch(match)
		if len(groups) < 4 { //nolint:mnd // regex groups: full, name, op, default
			return match
		}

		return []byte(resolveInlineVar(string(groups[1]), string(groups[2]), string(groups[3])))
	})
}

// walkAndExpand recursively walks the parsed YAML structure and expands variable references.
func walkAndExpand(obj any, path string, schema map[string]any) any {
	switch val := obj.(type) {
	case map[string]any:
		for key, child := range val {
			val[key] = walkAndExpand(child, path+"/"+key, schema)
		}

		return val
	case []any:
		for idx, item := range val {
			val[idx] = walkAndExpand(item, fmt.Sprintf("%s/%d", path, idx), schema)
		}

		return val
	case string:
		return expandStringValue(val, path, schema)
	default:
		return obj
	}
}

// expandStringValue expands Flux variable references in a string value.
func expandStringValue(val, path string, schema map[string]any) any {
	if !fluxVarPattern.MatchString(val) {
		return val
	}

	// Check if the entire value is a single substitution (bare or with default)
	match := fluxVarPattern.FindStringSubmatch(val)
	if match != nil && match[0] == val {
		return expandSingleVar(match, path, schema)
	}

	// Mixed text — expand inline (always string context)
	return expandMixedText(val)
}

// expandSingleVar expands a value that consists entirely of a single variable reference.
func expandSingleVar(match []string, path string, schema map[string]any) any {
	varName := match[1]
	operator := match[2]
	defaultVal := match[3]
	schemaType := getSchemaTypeAtPath(schema, path)

	if operator == "" {
		return expandBareVar(varName, schemaType)
	}

	return expandVarWithDefault(varName, defaultVal, operator, schemaType)
}

// expandBareVar expands a bare ${VAR} reference using typed placeholders.
func expandBareVar(_, schemaType string) any {
	return typedPlaceholderValue(schemaType)
}

// expandVarWithDefault expands ${VAR:=default} or ${VAR:-default} references.
func expandVarWithDefault(_, defaultVal, _, schemaType string) any {
	return parseTypedDefault(defaultVal, schemaType)
}

// expandMixedText expands variable references embedded within other text (always string context).
func expandMixedText(val string) string {
	return fluxVarPattern.ReplaceAllStringFunc(val, func(match string) string {
		groups := fluxVarPattern.FindStringSubmatch(match)
		if len(groups) < 4 { //nolint:mnd // regex groups: full, name, op, default
			return match
		}

		return resolveInlineVar(groups[1], groups[2], groups[3])
	})
}

// resolveInlineVar resolves a single variable reference in a mixed-text context to a string.
func resolveInlineVar(_, operator, defaultVal string) string {
	switch operator {
	case "":
		return placeholderString
	case ":=", ":-":
		return defaultVal
	default:
		return placeholderString
	}
}

// typedPlaceholderValue returns a Go value matching the schema type.
// When marshaled by sigs.k8s.io/yaml, these produce correctly typed YAML scalars.
func typedPlaceholderValue(schemaType string) any {
	switch schemaType {
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return true
	default:
		return placeholderString
	}
}

// parseTypedDefault parses a default value string into the appropriate Go type
// based on the schema type, so that sigs.k8s.io/yaml marshals it without quotes.
// When the schema type is unknown (empty string), YAML-native type inference is
// used, matching Flux's behavior where substitution occurs at the text level.
func parseTypedDefault(defaultVal, schemaType string) any {
	trimmed := strings.TrimSpace(defaultVal)

	switch schemaType {
	case "integer":
		return parseInteger(trimmed, defaultVal)
	case "number":
		return parseNumber(trimmed, defaultVal)
	case "boolean":
		return parseBoolean(trimmed, defaultVal)
	case typeString:
		return defaultVal
	default:
		return inferYAMLType(trimmed, defaultVal)
	}
}

func parseInteger(trimmed, defaultVal string) any {
	var intVal int64

	_, err := fmt.Sscanf(trimmed, "%d", &intVal)
	if err == nil {
		return intVal
	}

	return defaultVal
}

func parseNumber(trimmed, defaultVal string) any {
	var floatVal float64

	_, err := fmt.Sscanf(trimmed, "%f", &floatVal)
	if err == nil {
		return floatVal
	}

	return defaultVal
}

func parseBoolean(trimmed, defaultVal string) any {
	if trimmed == "true" {
		return true
	}

	if trimmed == "false" {
		return false
	}

	return defaultVal
}

// inferYAMLType uses YAML-native type inference so that values like "2" become
// integers and "true" becomes a boolean, matching how YAML would parse the
// substituted text.
func inferYAMLType(trimmed, defaultVal string) any {
	var typedVal any

	err := yaml.Unmarshal([]byte(trimmed), &typedVal)
	if err == nil && typedVal != nil {
		return typedVal
	}

	return defaultVal
}

// load returns the JSON schema for a Kubernetes resource from disk cache, or nil if unavailable.
// Network fetching is intentionally omitted to keep validation fast, deterministic, and
// offline-friendly. Schemas are available if kubeconform has previously cached them on disk.
func (reg *schemaRegistry) load(apiVersion, kind string) map[string]any {
	if apiVersion == "" || kind == "" {
		return nil
	}

	cacheKey := apiVersion + "/" + kind

	if cached, ok := reg.cache.Load(cacheKey); ok {
		schema, _ := cached.(map[string]any)

		return schema
	}

	schema := fetchSchemaFromDisk(apiVersion, kind)

	reg.cache.Store(cacheKey, schema)

	return schema
}

// fetchSchemaFromDisk tries to load a schema from the disk cache.
func fetchSchemaFromDisk(apiVersion, kind string) map[string]any {
	cacheDir := schemaCacheDir()

	for _, schemaURL := range schemaURLs(apiVersion, kind) {
		cachedPath := filepath.Join(cacheDir, schemaCacheFileName(schemaURL))

		data, err := os.ReadFile(cachedPath) //nolint:gosec // controlled cache directory
		if err != nil {
			continue
		}

		schema := parseJSONSchema(data)
		if schema != nil {
			return schema
		}
	}

	return nil
}

// schemaCacheDir returns the schema cache directory.
func schemaCacheDir() string {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ksail", "kubeconform")
	}

	return filepath.Join(userCacheDir, "ksail", "kubeconform")
}

// schemaCacheFileName produces a deterministic filename for caching a schema URL.
func schemaCacheFileName(schemaURL string) string {
	safe := strings.NewReplacer(
		"://", "_",
		"/", "_",
		".", "_",
	).Replace(schemaURL) + ".json"

	if len(safe) > schemaCacheFileMaxChars {
		safe = safe[len(safe)-schemaCacheFileMaxChars:]
	}

	return safe
}

// schemaURLs returns the candidate schema URLs for a given apiVersion/kind.
func schemaURLs(apiVersion, kind string) []string {
	kindLower := strings.ToLower(kind)
	group, version := splitAPIVersion(apiVersion)

	if group != "" {
		// Try Kubernetes JSON schema first (for core API groups like apps, batch, etc.),
		// then fall back to CRDs catalog for custom resources.
		return []string{
			fmt.Sprintf(
				"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/master-standalone/%s-%s-%s.json",
				kindLower,
				group,
				version,
			),
			fmt.Sprintf(
				"https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/%s/%s_%s.json",
				group, kindLower, version,
			),
		}
	}

	return []string{
		fmt.Sprintf(
			"https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/master-standalone/%s-%s.json",
			kindLower,
			version,
		),
	}
}

// splitAPIVersion splits "apps/v1" into ("apps", "v1") or "v1" into ("", "v1").
func splitAPIVersion(apiVersion string) (string, string) {
	parts := strings.SplitN(apiVersion, "/", 2) //nolint:mnd // splitting group/version
	if len(parts) == 2 {                        //nolint:mnd // group/version pair
		return parts[0], parts[1]
	}

	return "", parts[0]
}

// parseJSONSchema parses raw JSON bytes into a schema map.
func parseJSONSchema(data []byte) map[string]any {
	var schema map[string]any

	err := json.Unmarshal(data, &schema)
	if err != nil {
		return nil
	}

	return schema
}

// getSchemaTypeAtPath walks a JSON schema following a path like "/spec/replicas"
// and returns the type of the field ("string", "integer", "number", "boolean").
// Returns empty string when the schema is nil, path is empty, or type cannot be resolved.
func getSchemaTypeAtPath(schema map[string]any, path string) string {
	if schema == nil || path == "" {
		return ""
	}

	trimmed := strings.TrimPrefix(path, "/")
	segments := strings.Split(trimmed, "/")
	current := schema

	for _, seg := range segments {
		current = resolveSchemaNode(current, seg)
		if current == nil {
			return ""
		}
	}

	return schemaNodeType(current)
}

const typeString = "string"

// resolveSchemaNode navigates one level deeper into a JSON schema for a given key.
func resolveSchemaNode(schema map[string]any, key string) map[string]any {
	if result := resolveFromProperties(schema, key); result != nil {
		return result
	}

	if result := resolveFromItems(schema, key); result != nil {
		return result
	}

	return resolveFromCombiners(schema, key)
}

func resolveFromProperties(schema map[string]any, key string) map[string]any {
	props, found := schema["properties"].(map[string]any)
	if !found {
		return nil
	}

	child, childFound := props[key].(map[string]any)
	if !childFound {
		return nil
	}

	return child
}

func resolveFromItems(schema map[string]any, key string) map[string]any {
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return nil
	}

	if isNumericIndex(key) {
		return items
	}

	return nil
}

func resolveFromCombiners(schema map[string]any, key string) map[string]any {
	for _, combinerKey := range []string{"allOf", "anyOf", "oneOf"} {
		arr, ok := schema[combinerKey].([]any)
		if !ok {
			continue
		}

		for _, entry := range arr {
			sub, ok := entry.(map[string]any)
			if !ok {
				continue
			}

			if result := resolveSchemaNode(sub, key); result != nil {
				return result
			}
		}
	}

	return nil
}

// schemaNodeType extracts the type from a JSON schema node.
func schemaNodeType(schema map[string]any) string {
	if typeVal, ok := schema["type"].(string); ok {
		return typeVal
	}

	if arr, ok := schema["type"].([]any); ok {
		for _, item := range arr {
			if typeVal, ok := item.(string); ok && typeVal != "null" {
				return typeVal
			}
		}
	}

	return ""
}

// isNumericIndex checks if a string represents a numeric array index.
func isNumericIndex(str string) bool {
	if len(str) == 0 {
		return false
	}

	for _, char := range str {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

// NewWaitCmd creates the workload wait command.
func NewWaitCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateWaitCommand(kubeconfigPath)
	})
}

var errNotDirectory = errors.New("watch path is not a directory")

// watchCmdLong is the long description for the watch subcommand.
const watchCmdLong = `Watch a directory for file changes and automatically apply workloads.

When files in the watched directory are created, modified, or deleted,
the command debounces changes (~500ms) then scopes the apply to the
nearest directory containing a kustomization file recognized by kubectl
(kustomization.yaml, kustomization.yml, or Kustomization), walking up
from the changed file to the watch root. If no kustomization boundary is
found, or the boundary is the watch root, it applies the full root
directory. This scoping ensures only the affected Kustomize layer is
re-applied, making iteration faster in monorepo-style layouts.

Each reconcile prints a timestamped status line showing the changed file,
the outcome (success or failure), and the elapsed time for the apply.
Press Ctrl+C to stop the watcher.

Use --initial-apply to synchronize the cluster with the current state of
the watch directory before entering the watch loop. This is useful after
editing manifests offline or when starting a fresh session.

Examples:
  # Watch the default k8s/ directory
  ksail workload watch

  # Watch and apply once on startup before entering the loop
  ksail workload watch --initial-apply

  # Watch a custom directory
  ksail workload watch --path=./manifests`

// NewWatchCmd creates the workload watch command.
func NewWatchCmd() *cobra.Command {
	var (
		pathFlag     string
		initialApply bool
		debugFlag    bool
	)

	cmd := &cobra.Command{
		Use:          "watch",
		Short:        "Watch for file changes and auto-apply workloads",
		Long:         watchCmdLong,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cmd.Flags().StringVar(
		&pathFlag, "path", "",
		"Directory to watch for changes (default: k8s/ or spec.workload.sourceDirectory from ksail.yaml)",
	)

	cmd.Flags().BoolVar(
		&initialApply, "initial-apply", false,
		"Apply all workloads once immediately on startup before entering the watch loop",
	)

	cmd.Flags().BoolVar(
		&debugFlag, "debug", false,
		"Show diagnostic output for file events and polling (useful for troubleshooting watch behavior)",
	)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runWatch(cmd, pathFlag, initialApply, debugFlag)
	}

	return cmd
}

// runWatch starts the file watcher loop.
func runWatch(cmd *cobra.Command, pathFlag string, initialApply bool, debug bool) error {
	// The root-level error executor captures stderr into a buffer for error
	// aggregation.  For long-running commands like watch the buffer is never
	// flushed, making all feedback invisible.  Override with real stderr so
	// that watcher diagnostics (change detected, apply results) appear in the
	// terminal and in CI logs.
	cmd.SetErr(os.Stderr)

	cmdCtx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	watchDir := resolveSourceDir(cmdCtx.ClusterCfg, pathFlag)

	// Verify the directory exists.
	info, err := os.Stat(watchDir)
	if err != nil {
		return fmt.Errorf("access watch directory %q: %w", watchDir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q: %w", watchDir, errNotDirectory)
	}

	// Canonicalize the watch directory (resolve symlinks + absolute) so that
	// file events are matched against the real directory and symlink-escape
	// attacks are prevented in CI pipelines processing external manifests.
	absDir, err := fsutil.EvalCanonicalPath(watchDir)
	if err != nil {
		return fmt.Errorf("resolve watch directory %q: %w", watchDir, err)
	}

	// Try to create a Flux reconciler for selective Kustomization reconciliation.
	// If Flux is not available (CRDs not installed, kubeconfig error, etc.),
	// the reconciler is nil and selective reconciliation is silently skipped.
	fluxReconciler := tryCreateFluxReconciler()

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "👁️",
		Content: "Watching for changes...",
		Writer:  cmd.OutOrStdout(),
	})

	cmd.PrintErrf("  watching: %s\n", absDir)
	cmd.PrintErrf("  press Ctrl+C to stop\n\n")

	return watchLoop(cmd.Context(), cmd, absDir, initialApply, fluxReconciler, debug)
}

// watchLoop sets up the fsnotify watcher and runs the debounced apply loop.
// When initialApply is true, a full apply of the watch root is performed
// after the event loop goroutine is started, so watcher events are consumed
// immediately and not dropped or buffered during the initial apply. Ctrl+C
// cancels both the initial apply and the event loop via the shared sigCtx.
func watchLoop(
	ctx context.Context,
	cmd *cobra.Command,
	dir string,
	initialApply bool,
	fluxReconciler *flux.Reconciler,
	debug bool,
) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create file watcher: %w", err)
	}

	defer func() { _ = watcher.Close() }()

	// Add all directories recursively.
	err = addRecursive(watcher, dir)
	if err != nil {
		return err
	}

	// Set up signal handling for graceful shutdown.
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the event loop before any apply so that watcher events are
	// consumed immediately, avoiding backlogs or drops during the initial apply.
	errCh := make(chan error, 1)

	go func() {
		errCh <- eventLoop(sigCtx, cmd, watcher, dir, fluxReconciler, debug)
	}()

	if initialApply {
		executeAndReportApply(sigCtx, cmd, dir, "initial apply")
	}

	// Wait for the event loop to complete and propagate its error.
	return <-errCh
}

// eventLoop processes fsnotify events with debouncing.
//
// Applies are serialized through a single worker goroutine fed by a
// capacity-1 channel (applyCh). Rapid file events are coalesced: if a
// pending apply is already queued the stale entry is replaced with the
// latest changed file before the worker consumes it.
func eventLoop(
	ctx context.Context,
	cmd *cobra.Command,
	watcher *fsnotify.Watcher,
	dir string,
	fluxReconciler *flux.Reconciler,
	debug bool,
) error {
	state := &debounceState{}

	// applyCh serializes applies.  Capacity 1 ensures at most one apply is
	// pending at any time; coalescing replaces a queued entry with the latest.
	applyCh := make(chan string, 1)

	// Single worker: runs applies one at a time, stops when ctx is cancelled.
	go applyWorker(ctx, cmd, dir, applyCh, fluxReconciler)

	// Polling fallback: periodically scan for modification time changes to
	// catch events missed by fsnotify (CI runners, atomic-save editors).
	// Runs independently from the fsnotify debounce state so that fsnotify
	// events cannot invalidate polling-detected changes.
	go pollForChanges(ctx, dir, applyCh, debug)

	defer cancelPendingDebounce(state)

	return dispatchEvents(ctx, cmd, watcher, state, applyCh, debug)
}

// applyWorker runs applies one at a time, stopping when ctx is cancelled.
// The kustomization cache is owned exclusively by this single goroutine,
// so no mutex is needed.
func applyWorker(
	ctx context.Context,
	cmd *cobra.Command,
	dir string,
	applyCh <-chan string,
	fluxReconciler *flux.Reconciler,
) {
	var cachedKustomizations []flux.KustomizationInfo

	for {
		select {
		case <-ctx.Done():
			return
		case file, ok := <-applyCh:
			if !ok {
				return
			}

			applyAndReport(ctx, cmd, dir, file, fluxReconciler, &cachedKustomizations)
		}
	}
}

// dispatchEvents reads fsnotify events and errors, debouncing file changes
// before enqueuing them on applyCh.
func dispatchEvents(
	ctx context.Context,
	cmd *cobra.Command,
	watcher *fsnotify.Watcher,
	state *debounceState,
	applyCh chan string,
	debug bool,
) error {
	for {
		select {
		case <-ctx.Done():
			cmd.PrintErrln("\n✋ watcher stopped")

			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			handleFileEvent(event, watcher, cmd, state, applyCh, debug)

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}

			return fmt.Errorf("file watcher: %w", watchErr)
		}
	}
}

// handleFileEvent processes a single fsnotify event: filters irrelevant ops,
// registers new directories, and schedules a debounced apply.
func handleFileEvent(
	event fsnotify.Event,
	watcher *fsnotify.Watcher,
	cmd *cobra.Command,
	state *debounceState,
	applyCh chan string,
	debug bool,
) {
	if !isRelevantEvent(event) {
		return
	}

	if debug {
		fmt.Fprintf(os.Stderr, "  fsnotify: %s %s\n", event.Op, event.Name)
	}

	// If a new directory was created, watch it too.
	if event.Has(fsnotify.Create) {
		tryAddDirectory(watcher, event.Name, cmd)
	}

	scheduleApply(state, event.Name, applyCh)
}

// executeAndReportApply runs kubectl apply against the given directory and
// prints a timestamped result line with elapsed time. The label parameter
// (e.g. "initial apply", "reconciling") is printed before the apply starts.
// Used directly for the initial full-root sync and called by applyAndReport
// for scoped reconciles, keeping timing and formatting in one place.
func executeAndReportApply(ctx context.Context, cmd *cobra.Command, dir, label string) {
	if ctx.Err() != nil {
		return
	}

	timestamp := time.Now().Format("15:04:05")
	cmd.PrintErrf("[%s] %s\n", timestamp, label)

	start := time.Now()
	applyErr := runKubectlApply(ctx, cmd, dir)
	elapsed := time.Since(start)

	timestamp = time.Now().Format("15:04:05")

	if applyErr != nil {
		cmd.PrintErrf(
			"[%s] ✗ apply failed (%s): %v\n\n",
			timestamp,
			formatElapsed(elapsed),
			applyErr,
		)
	} else {
		cmd.PrintErrf("[%s] ✓ apply succeeded (%s)\n\n", timestamp, formatElapsed(elapsed))
	}
}

// applyAndReport runs kubectl apply and prints a timestamped status line with
// elapsed time. It scopes the apply to the nearest Kustomization subtree
// containing the changed file, falling back to a full reconcile when the
// change is at the root level or no kustomization.yaml boundary is found.
//
// When a Flux reconciler is available, it additionally triggers selective
// Flux Kustomization CR reconciliation for the affected subtree. If no
// CRs match the change, the root Kustomization CR is reconciled instead.
// When multiple CRs match (e.g. a parent directory change affects several
// child Kustomizations), all matching CRs are reconciled.
func applyAndReport(
	ctx context.Context,
	cmd *cobra.Command,
	dir, changedFile string,
	fluxReconciler *flux.Reconciler,
	cachedKustomizations *[]flux.KustomizationInfo,
) {
	if ctx.Err() != nil {
		return
	}

	timestamp := time.Now().Format("15:04:05")

	relFile, err := filepath.Rel(dir, changedFile)
	if err != nil {
		relFile = changedFile
	}

	cmd.PrintErrf("[%s] change detected: %s\n", timestamp, relFile)

	applyDir := findKustomizationDir(changedFile, dir)

	label := "reconciling"

	if applyDir != dir {
		relDir, relErr := filepath.Rel(dir, applyDir)
		if relErr != nil {
			relDir = applyDir
		}

		label = "→ reconciling subtree: " + relDir
	}

	executeAndReportApply(ctx, cmd, applyDir, label)

	reconcileFluxSelectively(ctx, cmd, fluxReconciler, applyDir, dir, cachedKustomizations)
}

// formatElapsed formats a duration as a compact human-readable string
// (e.g. "0.3s", "1.2s", "45.0s").
func formatElapsed(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// findKustomizationDir walks up from the changed path to find the nearest
// directory containing a kustomization file recognized by kubectl
// (kustomization.yaml, kustomization.yml, or Kustomization). Both changedFile
// and rootDir are normalized to absolute paths before comparison so that mixed
// relative / absolute inputs are handled correctly. If the nearest match is
// the root watch directory or no match is found, rootDir is returned
// (triggering a full reconcile). When changedFile is itself a directory the
// search starts there instead of at its parent.
func findKustomizationDir(changedFile, rootDir string) string {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return rootDir
	}

	absChanged, err := filepath.Abs(changedFile)
	if err != nil {
		return absRoot
	}

	// When the changed path is a directory, start the search there;
	// otherwise start at its parent directory.
	dir := filepath.Dir(absChanged)

	info, statErr := os.Stat(absChanged)
	if statErr == nil && info.IsDir() {
		dir = absChanged
	}

	for {
		if hasKustomizationFile(dir) {
			return dir
		}

		// Reached the root watch directory without finding a nested kustomization.
		if dir == absRoot {
			return absRoot
		}

		parent := filepath.Dir(dir)

		// Reached the filesystem root without finding anything.
		if parent == dir {
			return absRoot
		}

		dir = parent
	}
}

// tryCreateFluxReconciler attempts to create a Flux reconciler using the
// current kubeconfig. Returns nil if the reconciler cannot be created
// (e.g., no kubeconfig, cluster unreachable). The caller should treat
// a nil return as "Flux is unavailable; skip selective reconciliation".
func tryCreateFluxReconciler() *flux.Reconciler {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(nil)
	if kubeconfigPath == "" {
		return nil
	}

	r, err := flux.NewReconciler(kubeconfigPath)
	if err != nil {
		return nil
	}

	return r
}

// reconcileFluxSelectively triggers Flux Kustomization CR reconciliation
// scoped to the affected subtree. It uses a cached list of Kustomization CRs
// to avoid an API round-trip on every apply, refreshing the cache on the first
// call or when a previous list returned an error.
//
// When the reconciler is nil or Flux is not available, the function silently
// returns. Root-level or unmappable changes reconcile the root Kustomization
// CR. When multiple CRs match (e.g. a parent directory change affects several
// child Kustomizations), all matching CRs are reconciled individually.
func reconcileFluxSelectively(
	ctx context.Context,
	cmd *cobra.Command,
	reconciler *flux.Reconciler,
	applyDir, rootDir string,
	cachedKustomizations *[]flux.KustomizationInfo,
) {
	if reconciler == nil || ctx.Err() != nil {
		return
	}

	// Populate cache on first call or refresh on previous list error.
	if len(*cachedKustomizations) == 0 {
		kustomizations, err := reconciler.ListKustomizationPaths(ctx)
		if err != nil || len(kustomizations) == 0 {
			return
		}

		*cachedKustomizations = kustomizations
	}

	// Root-level change or no subtree match: reconcile the root CR.
	if applyDir == rootDir {
		reconcileRootKustomization(ctx, cmd, reconciler, "root")

		return
	}

	matches := matchFluxKustomizations(applyDir, rootDir, *cachedKustomizations)

	if len(matches) == 0 {
		reconcileRootKustomization(ctx, cmd, reconciler, "root fallback")

		return
	}

	reconcileMatchedKustomizations(ctx, cmd, reconciler, matches)
}

// reconcileRootKustomization triggers reconciliation of the root Kustomization
// CR and prints a timestamped status line. The label parameter (e.g. "root",
// "root fallback") is included in the output to indicate the trigger reason.
func reconcileRootKustomization(
	ctx context.Context,
	cmd *cobra.Command,
	reconciler *flux.Reconciler,
	label string,
) {
	timestamp := time.Now().Format("15:04:05")

	err := reconciler.TriggerKustomizationReconciliation(ctx)
	if err != nil {
		cmd.PrintErrf(
			"[%s] ⚠ flux reconcile (%s): %v\n",
			timestamp, label, err,
		)
	} else {
		cmd.PrintErrf(
			"[%s] ↻ flux: reconciled root kustomization (%s)\n",
			timestamp, label,
		)
	}
}

// reconcileMatchedKustomizations triggers reconciliation of each named
// Kustomization CR and prints a timestamped status line per CR.
func reconcileMatchedKustomizations(
	ctx context.Context,
	cmd *cobra.Command,
	reconciler *flux.Reconciler,
	matches []string,
) {
	timestamp := time.Now().Format("15:04:05")

	for _, name := range matches {
		err := reconciler.TriggerNamedKustomizationReconciliation(ctx, name)
		if err != nil {
			cmd.PrintErrf("[%s] ⚠ flux reconcile %q: %v\n", timestamp, name, err)
		} else {
			cmd.PrintErrf("[%s] ↻ flux: reconciled kustomization %q\n", timestamp, name)
		}
	}
}

// matchFluxKustomizations maps a changed directory (absolute path) to the
// Flux Kustomization CR(s) whose spec.path matches. A match occurs when
// the normalized relative path of the changed directory equals or is a
// parent/child of the CR's spec.path. Returns nil when no CRs match.
func matchFluxKustomizations(
	changedDir, rootDir string,
	kustomizations []flux.KustomizationInfo,
) []string {
	relDir, err := filepath.Rel(rootDir, changedDir)
	if err != nil {
		return nil
	}

	relDir = normalizeFluxPath(relDir)
	if relDir == "" {
		return nil
	}

	var matches []string

	for _, kustomization := range kustomizations {
		ksPath := normalizeFluxPath(kustomization.Path)
		if ksPath == "" {
			continue
		}

		if ksPath == relDir ||
			strings.HasPrefix(ksPath, relDir+"/") ||
			strings.HasPrefix(relDir, ksPath+"/") {
			matches = append(matches, kustomization.Name)
		}
	}

	return matches
}

// normalizeFluxPath strips leading "./" and cleans the path, converting
// OS-specific separators to forward slashes so prefix checks work
// consistently across platforms. Returns "" for paths that resolve to "."
// (root-level).
func normalizeFluxPath(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = filepath.ToSlash(filepath.Clean(path))

	if path == "." {
		return ""
	}

	return path
}

// hasKustomizationFile reports whether dir contains a regular kustomization
// file recognized by kubectl (kustomization.yaml, kustomization.yml, or
// Kustomization). Non-ErrNotExist errors (e.g., permission denied) are treated
// as a positive match so that transient stat failures do not silently switch
// the apply mode from -k to -f --recursive.
func hasKustomizationFile(dir string) bool {
	for _, name := range kustomizationFileNames {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil {
			if info.Mode().IsRegular() {
				return true
			}

			continue
		}

		if !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}

	return false
}

// runKubectlApply executes kubectl apply against the provided directory,
// which may be the root watch directory or a scoped Kustomization subtree.
// When the directory contains a kustomization file recognized by kubectl
// (kustomization.yaml, kustomization.yml, or Kustomization), it applies
// using -k (kustomize mode). Otherwise it falls back to -f with --recursive
// to apply all manifest files in the directory tree.
// The provided context is forwarded to the cobra command so that Ctrl+C
// (which cancels ctx) also terminates an in-flight apply promptly.
func runKubectlApply(ctx context.Context, cmd *cobra.Command, dir string) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    cmd.OutOrStdout(),
		ErrOut: cmd.ErrOrStderr(),
	})

	applyCmd := client.CreateApplyCommand(kubeconfigPath)

	var mode string

	if hasKustomizationFile(dir) {
		applyCmd.SetArgs([]string{"-k", dir})

		mode = "-k"
	} else {
		applyCmd.SetArgs([]string{"-f", dir, "--recursive"})

		mode = "-f --recursive"
	}

	applyCmd.SetOut(cmd.OutOrStdout())
	applyCmd.SetErr(cmd.ErrOrStderr())

	err := kubectl.ExecuteSafely(ctx, applyCmd)
	if err != nil {
		return fmt.Errorf("kubectl apply (%s): %w", mode, err)
	}

	return nil
}

// isRelevantEvent returns true for write, create, remove, and rename events.
func isRelevantEvent(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Remove) ||
		event.Has(fsnotify.Rename)
}

// addRecursive walks the directory tree and adds all directories to the watcher.
func addRecursive(watcher *fsnotify.Watcher, root string) error {
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			watchErr := watcher.Add(path)
			if watchErr != nil {
				return fmt.Errorf("watch %q: %w", path, watchErr)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk directory %q: %w", root, err)
	}

	return nil
}

// tryAddDirectory attempts to add a path to the watcher if it is a directory.
func tryAddDirectory(watcher *fsnotify.Watcher, path string, cmd *cobra.Command) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	if info.IsDir() {
		addErr := addRecursive(watcher, path)
		if addErr != nil {
			cmd.PrintErrf("⚠️  failed to watch new directory %s: %v\n", path, addErr)
		}
	}
}

// buildFileSnapshot walks the directory tree and records modification times
// for all regular files. Uses os.Stat instead of d.Info() to avoid stale
// cached stat data when files are replaced via rename (e.g. sed -i).
func buildFileSnapshot(dir string) fileSnapshot {
	snap := make(fileSnapshot)

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // skip non-regular entries and stat errors
		}

		snap[path] = info.ModTime()

		return nil
	})

	return snap
}

// detectChangedFile scans the directory for a file whose modification time
// differs from the snapshot. Returns the first changed file path found and
// updates the snapshot in place. Returns "" if no changes are detected.
func detectChangedFile(dir string, snapshot fileSnapshot) string {
	changed := scanForModifiedFiles(dir, snapshot)
	deleted := scanForDeletedFiles(snapshot)

	if changed != "" {
		return changed
	}

	return deleted
}

// scanForModifiedFiles walks the directory tree and returns the first file
// whose modification time differs from the snapshot. Updates the snapshot
// in place for all changed files encountered during the walk.
func scanForModifiedFiles(dir string, snapshot fileSnapshot) string {
	var changed string

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil //nolint:nilerr // skip non-regular entries and stat errors
		}

		modTime := info.ModTime()
		if prev, ok := snapshot[path]; !ok || !modTime.Equal(prev) {
			snapshot[path] = modTime

			if changed == "" {
				changed = path
			}
		}

		return nil
	})

	return changed
}

// scanForDeletedFiles checks all snapshot entries and removes any whose
// path is missing or is no longer a regular file. Returns the first
// deleted path found, or "".
func scanForDeletedFiles(snapshot fileSnapshot) string {
	var changed string

	for path := range snapshot {
		info, statErr := os.Lstat(path)

		if statErr != nil || !info.Mode().IsRegular() {
			delete(snapshot, path)

			if changed == "" {
				changed = path
			}
		}
	}

	return changed
}

// pollForChanges periodically scans the watched directory for modified files
// and enqueues applies directly on applyCh. This provides a fallback for
// environments where fsnotify events may be lost (CI runners, atomic-save
// editors using create+rename).
//
// Unlike the fsnotify path, polling bypasses the shared debounce state
// entirely. The polling interval (3s) already provides natural debouncing,
// and a blocking send ensures the change is reliably delivered to the
// apply worker — it cannot be silently dropped by a generation mismatch
// or a non-blocking channel send.
func pollForChanges(ctx context.Context, dir string, applyCh chan string, debug bool) {
	snapshot := buildFileSnapshot(dir)

	logPollSnapshot(dir, snapshot, debug)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	tickCount := 0

	for {
		select {
		case <-ctx.Done():
			if debug {
				fmt.Fprintf(os.Stderr, "  poll: stopped after %d ticks\n", tickCount)
			}

			return
		case <-ticker.C:
			tickCount++

			if tickCount%5 == 1 {
				logPollTick(tickCount, dir, snapshot, debug)
			}

			if !pollHandleChange(ctx, dir, snapshot, applyCh, tickCount, debug) {
				return
			}
		}
	}
}

// pollHandleChange detects and enqueues a changed file for the apply worker.
// Returns false if the context was cancelled during the blocking send.
func pollHandleChange(
	ctx context.Context,
	dir string,
	snapshot fileSnapshot,
	applyCh chan string,
	tickCount int,
	debug bool,
) bool {
	changed := detectChangedFile(dir, snapshot)
	if changed == "" {
		return true
	}

	if debug {
		fmt.Fprintf(os.Stderr, "  poll: change on tick %d: %s\n", tickCount, changed)
	}

	// Blocking send: guaranteed delivery to the apply worker.
	select {
	case applyCh <- changed:
		if debug {
			fmt.Fprintf(os.Stderr, "  poll: enqueued for apply\n")
		}

		return true
	case <-ctx.Done():
		return false
	}
}

// logPollSnapshot logs the initial snapshot contents (temporary diagnostics).
func logPollSnapshot(dir string, snapshot fileSnapshot, debug bool) {
	if !debug {
		return
	}

	fmt.Fprintf(os.Stderr, "  poll: started, %d files in snapshot\n", len(snapshot))

	for path, modTime := range snapshot {
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(os.Stderr, "  poll:   %s (mod=%s)\n", rel, modTime.Format(time.RFC3339Nano))
	}
}

// logPollTick logs file modTimes vs snapshot on periodic ticks (temporary diagnostics).
func logPollTick(tick int, dir string, snapshot fileSnapshot, debug bool) {
	if !debug {
		return
	}

	fmt.Fprintf(os.Stderr, "  poll: tick %d, scanning %s\n", tick, dir)

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries and directories
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil //nolint:nilerr // skip files that can't be stat'd
		}

		rel, _ := filepath.Rel(dir, path)
		cur := info.ModTime()
		prev, inSnap := snapshot[path]

		switch {
		case !inSnap:
			fmt.Fprintf(os.Stderr, "  poll:   %s NEW mod=%s\n", rel, cur.Format(time.RFC3339Nano))
		case !cur.Equal(prev):
			fmt.Fprintf(os.Stderr, "  poll:   %s CHANGED snap=%s cur=%s\n",
				rel, prev.Format(time.RFC3339Nano), cur.Format(time.RFC3339Nano))
		default:
			fmt.Fprintf(os.Stderr, "  poll:   %s unchanged mod=%s\n",
				rel, cur.Format(time.RFC3339Nano))
		}

		return nil
	})
}

// NewWorkloadCmd creates and returns the workload command group namespace.
func NewWorkloadCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workload",
		Short: "Manage workload operations",
		Long: "Manage workload operations including resource inspection, " +
			"GitOps reconciliation, and lifecycle management.\n\n" +
			"Read operations:\n" +
			"  get       - List resources with optional -o json for structured output including status/conditions\n" +
			"  describe  - Show detailed resource info including events, conditions, and error details\n" +
			"  logs      - Print container logs (use --tail=N, --previous for crash diagnostics)\n" +
			"  explain   - Show API documentation for a resource kind\n" +
			"  images    - List container images required by cluster components\n" +
			"  wait      - Wait for a specific condition on resources\n\n" +
			"Write operations:\n" +
			"  apply, create, debug, delete, edit, exec, export, expose, import, install, push, " +
			"reconcile, rollout, scale, watch\n\n" +
			"GitOps diagnostics: Use 'get' with Flux resources (kustomization, helmrelease, " +
			"ocirepository -A -o json) or ArgoCD resources (application -A -o json) to check " +
			"reconciliation status, health, and errors in a single call.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage: true,
		Annotations: map[string]string{
			// Consolidate workload subcommands into tools split by permission:
			// workload_read and workload_write.
			// The "workload_command" parameter will select which command to execute.
			annotations.AnnotationConsolidate: "workload_command",
		},
	}

	cmd.AddCommand(NewReconcileCmd(runtimeContainer))
	cmd.AddCommand(NewPushCmd(runtimeContainer))
	cmd.AddCommand(NewApplyCmd())
	cmd.AddCommand(NewCreateCmd(runtimeContainer))
	cmd.AddCommand(NewDebugCmd())
	cmd.AddCommand(NewDeleteCmd())
	cmd.AddCommand(NewDescribeCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewExecCmd())
	cmd.AddCommand(NewExplainCmd())
	cmd.AddCommand(NewExportCmd(runtimeContainer))
	cmd.AddCommand(NewExposeCmd())
	cmd.AddCommand(NewGetCmd())
	cmd.AddCommand(gen.NewGenCmd(runtimeContainer))
	cmd.AddCommand(NewImagesCmd())
	cmd.AddCommand(NewImportCmd(runtimeContainer))
	cmd.AddCommand(NewInstallCmd(runtimeContainer))
	cmd.AddCommand(NewLogsCmd())
	cmd.AddCommand(NewRolloutCmd())
	cmd.AddCommand(NewScaleCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewWaitCmd())
	cmd.AddCommand(NewWatchCmd())

	return cmd
}
