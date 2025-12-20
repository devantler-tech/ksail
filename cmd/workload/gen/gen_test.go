package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/cmd/workload/gen"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	"github.com/stretchr/testify/require"
)

func TestNewGenCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewGenCmd(rt)

	require.NotNil(t, cmd, "expected gen command to be created")
	require.Equal(t, "gen", cmd.Use, "expected command use to be 'gen'")
	require.NotEmpty(t, cmd.Short, "expected command to have short description")
	require.NotEmpty(t, cmd.Long, "expected command to have long description")
}

func TestNewGenCmd_HasSubcommands(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
	cmd := gen.NewNamespaceCmd(rt)

	require.NotNil(t, cmd, "expected namespace command to be created")
	require.Equal(t, "namespace", cmd.Name(), "expected command name to be 'namespace'")
}

func TestNewDeploymentCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewDeploymentCmd(rt)

	require.NotNil(t, cmd, "expected deployment command to be created")
	require.Equal(t, "deployment", cmd.Name(), "expected command name to be 'deployment'")
}

func TestNewServiceCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewServiceCmd(rt)

	require.NotNil(t, cmd, "expected service command to be created")
	require.Equal(t, "service", cmd.Name(), "expected command name to be 'service'")
}

func TestNewConfigMapCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewConfigMapCmd(rt)

	require.NotNil(t, cmd, "expected configmap command to be created")
	require.Equal(t, "configmap", cmd.Name(), "expected command name to be 'configmap'")
}

func TestNewSecretCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewSecretCmd(rt)

	require.NotNil(t, cmd, "expected secret command to be created")
	require.Equal(t, "secret", cmd.Name(), "expected command name to be 'secret'")
}

func TestNewJobCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewJobCmd(rt)

	require.NotNil(t, cmd, "expected job command to be created")
	require.Equal(t, "job", cmd.Name(), "expected command name to be 'job'")
}

func TestNewCronJobCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewCronJobCmd(rt)

	require.NotNil(t, cmd, "expected cronjob command to be created")
	require.Equal(t, "cronjob", cmd.Name(), "expected command name to be 'cronjob'")
}

func TestNewIngressCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewIngressCmd(rt)

	require.NotNil(t, cmd, "expected ingress command to be created")
	require.Equal(t, "ingress", cmd.Name(), "expected command name to be 'ingress'")
}

func TestNewRoleCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewRoleCmd(rt)

	require.NotNil(t, cmd, "expected role command to be created")
	require.Equal(t, "role", cmd.Name(), "expected command name to be 'role'")
}

func TestNewRoleBindingCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewRoleBindingCmd(rt)

	require.NotNil(t, cmd, "expected rolebinding command to be created")
	require.Equal(t, "rolebinding", cmd.Name(), "expected command name to be 'rolebinding'")
}

func TestNewClusterRoleCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewClusterRoleCmd(rt)

	require.NotNil(t, cmd, "expected clusterrole command to be created")
	require.Equal(t, "clusterrole", cmd.Name(), "expected command name to be 'clusterrole'")
}

func TestNewClusterRoleBindingCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
	cmd := gen.NewServiceAccountCmd(rt)

	require.NotNil(t, cmd, "expected serviceaccount command to be created")
	require.Equal(t, "serviceaccount", cmd.Name(), "expected command name to be 'serviceaccount'")
}

func TestNewQuotaCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewQuotaCmd(rt)

	require.NotNil(t, cmd, "expected quota command to be created")
	require.Equal(t, "quota", cmd.Name(), "expected command name to be 'quota'")
}

func TestNewPodDisruptionBudgetCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
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

	rt := runtime.NewRuntime()
	cmd := gen.NewPriorityClassCmd(rt)

	require.NotNil(t, cmd, "expected priorityclass command to be created")
	require.Equal(t, "priorityclass", cmd.Name(), "expected command name to be 'priorityclass'")
}

func TestNewHelmReleaseCmd(t *testing.T) {
	t.Parallel()

	rt := runtime.NewRuntime()
	cmd := gen.NewHelmReleaseCmd(rt)

	require.NotNil(t, cmd, "expected helmrelease command to be created")
	require.Equal(t, "helmrelease", cmd.Name(), "expected command name to be 'helmrelease'")
}
