package workload

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/hostdebug"
	"github.com/spf13/cobra"
)

const defaultDebugImage = "docker.io/library/alpine:latest"

// ErrHostModePositionalArgs is returned when kubectl-style positional args are
// used with --host mode.
var ErrHostModePositionalArgs = errors.New(
	"--host mode does not accept kubectl-style positional args; " +
		"place the container command after '--' (e.g., debug --host <node> -- /bin/sh)",
)

// NewDebugCmd creates the workload debug command.
//
// Without --host it wraps kubectl debug (ephemeral containers, node debugging).
// With --host <node-name> it performs host-level debugging routed per distribution
// by the pkg/svc/hostdebug service:
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

// runHostDebug resolves the cluster and flag inputs from the cobra command and
// delegates to the hostdebug service for distribution/provider routing.
func runHostDebug(cmd *cobra.Command, nodeName string, args []string) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	contextName := ""
	if cmd.Flags().Lookup("context") != nil {
		contextName, _ = cmd.Flags().GetString("context")
	}

	info, err := clusterdetector.DetectInfo(cmd.Context(), kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("detect cluster info: %w", err)
	}

	debugImage, _ := cmd.Flags().GetString("image")
	if debugImage == "" {
		debugImage = defaultDebugImage
	}

	//nolint:wrapcheck // hostdebug sentinels (ErrNodeNotFound, …) must stay unwrapped for tests
	return hostdebug.Run(cmd.Context(), hostdebug.Options{
		Info:           info,
		KubeconfigPath: kubeconfigPath,
		ContextName:    contextName,
		NodeName:       nodeName,
		Image:          debugImage,
		Args:           args,
	})
}
