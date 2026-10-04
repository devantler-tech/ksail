package cluster_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloudClusterRecorder stands in for a GKE or AKS provisioner. Like the cloud
// API it knows the cluster only by its real name, and it records every name a
// command asks it about. The real provisioners report no provisioner-level
// diff; the recorder does, so the name `cluster diff` resolves is observable too.
type cloudClusterRecorder struct {
	fakeProvisioner

	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
	// expectedName is the only cluster name the stand-in cloud API knows.
	expectedName string
	names        []string
}

func (r *cloudClusterRecorder) Exists(_ context.Context, name string) (bool, error) {
	r.names = append(r.names, name)

	return name == r.expectedName, nil
}

func (r *cloudClusterRecorder) GetCurrentConfig(
	_ context.Context, name string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	r.names = append(r.names, name)

	return &v1alpha1.ClusterSpec{Distribution: r.distribution, Provider: r.provider}, nil, nil
}

func (r *cloudClusterRecorder) DiffConfig(
	_ context.Context, name string, _, _ *v1alpha1.ClusterSpec,
) (*clusterupdate.UpdateResult, error) {
	r.names = append(r.names, name)

	return clusterupdate.NewEmptyUpdateResult(), nil
}

func (*cloudClusterRecorder) Update(
	context.Context, string, *v1alpha1.ClusterSpec, *v1alpha1.ClusterSpec,
	clusterupdate.UpdateOptions,
) (*clusterupdate.UpdateResult, error) {
	return clusterupdate.NewEmptyUpdateResult(), nil
}

type cloudClusterRecorderFactory struct{ recorder *cloudClusterRecorder }

func (f cloudClusterRecorderFactory) Create(
	context.Context, *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.recorder, nil, nil
}

// cloudProject is a GKE or AKS project on disk, as a user would write it.
type cloudProject struct {
	name         string
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
	// context is spec.cluster.connection.context, and the only context in the
	// project's kubeconfig.
	context string
	// configFile and configContent are the distribution config file. An empty
	// content leaves the file out, so the name is parsed from the context when
	// the configuration loads.
	configFile    string
	configContent string
	metadataName  string
}

// expectedName is the name the cloud API knows the project's cluster by: a
// configured metadata.name, and otherwise the name the configuration holds.
func (p cloudProject) expectedName() string {
	if p.metadataName != "" {
		return p.metadataName
	}

	return cloudClusterName
}

func cloudProjects() []cloudProject {
	const (
		gkeConfig = "name: " + cloudClusterName + "\nlocation: europe-north1\n"
		aksConfig = "name: " + cloudClusterName + "\nlocation: swedencentral\n"
	)

	return []cloudProject{
		{
			name:          "GKE gcloud context",
			distribution:  v1alpha1.DistributionGKE,
			provider:      v1alpha1.ProviderGCP,
			context:       gcloudContext,
			configFile:    "gke.yaml",
			configContent: gkeConfig,
		},
		{
			name:         "GKE gcloud context without a cluster spec",
			distribution: v1alpha1.DistributionGKE,
			provider:     v1alpha1.ProviderGCP,
			context:      gcloudContext,
			configFile:   "gke.yaml",
		},
		{
			name:          "GKE gcloud context with a configured name",
			distribution:  v1alpha1.DistributionGKE,
			provider:      v1alpha1.ProviderGCP,
			context:       gcloudContext,
			configFile:    "gke.yaml",
			configContent: gkeConfig,
			metadataName:  cloudClusterName,
		},
		{
			name:          "AKS admin context",
			distribution:  v1alpha1.DistributionAKS,
			provider:      v1alpha1.ProviderAzure,
			context:       aksAdminContext,
			configFile:    "aks.yaml",
			configContent: aksConfig,
		},
		{
			name:          "AKS admin context with a configured name",
			distribution:  v1alpha1.DistributionAKS,
			provider:      v1alpha1.ProviderAzure,
			context:       aksAdminContext,
			configFile:    "aks.yaml",
			configContent: aksConfig,
			metadataName:  cloudClusterName,
		},
	}
}

// writeCloudProject changes into a fresh directory holding the project's
// ksail.yaml, its distribution config file and a kubeconfig whose one context
// points at a server that answers every request with 404.
func writeCloudProject(t *testing.T, project cloudProject) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	metadata := ""
	if project.metadataName != "" {
		metadata = "metadata:\n  name: " + project.metadataName + "\n"
	}

	writeFile(t, workingDir, "ksail.yaml", "apiVersion: ksail.io/v1alpha1\n"+
		"kind: Cluster\n"+
		metadata+
		"spec:\n"+
		"  cluster:\n"+
		"    distribution: "+string(project.distribution)+"\n"+
		"    provider: "+string(project.provider)+"\n"+
		"    distributionConfig: "+project.configFile+"\n"+
		"    connection:\n"+
		"      kubeconfig: ./kubeconfig\n"+
		"      context: "+project.context+"\n",
	)

	if project.configContent != "" {
		writeFile(t, workingDir, project.configFile, project.configContent)
	}

	writeFile(t, workingDir, "kubeconfig", "apiVersion: v1\nkind: Config\n"+
		"clusters:\n- name: "+project.context+"\n"+
		"  cluster:\n    server: "+server.URL+"\n"+
		"contexts:\n- name: "+project.context+"\n"+
		"  context:\n    cluster: "+project.context+"\n    user: cloud-user\n"+
		"users:\n- name: cloud-user\n  user:\n    token: fake\n",
	)
}

// runCloudCommand runs the real `ksail cluster diff` or
// `ksail cluster update --dry-run` in project against a recording provisioner,
// and returns the cluster names the command asked the provisioner about.
func runCloudCommand(t *testing.T, project cloudProject, command string) []string {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	writeCloudProject(t, project)

	recorder := &cloudClusterRecorder{
		distribution: project.distribution,
		provider:     project.provider,
		expectedName: project.expectedName(),
	}

	t.Cleanup(cluster.SetProvisionerFactoryForTests(cloudClusterRecorderFactory{recorder}))
	t.Cleanup(cluster.ExportSetUpdateUnmanagedGuard(
		func(_ context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			recorder.names = append(recorder.names, resolved.ClusterName)

			return nil
		},
	))

	stdoutPath, stderrPath := redirectProcessOutput(t)

	cmd := cluster.NewDiffCmd()
	if command == "update" {
		cmd = cluster.NewUpdateCmd()
		cmd.SetArgs([]string{"--dry-run"})
	} else {
		cmd.SetArgs(nil)
	}

	cmd.SetContext(t.Context())

	execErr := cmd.Execute()

	stdout, err := os.ReadFile(stdoutPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	stderr, err := os.ReadFile(stderrPath) //nolint:gosec // path is under t.TempDir
	require.NoError(t, err)

	require.NoError(t, execErr, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	return recorder.names
}

// `ksail cluster diff` must ask the provisioner about the cluster by the name
// the GKE or AKS configuration holds, whatever context reaches the cluster.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestCloudClusterDiffResolvesConfigurationName(t *testing.T) {
	for _, project := range cloudProjects() {
		t.Run(project.name, func(t *testing.T) {
			names := runCloudCommand(t, project, "diff")

			assert.Equal(t, []string{cloudClusterName}, slices.Compact(names))
		})
	}
}

// `ksail cluster update` must find the existing cluster: the managed-target
// guard and the existence check both get the name the GKE or AKS configuration
// holds, so a cluster that exists is not reported as missing. A configured
// metadata.name derives a new connection context, which update then requires
// in the kubeconfig, so only the unnamed projects are run here.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestCloudClusterUpdateResolvesConfigurationName(t *testing.T) {
	for _, project := range cloudProjects() {
		if project.metadataName != "" {
			continue
		}

		t.Run(project.name, func(t *testing.T) {
			names := runCloudCommand(t, project, "update")

			assert.Equal(t, []string{cloudClusterName}, slices.Compact(names))
		})
	}
}

// A configured metadata.name overrides the name in the GKE or AKS
// configuration, and `ksail cluster update` must carry that override to the
// managed-target guard and the provisioner. The name differs from the one in
// the configuration file, so dropping the override on the way is visible, and
// the kubeconfig holds the connection context that name derives.
//
//nolint:paralleltest // uses t.Chdir and replaces the process's stdout/stderr.
func TestCloudClusterUpdateResolvesMetadataName(t *testing.T) {
	const metadataName = "renamed"

	for _, project := range cloudProjects() {
		if project.metadataName == "" {
			continue
		}

		project.metadataName = metadataName
		project.context = cluster.ExportResolveCreatedContextName(
			project.distribution, project.provider, metadataName,
		)

		t.Run(project.name, func(t *testing.T) {
			names := runCloudCommand(t, project, "update")

			assert.Equal(t, []string{metadataName}, slices.Compact(names))
		})
	}
}
