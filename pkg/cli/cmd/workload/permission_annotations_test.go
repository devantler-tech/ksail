package workload_test

// cspell:words toolgen

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// TestWriteWorkloadCommandsHaveWritePermission verifies that each
// state-mutating workload command listed in testCases carries the "write"
// permission annotation. The AI toolgen system uses this annotation to
// classify commands into read/write tool groups (workload_read vs
// workload_write), which enables user-confirmation prompts before any
// destructive or mutating operation exposed through these commands.
func TestWriteWorkloadCommandsHaveWritePermission(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "apply", cmd: workload.NewApplyCmd()},
		{name: "create", cmd: workload.NewCreateCmd(di.New(nil))},
		{name: "delete", cmd: workload.NewDeleteCmd()},
		{name: "edit", cmd: workload.NewEditCmd()},
		{name: "exec", cmd: workload.NewExecCmd()},
		{name: "expose", cmd: workload.NewExposeCmd()},
		{name: "import", cmd: workload.NewImportCmd(di.New(nil))},
		{name: "install", cmd: workload.NewInstallCmd(di.New(nil))},
		{name: "push", cmd: workload.NewPushCmd(di.New(nil))},
		{name: "reconcile", cmd: workload.NewReconcileCmd(di.New(nil))},
		{name: "rollout", cmd: workload.NewRolloutCmd()},
		{name: "scale", cmd: workload.NewScaleCmd()},
		{name: "watch", cmd: workload.NewWatchCmd()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			perm, ok := testCase.cmd.Annotations[annotations.AnnotationPermission]
			if !ok {
				t.Fatalf(
					"command %q is missing %q annotation; "+
						"add Annotations: map[string]string{annotations.AnnotationPermission: \"write\"}",
					testCase.name,
					annotations.AnnotationPermission,
				)
			}

			if perm != "write" {
				t.Fatalf(
					"command %q has permission %q, expected \"write\"",
					testCase.name,
					perm,
				)
			}
		})
	}
}

// TestReadWorkloadCommandsDoNotHaveWritePermission verifies that read-only
// workload commands do NOT carry the "ai.toolgen.permission" annotation at all.
// These commands must not require user confirmation in the AI toolgen system.
func TestReadWorkloadCommandsDoNotHaveWritePermission(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "describe", cmd: workload.NewDescribeCmd()},
		{name: "explain", cmd: workload.NewExplainCmd()},
		{name: "export", cmd: workload.NewExportCmd(di.New(nil))},
		{name: "get", cmd: workload.NewGetCmd()},
		{name: "images", cmd: workload.NewImagesCmd()},
		{name: "logs", cmd: workload.NewLogsCmd()},
		{name: "wait", cmd: workload.NewWaitCmd()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, hasAnnotation := testCase.cmd.Annotations[annotations.AnnotationPermission]; hasAnnotation {
				t.Fatalf(
					"read-only command %q must not have the %q annotation set; "+
						"remove Annotations: map[string]string{annotations.AnnotationPermission: ...}",
					testCase.name,
					annotations.AnnotationPermission,
				)
			}
		})
	}
}
