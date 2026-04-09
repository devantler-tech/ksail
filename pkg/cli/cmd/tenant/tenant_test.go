package tenant_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/cli/annotations"
	tenantpkg "github.com/devantler-tech/ksail/v6/pkg/cli/cmd/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestTenantCmd_ShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewTenantCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	require.Contains(t, output, "Manage multi-tenancy onboarding")
	require.Contains(t, output, "create")

	snaps.MatchSnapshot(t, output)
}

func TestTenantCmd_HasConsolidateAnnotation(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewTenantCmd(nil)
	require.Equal(t, "tenant_command", cmd.Annotations[annotations.AnnotationConsolidate])
}

func TestTenantCmd_RejectsArgs(t *testing.T) {
	t.Parallel()

	cmd := tenantpkg.NewTenantCmd(nil)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unexpected-arg"})

	err := cmd.Execute()
	require.Error(t, err)
}
