package clusterdiscovery_test

import (
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type namedAWSResolver struct {
	values map[credentials.Key]string
	names  map[credentials.Key]string
}

func (r namedAWSResolver) Value(key credentials.Key) string { return r.values[key] }

func (r namedAWSResolver) EnvVar(key credentials.Key) string {
	if name := r.names[key]; name != "" {
		return name
	}

	return credentials.DefaultEnvVar(key)
}

func TestDiscoverAWS_UsesCanonicalIsolatedChildEnvironment(t *testing.T) {
	// Not parallel: the real ExecRunner resolves the fixture from PATH.
	binDir := t.TempDir()
	eksctlPath := filepath.Join(binDir, "eksctl")
	writeExecutableFixture(t, eksctlPath, `#!/bin/sh
[ "${AWS_PROFILE-}" = "selected-profile" ] || exit 41
[ "${AWS_ACCESS_KEY_ID-}" = "fixture-access" ] || exit 42
[ "${AWS_SECRET_ACCESS_KEY-}" = "fixture-secret" ] || exit 43
[ "${AWS_SESSION_TOKEN-}" = "fixture-session" ] || exit 44
[ -z "${KSAIL_PROFILE+x}" ] || exit 45
[ -z "${KSAIL_ACCESS+x}" ] || exit 46
[ -z "${KSAIL_SECRET+x}" ] || exit 47
[ -z "${KSAIL_SESSION+x}" ] || exit 48
printf '[{"Name":"mapped-eks","Region":"eu-west-1","EksctlCreated":"True"}]\n'
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSAIL_PROFILE", "parent-custom-profile")
	t.Setenv("KSAIL_ACCESS", "parent-custom-access")
	t.Setenv("KSAIL_SECRET", "parent-custom-secret")
	t.Setenv("KSAIL_SESSION", "parent-custom-session")
	t.Setenv("AWS_PROFILE", "parent-stale-profile")
	t.Setenv("AWS_ACCESS_KEY_ID", "parent-stale-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "parent-stale-secret")
	t.Setenv("AWS_SESSION_TOKEN", "parent-stale-session")

	discoverer := &clusterdiscovery.Discoverer{
		Resolver: namedAWSResolver{
			values: map[credentials.Key]string{
				credentials.AWSProfile:         "selected-profile",
				credentials.AWSAccessKeyID:     "fixture-access",
				credentials.AWSSecretAccessKey: "fixture-secret",
				credentials.AWSSessionToken:    "fixture-session",
			},
			names: map[credentials.Key]string{
				credentials.AWSProfile:         "KSAIL_PROFILE",
				credentials.AWSAccessKeyID:     "KSAIL_ACCESS",
				credentials.AWSSecretAccessKey: "KSAIL_SECRET",
				credentials.AWSSessionToken:    "KSAIL_SESSION",
			},
		},
		LookPath: func(string) (string, error) { return eksctlPath, nil },
	}

	clusters, failures := discoverer.Discover(
		t.Context(),
		[]v1alpha1.Provider{v1alpha1.ProviderAWS},
	)

	require.Empty(t, failures)
	require.Len(t, clusters, 1)
	assert.Equal(t, "mapped-eks", clusters[0].Name)
	assert.Equal(t, "parent-stale-profile", os.Getenv("AWS_PROFILE"))
	assert.Equal(t, "parent-custom-profile", os.Getenv("KSAIL_PROFILE"))
}

func writeExecutableFixture(t *testing.T, path, contents string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(
		t,
		//nolint:gosec // owner execute is required for the fixture.
		os.Chmod(path, 0o700),
	)
}
