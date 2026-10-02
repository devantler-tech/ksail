package cluster_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detectingUpgraderFake reads live state the way the real Kind, K3d and Talos
// provisioners do: update hands it the component detector, and
// GetCurrentConfig queries the cluster through it. Detection errors are
// ignored, as the real provisioners fall back to defaults.
type detectingUpgraderFake struct {
	*updatableUpgraderFake

	detector *detector.ComponentDetector
}

func (f *detectingUpgraderFake) SetComponentDetector(
	componentDetector *detector.ComponentDetector,
) {
	f.detector = componentDetector
}

func (f *detectingUpgraderFake) GetCurrentConfig(
	ctx context.Context,
	name string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	if f.detector != nil {
		_, _ = f.detector.DetectComponents(
			ctx, v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker,
		)
	}

	return f.updatableUpgraderFake.GetCurrentConfig(ctx, name)
}

type detectingUpgraderFactory struct{ provisioner *detectingUpgraderFake }

func (f detectingUpgraderFactory) Create(
	context.Context, *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

// Context names for the named-cluster inspection tests. The configured cluster
// name is "named-7382", so the Vanilla convention derives "kind-named-7382";
// the explicit context deliberately does not follow that convention.
const (
	namedClusterName      = "named-7382"
	explicitContextName   = "custom-7382"
	derivedContextName    = "kind-" + namedClusterName
	retargetClusterName   = "retarget-7382"
	retargetContextName   = "kind-" + retargetClusterName
	namedContextKubeUser  = "named-7382-user"
	namedContextKubeToken = "fake"
)

// recordingAPIServer stands in for one kubeconfig context's API server. It
// answers every request with 404 and counts them, so a test can tell which
// context a command actually inspected.
type recordingAPIServer struct {
	server   *httptest.Server
	requests atomic.Int64
}

func newRecordingAPIServer(t *testing.T) *recordingAPIServer {
	t.Helper()

	recorder := &recordingAPIServer{}
	recorder.server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			recorder.requests.Add(1)
			http.NotFound(writer, nil)
		},
	))
	t.Cleanup(recorder.server.Close)

	return recorder
}

// writeNamedContextProject writes a Vanilla project named namedClusterName whose
// kubeconfig holds one context per entry of servers, each pointing at its own
// recording API server. configuredContext is written to
// spec.cluster.connection.context; an empty value leaves the field out.
func writeNamedContextProject(
	t *testing.T,
	configuredContext string,
	servers map[string]*recordingAPIServer,
) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)

	contextLine := ""
	if configuredContext != "" {
		contextLine = "      context: " + configuredContext + "\n"
	}

	writeFile(t, workingDir, "ksail.yaml", "apiVersion: ksail.io/v1alpha1\n"+
		"kind: Cluster\n"+
		"metadata:\n"+
		"  name: "+namedClusterName+"\n"+
		"spec:\n"+
		"  cluster:\n"+
		"    distribution: Vanilla\n"+
		"    distributionConfig: kind.yaml\n"+
		"    metricsServer: Disabled\n"+
		"    localRegistry:\n"+
		"      enabled: false\n"+
		"    connection:\n"+
		"      kubeconfig: ./kubeconfig\n"+
		contextLine,
	)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	writeFile(t, workingDir, "kubeconfig", namedContextKubeconfig(servers))
}

// namedContextKubeconfig renders a kubeconfig with one cluster and context per
// entry of servers, sharing a single token user. It deliberately sets no
// current-context, so nothing can reach a server except by naming its context.
func namedContextKubeconfig(servers map[string]*recordingAPIServer) string {
	var clusters, contexts strings.Builder

	for contextName, recorder := range servers {
		clusters.WriteString("- name: " + contextName + "\n" +
			"  cluster:\n" +
			"    server: " + recorder.server.URL + "\n")
		contexts.WriteString("- name: " + contextName + "\n" +
			"  context:\n" +
			"    cluster: " + contextName + "\n" +
			"    user: " + namedContextKubeUser + "\n")
	}

	return "apiVersion: v1\nkind: Config\n" +
		"clusters:\n" + clusters.String() +
		"contexts:\n" + contexts.String() +
		"users:\n- name: " + namedContextKubeUser + "\n" +
		"  user:\n    token: " + namedContextKubeToken + "\n"
}

// runNamedContextCommand runs the real `ksail cluster <command>` against an
// offline provisioner fake and returns its combined stdout and stderr.
func runNamedContextCommand(t *testing.T, command string, args ...string) string {
	t.Helper()

	provisioner := &detectingUpgraderFake{updatableUpgraderFake: pinnedUpgraderFake()}

	t.Cleanup(cluster.SetProvisionerFactoryForTests(detectingUpgraderFactory{provisioner}))
	t.Cleanup(cluster.ExportSetUpdateUnmanagedGuard(
		func(context.Context, *lifecycle.ResolvedClusterInfo) error { return nil },
	))

	stdoutPath, stderrPath := redirectProcessOutput(t)

	cmd := cluster.NewDiffCmd()
	if command == "update" {
		cmd = cluster.NewUpdateCmd()

		args = append([]string{"--dry-run"}, args...)
	}

	cmd.SetContext(t.Context())
	cmd.SetArgs(args)

	execErr := cmd.Execute()

	stdout, err := os.ReadFile(stdoutPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	stderr, err := os.ReadFile(stderrPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	output := string(stdout) + string(stderr)
	require.NoError(t, execErr, "output:\n%s", output)

	return output
}

// A named configuration with an explicit spec.cluster.connection.context must
// inspect the cluster through that context during diff and update. The
// configured metadata.name names the cluster; it is not a request to retarget
// the connection, so a context that differs from the distribution convention
// (a custom or OIDC context) is kept rather than replaced by "kind-<name>".
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestNamedClusterInspectionUsesExplicitContext(t *testing.T) {
	for _, command := range []string{"diff", "update"} {
		t.Run(command, func(t *testing.T) {
			explicit := newRecordingAPIServer(t)
			derived := newRecordingAPIServer(t)
			writeNamedContextProject(t, explicitContextName, map[string]*recordingAPIServer{
				explicitContextName: explicit,
				derivedContextName:  derived,
			})

			output := runNamedContextCommand(t, command)

			assert.Positive(t, explicit.requests.Load(),
				"the explicit context %q was never inspected; output:\n%s",
				explicitContextName, output)
			assert.Zero(t, derived.requests.Load(),
				"the derived context %q was inspected instead of the explicit one; output:\n%s",
				derivedContextName, output)
		})
	}
}

// Leaving spec.cluster.connection.context blank keeps deriving the
// distribution's normal context from the configured cluster name.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestNamedClusterInspectionDerivesBlankContext(t *testing.T) {
	for _, command := range []string{"diff", "update"} {
		t.Run(command, func(t *testing.T) {
			derived := newRecordingAPIServer(t)
			writeNamedContextProject(t, "", map[string]*recordingAPIServer{
				derivedContextName: derived,
			})

			output := runNamedContextCommand(t, command)

			assert.Positive(t, derived.requests.Load(),
				"the derived context %q was never inspected; output:\n%s",
				derivedContextName, output)
		})
	}
}

// --name is an explicit command-line retarget request: it still replaces the
// configured context with the one derived from the new name, unlike the
// configured metadata.name.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestNameFlagRetargetsExplicitContext(t *testing.T) {
	for _, command := range []string{"diff", "update"} {
		t.Run(command, func(t *testing.T) {
			explicit := newRecordingAPIServer(t)
			retarget := newRecordingAPIServer(t)
			writeNamedContextProject(t, explicitContextName, map[string]*recordingAPIServer{
				explicitContextName: explicit,
				retargetContextName: retarget,
			})

			output := runNamedContextCommand(t, command, "--name", retargetClusterName)

			assert.Positive(t, retarget.requests.Load(),
				"the --name context %q was never inspected; output:\n%s",
				retargetContextName, output)
			assert.Zero(t, explicit.requests.Load(),
				"the configured context %q was inspected despite --name; output:\n%s",
				explicitContextName, output)
		})
	}
}
