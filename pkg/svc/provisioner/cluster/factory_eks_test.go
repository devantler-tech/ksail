package clusterprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eksTestCluster returns a cluster shaped for the EKS factory path.
func eksTestCluster() *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionEKS

	return cluster
}

func TestCreateEKSProvisionerPinsKubeconfigPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script eksctl fixture is not portable to Windows")
	}

	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "args")
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
printf '%s\n' "$@" > "$KSAIL_EKSCTL_ARGS"
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSAIL_EKSCTL_ARGS", argsPath)
	t.Setenv("KSAIL_PROFILE", "selected-profile")
	t.Setenv("KSAIL_ACCESS", "fixture-access")
	t.Setenv("KSAIL_SECRET", "fixture-secret")
	t.Setenv("KSAIL_SESSION", "fixture-session")
	t.Setenv("AWS_PROFILE", "stale-profile")
	t.Setenv("AWS_ACCESS_KEY_ID", "stale-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "stale-secret")
	t.Setenv("AWS_SESSION_TOKEN", "stale-session")

	cluster := eksTestCluster()
	cluster.Spec.Provider.AWS = v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		AccessKeyIDEnvVar:     "KSAIL_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_SECRET",
		SessionTokenEnvVar:    "KSAIL_SESSION",
	}

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{
				Name:           "test-eks",
				Region:         "eu-west-1",
				ConfigPath:     "eks.yaml",
				KubeconfigPath: "/tmp/ksail-kubeconfig",
			},
		},
	}

	provisioner, _, err := factory.Create(context.Background(), cluster)
	require.NoError(t, err)
	require.NoError(t, provisioner.Create(t.Context(), "test-eks"))

	//nolint:gosec // argsPath is created inside this test's private temporary directory
	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"create", "cluster",
		"--config-file", "eks.yaml",
		"--region", "eu-west-1",
		"--kubeconfig", "/tmp/ksail-kubeconfig",
	}, strings.Fields(string(args)))
}

// writeExecutableFixture writes a private executable used to stand in for eksctl.
func writeExecutableFixture(t *testing.T, path, contents string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(
		t,
		//nolint:gosec // owner execute is required for the fixture.
		os.Chmod(path, 0o700),
	)
}

// TestCreateEKSProvisionerWithConfig asserts a populated EKSConfig yields an EKS
// provisioner. The eksctl client shells out to the binary only at call time and
// the AWS provider resolves credentials lazily, so no live AWS access is needed
// at construction.
func TestCreateEKSProvisionerWithConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{
				Name:       "test-eks",
				Region:     "eu-west-1",
				ConfigPath: "eks.yaml",
			},
		},
	}

	provisioner, config, err := factory.Create(context.Background(), eksTestCluster())
	require.NoError(t, err)
	assert.IsType(t, &eksprovisioner.Provisioner{}, provisioner)

	eksConfig, isEKSConfig := config.(*clusterprovisioner.EKSConfig)
	require.True(t, isEKSConfig)
	assert.Equal(t, "test-eks", eksConfig.GetClusterName())
}

// TestCreateEKSProvisionerWithoutConfig asserts that selecting the EKS
// distribution without a pre-loaded EKSConfig surfaces
// ErrMissingDistributionConfig rather than a nil-pointer panic.
func TestCreateEKSProvisionerWithoutConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{},
	}

	provisioner, _, err := factory.Create(context.Background(), eksTestCluster())
	require.ErrorIs(t, err, clusterprovisioner.ErrMissingDistributionConfig)
	assert.Nil(t, provisioner)
}
