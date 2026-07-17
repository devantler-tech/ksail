package cluster_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const standaloneEKSClusterFixture = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: config-file-name
spec:
  cluster:
    distribution: EKS
    provider: AWS
    distributionConfig: eks.yaml
    connection:
      kubeconfig: kubeconfig
  provider:
    aws:
      profileEnvVar: KSAIL_PROFILE
      regionEnvVar: KSAIL_REGION
      accessKeyIdEnvVar: KSAIL_ACCESS
      secretAccessKeyEnvVar: KSAIL_SECRET
      sessionTokenEnvVar: KSAIL_SESSION
`

const standaloneEKSEksConfigFixture = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: config-file-name
  region: eu-west-1
`

const standaloneEKSEksctlFixture = `#!/bin/sh
[ "${AWS_PROFILE-}" = "selected-profile" ] || exit 41
[ "${AWS_ACCESS_KEY_ID-}" = "fixture-access" ] || exit 42
[ "${AWS_SECRET_ACCESS_KEY-}" = "fixture-secret" ] || exit 43
[ "${AWS_SESSION_TOKEN-}" = "fixture-session" ] || exit 44
[ -z "${KSAIL_PROFILE+x}" ] || exit 45
[ -z "${KSAIL_ACCESS+x}" ] || exit 46
[ -z "${KSAIL_SECRET+x}" ] || exit 47
[ -z "${KSAIL_SESSION+x}" ] || exit 48

printf '%s\n' "$*" >> "$KSAIL_EKSCTL_MARKER"

if [ "${KSAIL_EKSCTL_FAIL-}" = "$1" ]; then
  printf 'forced %s failure\n' "$1" >&2
  exit 49
fi

if [ "$1 $2" = "get cluster" ]; then
  printf '[{"Name":"%s","Region":"ap-southeast-2","EksctlCreated":"True"}]\n' "$KSAIL_EKS_CLUSTER"
elif [ "$1 $2" = "get nodegroup" ]; then
  printf '[{"Cluster":"%s","Name":"workers","Status":"ACTIVE",' "$KSAIL_EKS_CLUSTER"
  printf '"DesiredCapacity":0,"MinSize":2,"MaxSize":4,"NodeGroupType":"managed"}]\n'
fi
`

type standaloneEKSLifecycleCase struct {
	name          string
	newCommand    func() *cobra.Command
	extraArgs     []string
	expectedCalls func(clusterName string) []string
}

func standaloneEKSLifecycleCases() []standaloneEKSLifecycleCase {
	return []standaloneEKSLifecycleCase{
		{
			name:       "delete",
			newCommand: cluster.NewDeleteCmd,
			extraArgs:  []string{"--force"},
			expectedCalls: func(clusterName string) []string {
				return []string{
					"get cluster --output json --region ap-southeast-2",
					fmt.Sprintf(
						"delete cluster --name %s --region ap-southeast-2 --wait",
						clusterName,
					),
				}
			},
		},
		{
			name:       "start",
			newCommand: cluster.NewStartCmd,
			expectedCalls: func(clusterName string) []string {
				return []string{
					fmt.Sprintf(
						"get nodegroup --cluster %s --output json --region ap-southeast-2",
						clusterName,
					),
					fmt.Sprintf(
						"scale nodegroup --cluster %s --name workers --nodes 2 --nodes-min 2 --nodes-max 4 --region ap-southeast-2",
						clusterName,
					),
				}
			},
		},
		{
			name:       "stop",
			newCommand: cluster.NewStopCmd,
			expectedCalls: func(clusterName string) []string {
				return []string{
					fmt.Sprintf(
						"get nodegroup --cluster %s --output json --region ap-southeast-2",
						clusterName,
					),
					fmt.Sprintf(
						"scale nodegroup --cluster %s --name workers --nodes 0 --nodes-min 0 --nodes-max 4 --region ap-southeast-2",
						clusterName,
					),
				}
			},
		},
	}
}

// TestStandaloneEKSLifecycleCommandsRouteToEksctl proves the three standalone
// command surfaces reach the existing EKS provisioner, honor the custom AWS
// credential mapping, and prefer the configured region environment variable.
//
//nolint:paralleltest // each case changes process environment and working directory
func TestStandaloneEKSLifecycleCommandsRouteToEksctl(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		//nolint:paralleltest // setup changes process environment and working directory
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-routing-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			require.NoError(t, cmd.Execute())

			calls, err := os.ReadFile(markerPath) //nolint:gosec // test-private path
			require.NoError(t, err)
			assert.Equal(
				t,
				testCase.expectedCalls(clusterName),
				strings.Split(strings.TrimSpace(string(calls)), "\n"),
			)
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSLifecycleCommandsPropagateEksctlErrors covers each command's
// user-facing error path after routing has reached eksctl.
func TestStandaloneEKSLifecycleCommandsPropagateEksctlErrors(t *testing.T) {
	testCases := []struct {
		name       string
		newCommand func() *cobra.Command
		extraArgs  []string
		failOn     string
		wantError  string
	}{
		{
			name:       "delete",
			newCommand: cluster.NewDeleteCmd,
			extraArgs:  []string{"--force"},
			failOn:     "delete",
			wantError:  "cluster deletion failed",
		},
		{
			name:       "start",
			newCommand: cluster.NewStartCmd,
			failOn:     "scale",
			wantError:  "failed to starting cluster",
		},
		{
			name:       "stop",
			newCommand: cluster.NewStopCmd,
			failOn:     "scale",
			wantError:  "failed to stopping cluster",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-error-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			t.Setenv("KSAIL_EKSCTL_FAIL", testCase.failOn)

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantError)

			calls, readErr := os.ReadFile(markerPath) //nolint:gosec // test-private path
			require.NoError(t, readErr)
			assert.Contains(t, string(calls), testCase.failOn+" ")
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

func setupStandaloneEKSLifecycleFixture(
	t *testing.T,
	clusterName string,
) string {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)

	binDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "eksctl-calls")
	eksctlPath := filepath.Join(binDir, "eksctl")
	writeExecutableFixture(t, eksctlPath, standaloneEKSEksctlFixture)

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workingDir, "ksail.yaml"),
			[]byte(standaloneEKSClusterFixture),
			0o600,
		),
	)
	eksConfigPath := filepath.Join(workingDir, "eks.yaml")
	require.NoError(t, os.WriteFile(eksConfigPath, []byte(standaloneEKSEksConfigFixture), 0o600))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workingDir, "kubeconfig"),
			[]byte("apiVersion: v1\nkind: Config\n"),
			0o600,
		),
	)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSAIL_EKSCTL_MARKER", markerPath)
	t.Setenv("KSAIL_EKS_CLUSTER", clusterName)
	t.Setenv("KSAIL_PROFILE", "selected-profile")
	t.Setenv("KSAIL_REGION", "ap-southeast-2")
	t.Setenv("KSAIL_ACCESS", "fixture-access")
	t.Setenv("KSAIL_SECRET", "fixture-secret")
	t.Setenv("KSAIL_SESSION", "fixture-session")
	t.Setenv("AWS_PROFILE", "stale-profile")
	t.Setenv("AWS_REGION", "stale-region")
	t.Setenv("AWS_ACCESS_KEY_ID", "stale-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "stale-secret")
	t.Setenv("AWS_SESSION_TOKEN", "stale-session")

	return markerPath
}

func assertParentAWSEnvironmentUnchanged(t *testing.T) {
	t.Helper()

	assert.Equal(t, "stale-profile", os.Getenv("AWS_PROFILE"))
	assert.Equal(t, "stale-region", os.Getenv("AWS_REGION"))
	assert.Equal(t, "stale-access", os.Getenv("AWS_ACCESS_KEY_ID"))
	assert.Equal(t, "stale-secret", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	assert.Equal(t, "stale-session", os.Getenv("AWS_SESSION_TOKEN"))
}
