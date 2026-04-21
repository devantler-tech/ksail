package gen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestGenClusterRoleBinding(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewClusterRoleBindingCmd, []string{
		"test-clusterrolebinding",
		"--clusterrole=test-clusterrole",
		"--user=test-user",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenConfigMap(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewConfigMapCmd, []string{
		"test-config",
		"--from-literal=APP_ENV=production",
		"--from-literal=DEBUG=false",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenCronJob(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewCronJobCmd, []string{
		"test-cronjob",
		"--image=busybox:latest",
		"--schedule=*/5 * * * *",
		"--restart=OnFailure",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenDeployment(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewDeploymentCmd, []string{
		"test-deployment",
		"--image=nginx:1.21",
		"--replicas=3",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

// execGen runs the command returned by cmdFactory with the provided args,
// returning stdout, stderr, and any execution error.
func execGen(
	t *testing.T,
	cmdFactory func(*di.Runtime) *cobra.Command,
	args []string,
) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := cmdFactory(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestNewGenCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewGenCmd(rt)

	require.NotNil(t, cmd, "expected gen command to be created")
	require.Equal(t, "gen", cmd.Use, "expected command use to be 'gen'")
	require.NotEmpty(t, cmd.Short, "expected command to have short description")
	require.NotEmpty(t, cmd.Long, "expected command to have long description")
}

func TestNewGenCmd_HasSubcommands(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewGenCmd(rt)

	subcommands := cmd.Commands()
	require.NotEmpty(t, subcommands, "expected gen command to have subcommands")

	// Check for some expected subcommands
	subcommandNames := make(map[string]bool)
	for _, subcmd := range subcommands {
		subcommandNames[subcmd.Name()] = true
	}

	expectedSubcommands := []string{
		"namespace",
		"deployment",
		"service",
		"configmap",
		"secret",
		"job",
		"cronjob",
		"ingress",
		"role",
		"rolebinding",
		"clusterrole",
		"clusterrolebinding",
		"serviceaccount",
		"quota",
		"poddisruptionbudget",
		"priorityclass",
		"helmrelease",
	}

	for _, expected := range expectedSubcommands {
		require.True(t, subcommandNames[expected], "expected subcommand %q to be present", expected)
	}
}

func TestNewNamespaceCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewNamespaceCmd(rt)

	require.NotNil(t, cmd, "expected namespace command to be created")
	require.Equal(t, "namespace", cmd.Name(), "expected command name to be 'namespace'")
}

func TestNewDeploymentCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewDeploymentCmd(rt)

	require.NotNil(t, cmd, "expected deployment command to be created")
	require.Equal(t, "deployment", cmd.Name(), "expected command name to be 'deployment'")
}

func TestNewServiceCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewServiceCmd(rt)

	require.NotNil(t, cmd, "expected service command to be created")
	require.Equal(t, "service", cmd.Name(), "expected command name to be 'service'")
}

func TestNewConfigMapCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewConfigMapCmd(rt)

	require.NotNil(t, cmd, "expected configmap command to be created")
	require.Equal(t, "configmap", cmd.Name(), "expected command name to be 'configmap'")
}

func TestNewSecretCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewSecretCmd(rt)

	require.NotNil(t, cmd, "expected secret command to be created")
	require.Equal(t, "secret", cmd.Name(), "expected command name to be 'secret'")
}

func TestNewJobCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewJobCmd(rt)

	require.NotNil(t, cmd, "expected job command to be created")
	require.Equal(t, "job", cmd.Name(), "expected command name to be 'job'")
}

func TestNewCronJobCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewCronJobCmd(rt)

	require.NotNil(t, cmd, "expected cronjob command to be created")
	require.Equal(t, "cronjob", cmd.Name(), "expected command name to be 'cronjob'")
}

func TestNewIngressCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewIngressCmd(rt)

	require.NotNil(t, cmd, "expected ingress command to be created")
	require.Equal(t, "ingress", cmd.Name(), "expected command name to be 'ingress'")
}

func TestNewRoleCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewRoleCmd(rt)

	require.NotNil(t, cmd, "expected role command to be created")
	require.Equal(t, "role", cmd.Name(), "expected command name to be 'role'")
}

func TestNewRoleBindingCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewRoleBindingCmd(rt)

	require.NotNil(t, cmd, "expected rolebinding command to be created")
	require.Equal(t, "rolebinding", cmd.Name(), "expected command name to be 'rolebinding'")
}

func TestNewClusterRoleCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewClusterRoleCmd(rt)

	require.NotNil(t, cmd, "expected clusterrole command to be created")
	require.Equal(t, "clusterrole", cmd.Name(), "expected command name to be 'clusterrole'")
}

func TestNewClusterRoleBindingCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewClusterRoleBindingCmd(rt)

	require.NotNil(t, cmd, "expected clusterrolebinding command to be created")
	require.Equal(
		t,
		"clusterrolebinding",
		cmd.Name(),
		"expected command name to be 'clusterrolebinding'",
	)
}

func TestNewServiceAccountCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewServiceAccountCmd(rt)

	require.NotNil(t, cmd, "expected serviceaccount command to be created")
	require.Equal(t, "serviceaccount", cmd.Name(), "expected command name to be 'serviceaccount'")
}

func TestNewQuotaCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewQuotaCmd(rt)

	require.NotNil(t, cmd, "expected quota command to be created")
	require.Equal(t, "quota", cmd.Name(), "expected command name to be 'quota'")
}

func TestNewPodDisruptionBudgetCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewPodDisruptionBudgetCmd(rt)

	require.NotNil(t, cmd, "expected poddisruptionbudget command to be created")
	require.Equal(
		t,
		"poddisruptionbudget",
		cmd.Name(),
		"expected command name to be 'poddisruptionbudget'",
	)
}

func TestNewPriorityClassCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewPriorityClassCmd(rt)

	require.NotNil(t, cmd, "expected priorityclass command to be created")
	require.Equal(t, "priorityclass", cmd.Name(), "expected command name to be 'priorityclass'")
}

func TestNewHelmReleaseCmd(t *testing.T) {
	t.Parallel()

	rt := di.NewRuntime()
	cmd := gen.NewHelmReleaseCmd(rt)

	require.NotNil(t, cmd, "expected helmrelease command to be created")
	require.Equal(t, "helmrelease", cmd.Name(), "expected command name to be 'helmrelease'")
}

func TestGenHelmReleaseSimple(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"podinfo",
		"--source=HelmRepository/podinfo",
		"--chart=podinfo",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithAllFlags(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--namespace=production",
		"--source=HelmRepository/charts.flux-system",
		"--chart=webapp",
		"--chart-version=^1.0.0",
		"--interval=5m",
		"--timeout=10m",
		"--target-namespace=apps",
		"--storage-namespace=flux-system",
		"--create-target-namespace=true",
		"--service-account=webapp-sa",
		"--crds=CreateReplace",
		"--release-name=webapp-prod",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithChartRef(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--chart-ref=OCIRepository/webapp.flux-system",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithDependencies(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--depends-on=database",
		"--depends-on=production/redis",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithValuesFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	valuesFile := filepath.Join(tmpDir, "values.yaml")

	err := os.WriteFile(valuesFile, []byte("image:\n  tag: v2.0.0\nreplicaCount: 3\n"), 0o600)
	require.NoError(t, err)

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values=" + valuesFile,
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithValuesFrom(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values-from=Secret/my-values",
		"--values-from=ConfigMap/common-config",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithVersion(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--namespace=production",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--chart-version=^1.0.0",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseMissingSourceAndRef(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(),
		"either --source with --chart or --chart-ref must be specified")
}

func TestGenHelmReleaseMissingChart(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(),
		"either --source with --chart or --chart-ref must be specified")
}

func TestGenHelmReleaseConflictingSourceAndChartRef(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--chart-ref=OCIRepository/webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot specify both --source/--chart and --chart-ref")
}

func TestGenHelmReleaseInvalidSourceKind(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=InvalidKind/charts",
		"--chart=webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseInvalidChartRefKind(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--chart-ref=InvalidKind/webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseInvalidCRDsPolicy(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--crds=InvalidPolicy",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseWithoutExport(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet implemented")
}

func TestGenHelmReleaseInvalidSourceFormat(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepositoryMissingSlash",
		"--chart=webapp",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid format")
}

func TestGenHelmReleaseInvalidDependencyFormat(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--depends-on=a/b/c",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid depends-on format")
}

func TestGenHelmReleaseNonExistentValuesFile(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values=/nonexistent/path/values.yaml",
		"--export",
	})

	require.Error(t, err)
}

func TestGenHelmReleaseWithGitRepositorySource(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=GitRepository/my-repo",
		"--chart=./charts/webapp",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "GitRepository")
}

func TestGenHelmReleaseWithBucketSource(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=Bucket/my-bucket",
		"--chart=webapp",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "Bucket")
}

func TestGenHelmReleaseWithHelmChartRef(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--chart-ref=HelmChart/webapp.flux-system",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "HelmChart")
}

func TestGenHelmReleaseWithKubeconfigSecretRef(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--kubeconfig-secret-ref=my-kubeconfig",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "my-kubeconfig")
}

func TestGenHelmReleaseWithCaseSensitiveValuesFrom(t *testing.T) {
	t.Parallel()

	// validateKindCaseInsensitive should normalize "secret" to "Secret"
	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values-from=secret/my-values",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, output, "kind: Secret")
}

func TestGenHelmReleaseInvalidValuesFromKind(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--values-from=Deployment/my-config",
		"--export",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestGenHelmReleaseSrcNamespaceFromDot(t *testing.T) {
	t.Parallel()

	// HelmRepository/charts.custom-ns should split into name=charts, namespace=custom-ns
	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"webapp",
		"--source=HelmRepository/charts.custom-ns",
		"--chart=webapp",
		"--export",
	})

	require.NoError(t, err)
	// Verify the sourceRef has name=charts (not charts.custom-ns) and namespace=custom-ns
	require.Contains(t, output, "name: charts\n")
	require.Contains(t, output, "namespace: custom-ns")
}

func TestGenHelmReleaseRequiresName(t *testing.T) {
	t.Parallel()

	_, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"--source=HelmRepository/charts",
		"--chart=webapp",
		"--export",
	})

	require.Error(t, err)
}

func TestGenIngressSimple(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewIngressCmd, []string{
		"test-ingress",
		"--rule=example.com/*=svc:80",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenIngressWithTLS(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewIngressCmd, []string{
		"test-ingress-tls",
		"--rule=secure.example.com/*=svc:443,tls=my-tls-secret",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenIngressMultipleRules(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewIngressCmd, []string{
		"test-ingress-multi",
		"--rule=api.example.com/*=api-svc:8080",
		"--rule=web.example.com/*=web-svc:80",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenJob(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewJobCmd, []string{
		"test-job",
		"--image=busybox:latest",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenNamespace(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewNamespaceCmd, []string{"test-namespace"})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenPodDisruptionBudget(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewPodDisruptionBudgetCmd, []string{
		"test-pdb",
		"--min-available=2",
		"--selector=app=test",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenPriorityClass(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewPriorityClassCmd, []string{
		"test-priority",
		"--value=1000",
		"--description=Test priority class",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenQuota(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewQuotaCmd, []string{
		"test-quota",
		"--hard=cpu=1,memory=1Gi,pods=10",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenRoleBinding(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewRoleBindingCmd, []string{
		"test-rolebinding",
		"--role=test-role",
		"--user=test-user",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenSecretGeneric(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewSecretCmd, []string{
		"generic", "test-secret",
		"--from-literal=key1=value1",
		"--from-literal=key2=value2",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenSecretTLS(t *testing.T) {
	t.Parallel()

	certFile := filepath.Join("testdata", "tls.crt")
	keyFile := filepath.Join("testdata", "tls.key")

	output, _, err := execGen(t, gen.NewSecretCmd, []string{
		"tls", "test-tls-secret",
		"--cert=" + certFile,
		"--key=" + keyFile,
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenSecretDockerRegistry(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewSecretCmd, []string{
		"docker-registry", "test-docker-secret",
		"--docker-server=https://registry.example.com",
		"--docker-username=testuser",
		"--docker-password=testpass123",
		"--docker-email=testuser@example.com",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceClusterIP(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"clusterip", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceNodePort(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"nodeport", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceLoadBalancer(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"loadbalancer", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceExternalName(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"externalname", "test-svc",
		"--external-name=example.com",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceAccount(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceAccountCmd, []string{"test-sa"})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}
