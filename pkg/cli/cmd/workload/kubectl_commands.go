package workload

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// newKubectlWrapperCommand builds a kubectl wrapper command from a kubectl.Client
// method expression, applying the write-permission annotation uniformly when the
// command mutates cluster state. Command names, flags, and help text come from
// pkg/client/kubectl, so the AI tool surface and generated docs are unchanged.
func newKubectlWrapperCommand(creator kubectlCommandCreator, write bool) *cobra.Command {
	cmd := newKubectlCommand(creator)

	if write {
		// Mark as requiring permission for state-modifying operations. The
		// permission system gates AI tool execution confirmation; kubectl/helm
		// may additionally have their own confirmation prompts.
		cmd.Annotations = map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		}
	}

	return cmd
}

// NewApplyCmd creates the workload apply command.
func NewApplyCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateApplyCommand, true)
}

// NewDeleteCmd creates the workload delete command.
func NewDeleteCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateDeleteCommand, true)
}

// NewExecCmd creates the workload exec command.
func NewExecCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateExecCommand, true)
}

// NewExposeCmd creates the workload expose command.
func NewExposeCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateExposeCommand, true)
}

// NewScaleCmd creates the workload scale command.
func NewScaleCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateScaleCommand, true)
}

// NewRolloutCmd creates the workload rollout command.
func NewRolloutCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateRolloutCommand, true)
}

// NewGetCmd creates the workload get command.
func NewGetCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateGetCommand, false)
}

// NewDescribeCmd creates the workload describe command.
func NewDescribeCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateDescribeCommand, false)
}

// NewExplainCmd creates the workload explain command.
func NewExplainCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateExplainCommand, false)
}

// NewLogsCmd creates the workload logs command.
func NewLogsCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateLogsCommand, false)
}

// NewWaitCmd creates the workload wait command.
func NewWaitCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreateWaitCommand, false)
}

// NewForwardCmd creates the workload forward command.
func NewForwardCmd() *cobra.Command {
	return newKubectlWrapperCommand((*kubectl.Client).CreatePortForwardCommand, false)
}
