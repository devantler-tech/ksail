package k3dprovisioner_test

import (
	"context"
	"io"
	"testing"

	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	k3dlogger "github.com/k3d-io/k3d/v5/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProvisioner_List_RuntimeUnavailableDoesNotExitProcess drives the REAL k3d
// cluster-list path (no stubbed seam) against a Docker socket that does not
// exist. k3d reacts to an unreachable runtime by calling logrus Fatal, which
// defaults to os.Exit(1); before the fix that terminated the whole host process
// — crashing, for example, the desktop app on boot when Docker Desktop is not
// running. The fix recovers it into ErrK3dRuntimeUnavailable. Simply reaching the
// assertions below proves the process was NOT exited.
func TestProvisioner_List_RuntimeUnavailableDoesNotExitProcess(t *testing.T) {
	// Not parallel: t.Setenv forbids it, and List mutates process-global os.Stdout
	// and the k3d/logrus loggers under an internal mutex.
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/ksail-k3d-runtime-test.sock")

	prov := k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "")

	_, err := prov.List(context.Background())

	require.Error(
		t,
		err,
		"List must return an error when the Docker daemon is unavailable, not exit",
	)
	require.ErrorIs(t, err, k3dprovisioner.ErrK3dRuntimeUnavailable)

	// The loggers must be restored after List, so k3d's process-exiting Fatal and
	// its console output are not left neutralized/muted for later operations.
	assert.NotEqual(t, io.Discard, k3dlogger.Log().Out,
		"k3d logger output must be restored after List")
}

// TestProvisioner_Commands_RuntimeUnavailableDoesNotExitProcess drives every k3d
// command the provisioner runs — the create/delete/start/stop lifecycle commands
// and the add/remove/list agent-node commands used by `cluster update` (update.go)
// — through the REAL runtime against a Docker socket that does not exist. k3d's
// cobra commands call logrus Fatal (os.Exit(1) by default) when the runtime is
// unreachable (Create alone has 19 such calls), and listAgentNodes runs one in its
// OWN goroutine where an unrecovered panic would be uncatchable. Before they routed
// through runK3dSafely, a Docker-down create — or a `cluster update` on a k3d
// cluster — crashed the whole host process (e.g. the desktop app, where these run
// in background goroutines). Reaching the assertions at all proves the process was
// NOT exited.
func TestProvisioner_Commands_RuntimeUnavailableDoesNotExitProcess(t *testing.T) {
	// Not parallel: t.Setenv forbids it, and these exercise the process-global k3d
	// logger and runner.
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/ksail-k3d-commands-test.sock")

	// k3d's commands log to k3d's own logger (the lifecycle/node paths, unlike List,
	// deliberately leave that output visible in production). Mute it for the duration
	// of the test so the real runtime-down output does not clutter the test log.
	k3dLog := k3dlogger.Log()
	originalOut := k3dLog.Out
	k3dLog.SetOutput(io.Discard)

	t.Cleanup(func() { k3dLog.SetOutput(originalOut) })

	const clusterName = "ksail-runtime-test"

	commands := []struct {
		name string
		run  func(p *k3dprovisioner.Provisioner) error
	}{
		{"Create", func(p *k3dprovisioner.Provisioner) error {
			return p.Create(context.Background(), clusterName)
		}},
		{"Delete", func(p *k3dprovisioner.Provisioner) error {
			return p.Delete(context.Background(), clusterName)
		}},
		{"Start", func(p *k3dprovisioner.Provisioner) error {
			return p.Start(context.Background(), clusterName)
		}},
		{"Stop", func(p *k3dprovisioner.Provisioner) error {
			return p.Stop(context.Background(), clusterName)
		}},
		{"ListAgentNodes", func(p *k3dprovisioner.Provisioner) error {
			return p.ListAgentNodesForTest(context.Background(), clusterName)
		}},
		{"AddAgentNodes", func(p *k3dprovisioner.Provisioner) error {
			return p.AddAgentNodesForTest(context.Background(), clusterName, 1)
		}},
		{"RemoveAgentNodes", func(p *k3dprovisioner.Provisioner) error {
			return p.RemoveAgentNodesForTest(context.Background(), clusterName, 1)
		}},
	}

	// Subtests share the parent's DOCKER_HOST (t.Setenv) and the process-global k3d
	// logger, so they must run serially, not in parallel.
	//nolint:paralleltest // serial by design: shared env + process-global k3d logger
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			prov := k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "")

			err := command.run(prov)

			require.Error(
				t,
				err,
				"%s must return an error when the Docker daemon is unavailable, not exit",
				command.name,
			)
			require.ErrorIs(t, err, k3dprovisioner.ErrK3dRuntimeUnavailable)
		})
	}
}
