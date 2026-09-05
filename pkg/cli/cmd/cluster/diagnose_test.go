package cluster_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDiagnoseCmd verifies that NewDiagnoseCmd registers the expected
// command with the correct basic shape and flags.
func TestNewDiagnoseCmd(t *testing.T) {
	t.Parallel()

	diagnoseCmd := cluster.NewDiagnoseCmd()
	require.NotNil(t, diagnoseCmd)

	assert.Equal(t, "diagnose", diagnoseCmd.Name())
	assert.Equal(t, "Diagnose failing cluster resources", diagnoseCmd.Short)
	assert.True(t, diagnoseCmd.SilenceUsage)

	nameFlag := diagnoseCmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)
	assert.Equal(t, "n", nameFlag.Shorthand)

	providerFlag := diagnoseCmd.Flags().Lookup("provider")
	require.NotNil(t, providerFlag)
	assert.Equal(t, "p", providerFlag.Shorthand)

	outputFlag := diagnoseCmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "text", outputFlag.DefValue)
}

// TestDiagnoseCmd_InvalidFormatRejectsEarly verifies that an unknown --output
// value is rejected before any cluster interaction takes place.
// This guards against typos like "--output jsn" silently falling back to the
// text path instead of returning an actionable error.
func TestDiagnoseCmd_InvalidFormatRejectsEarly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
	}{
		{name: "typo jsn", format: "jsn"},
		{name: "empty format", format: ""},
		{name: "xml", format: "xml"},
		{name: "pretty", format: "pretty"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnoseCmd := cluster.NewDiagnoseCmd()
			diagnoseCmd.SetOut(io.Discard)
			diagnoseCmd.SetErr(io.Discard)
			diagnoseCmd.SetArgs([]string{"--output", testCase.format})

			err := diagnoseCmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, cluster.ErrUnsupportedOutputFormat,
				"expected ErrUnsupportedOutputFormat for format %q, got: %v",
				testCase.format, err,
			)
		})
	}
}

// TestResolveClusterContext verifies the fix for #4835: cluster info resolves
// the requested cluster's context and returns "" when none matches, so it never
// falls back to the kubeconfig's current context for a non-existent cluster.
func TestResolveClusterContext(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: kind-real
clusters:
- name: kind-real
  cluster:
    server: https://127.0.0.1:6443
- name: kind-dup
  cluster:
    server: https://127.0.0.1:6444
- name: k3d-dup
  cluster:
    server: https://127.0.0.1:6445
contexts:
- name: kind-real
  context:
    cluster: kind-real
    user: kind-real
- name: kind-dup
  context:
    cluster: kind-dup
    user: kind-dup
- name: k3d-dup
  context:
    cluster: k3d-dup
    user: k3d-dup
users:
- name: kind-real
  user: {}
- name: kind-dup
  user: {}
- name: k3d-dup
  user: {}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(kubeconfig), 0o600))

	// A real cluster resolves to its prefixed context.
	ctx, err := cluster.ExportResolveClusterContext(path, "real")
	require.NoError(t, err)
	assert.Equal(t, "kind-real", ctx)

	// A non-existent cluster reports not-found and resolves to "" — no
	// current-context fallback.
	ctx, err = cluster.ExportResolveClusterContext(path, "ghost")
	require.ErrorIs(t, err, cluster.ErrContextNotFound)
	assert.Empty(t, ctx)

	// A name matching multiple contexts surfaces an ambiguity error rather than
	// silently behaving like "not found".
	ctx, err = cluster.ExportResolveClusterContext(path, "dup")
	require.ErrorIs(t, err, cluster.ErrAmbiguousCluster)
	assert.Empty(t, ctx)

	// An unreadable kubeconfig is non-fatal: "" with no error.
	ctx, err = cluster.ExportResolveClusterContext(filepath.Join(dir, "missing"), "real")
	require.NoError(t, err)
	assert.Empty(t, ctx)
}

// TestClusterCmd_RegistersDiagnoseSubcommand verifies that NewClusterCmd wires
// the diagnose subcommand into the cluster command tree so that toolgen can
// expose it as part of the cluster_read tool.
func TestClusterCmd_RegistersDiagnoseSubcommand(t *testing.T) {
	t.Parallel()

	clusterCmd := cluster.NewClusterCmd()
	require.NotNil(t, clusterCmd)

	diagnoseCmd := findClusterSubcommand(clusterCmd, "diagnose")
	require.NotNil(t, diagnoseCmd, "expected 'diagnose' subcommand to be registered")
}

func TestRunDiagnoseTextReport_HealthyCluster(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "my-cluster",
		HealthScore: 100,
		Findings:    []k8s.DiagnoseFinding{},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseTextReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"my-cluster"`)
	assert.Contains(t, out, "looks healthy")
	assert.Contains(t, out, "Health score: 100/100")
}

func TestRunDiagnoseTextReport_WithFindings(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "broken-cluster",
		HealthScore: 75,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityCritical,
				Resource:    "pod/crash-pod (default)",
				Reason:      "CrashLoopBackOff",
				Remediation: "Check pod logs for errors.",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseTextReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"broken-cluster"`)
	assert.Contains(t, out, "Health score: 75/100")
	assert.Contains(t, out, "critical")
	assert.Contains(t, out, "pod/crash-pod (default)")
	assert.Contains(t, out, "CrashLoopBackOff")
	assert.Contains(t, out, "Suggested fix: Check pod logs for errors.")
}

func TestRunDiagnoseTextReport_FindingWithoutRemediation(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "cluster",
		HealthScore: 90,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityWarning,
				Resource:    "pvc/data-pvc (default)",
				Reason:      "Pending",
				Remediation: "",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseTextReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "warning")
	assert.Contains(t, out, "pvc/data-pvc (default)")
	assert.NotContains(t, out, "Suggested fix")
}

func TestRunDiagnoseJSONReport_HealthyCluster(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "my-cluster",
		HealthScore: 100,
		Findings:    []k8s.DiagnoseFinding{},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseJSONReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"clusterName": "my-cluster"`)
	assert.Contains(t, out, `"healthScore": 100`)
	assert.Contains(t, out, `"findings": []`)
}

// TestRunDiagnoseJSONReport_DoesNotEscapeHTML verifies the fix for the JSON
// HTML-escaping issue: '<', '>', '&' (e.g. in remediation hints like "<name>")
// appear literally instead of being </>/&-escaped.
func TestRunDiagnoseJSONReport_DoesNotEscapeHTML(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "broken-cluster",
		HealthScore: 90,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityWarning,
				Resource:    "pvc/stuck (default)",
				Reason:      "PVC is stuck in Pending phase",
				Remediation: "Run 'ksail workload describe pvc/<name> -n <namespace>'.",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseJSONReport(report, &buf)
	require.NoError(t, err)

	out := buf.String()
	// With HTML-escaping disabled, '<', '>' and '&' appear literally. If
	// escaping were enabled they would be emitted as their unicode escape
	// sequences instead, so the literal substring assertions below would fail.
	assert.Contains(t, out, "<name>")
	assert.Contains(t, out, "<namespace>")
}

func TestRunDiagnoseJSONReport_WithFindings(t *testing.T) {
	t.Parallel()

	report := k8s.DiagnoseReport{
		ClusterName: "broken-cluster",
		HealthScore: 75,
		Findings: []k8s.DiagnoseFinding{
			{
				Severity:    k8s.DiagnoseSeverityCritical,
				Resource:    "pod/crash-pod (default)",
				Reason:      "CrashLoopBackOff",
				Remediation: "Check pod logs for errors.",
			},
		},
	}

	var buf strings.Builder

	err := cluster.ExportRunDiagnoseJSONReport(report, &buf)

	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"clusterName": "broken-cluster"`)
	assert.Contains(t, out, `"healthScore": 75`)
	assert.Contains(t, out, `"severity": "critical"`)
	assert.Contains(t, out, `"resource": "pod/crash-pod (default)"`)
	assert.Contains(t, out, `"reason": "CrashLoopBackOff"`)
	assert.Contains(t, out, `"remediation": "Check pod logs for errors."`)
}

// fakeAPIServer serves the empty node/namespace/pod/PVC lists DiagnoseClusterReport
// walks, recording every path it was asked for so a test can prove which cluster
// the diagnosis actually talked to.
func fakeAPIServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()

	var (
		mu    sync.Mutex
		paths []string
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(`{"kind":"NodeList","apiVersion":"v1","items":[]}`))
		case "/api/v1/namespaces":
			_, _ = w.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1",` +
				`"items":[{"metadata":{"name":"default"}}]}`))
		case "/api/v1/namespaces/default/pods":
			_, _ = w.Write([]byte(`{"kind":"PodList","apiVersion":"v1","items":[]}`))
		case "/api/v1/namespaces/default/persistentvolumeclaims":
			_, _ = w.Write([]byte(`{"kind":"PersistentVolumeClaimList","apiVersion":"v1","items":[]}`))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server, &paths
}

// TestDiagnoseCmd_NameSelectsThatClustersContext pins the fix for #6117: with
// --name, the diagnosis must be built against THAT cluster's kubeconfig context,
// not the kubeconfig's current context. The current context points at an
// address nothing listens on, so a client that followed it would fail, while the
// named cluster's context points at a fake API server that records the calls.
//
//nolint:paralleltest // uses t.Setenv and t.Chdir to isolate kubeconfig/config discovery
func TestDiagnoseCmd_NameSelectsThatClustersContext(t *testing.T) {
	server, paths := fakeAPIServer(t)

	kubeconfig := `apiVersion: v1
kind: Config
current-context: kind-other
clusters:
- name: kind-other
  cluster:
    server: http://127.0.0.1:1
- name: kind-target
  cluster:
    server: ` + server.URL + `
contexts:
- name: kind-other
  context:
    cluster: kind-other
    user: kind-other
- name: kind-target
  context:
    cluster: kind-target
    user: kind-target
users:
- name: kind-other
  user: {}
- name: kind-target
  user: {}
`

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))
	t.Setenv("KUBECONFIG", kubeconfigPath)
	// No ksail.yaml in scope: the cluster is identified by --name alone.
	t.Chdir(t.TempDir())

	var out strings.Builder

	diagnoseCmd := cluster.NewDiagnoseCmd()
	diagnoseCmd.SetOut(&out)
	diagnoseCmd.SetErr(io.Discard)
	diagnoseCmd.SetArgs([]string{"--name", "target"})

	err := diagnoseCmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, out.String(), `Cluster "target" looks healthy`)
	assert.Contains(t, out.String(), "Health score: 100/100")
	// The named cluster's API server — not the current context's — answered the diagnosis.
	assert.Contains(t, *paths, "/api/v1/nodes")
}

// TestDiagnoseCmd_NameNotInKubeconfigFails pins the other half of #6117: a --name
// that matches no kubeconfig context is an error, never a silent fallback to the
// current context.
//
//nolint:paralleltest // uses t.Setenv and t.Chdir to isolate kubeconfig/config discovery
func TestDiagnoseCmd_NameNotInKubeconfigFails(t *testing.T) {
	server, paths := fakeAPIServer(t)

	kubeconfig := `apiVersion: v1
kind: Config
current-context: kind-other
clusters:
- name: kind-other
  cluster:
    server: ` + server.URL + `
contexts:
- name: kind-other
  context:
    cluster: kind-other
    user: kind-other
users:
- name: kind-other
  user: {}
`

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))
	t.Setenv("KUBECONFIG", kubeconfigPath)
	t.Chdir(t.TempDir())

	diagnoseCmd := cluster.NewDiagnoseCmd()
	diagnoseCmd.SetOut(io.Discard)
	diagnoseCmd.SetErr(io.Discard)
	diagnoseCmd.SetArgs([]string{"--name", "ghost"})

	err := diagnoseCmd.Execute()
	require.ErrorIs(t, err, cluster.ErrContextNotFound)
	// The current context was never consulted for a cluster that does not exist.
	assert.Empty(t, *paths)
}
