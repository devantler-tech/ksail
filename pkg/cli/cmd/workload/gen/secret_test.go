package gen_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

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
