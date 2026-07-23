package cluster_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
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
[ -z "${AWS_PROFILE+x}" ] || exit 41
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
  if [ "${KSAIL_EKSCTL_CLUSTER_ABSENT-}" = "1" ]; then
    printf '[]\n'
    exit 0
  fi
  discovered_cluster="${KSAIL_EKS_DISCOVERED_CLUSTER:-$KSAIL_EKS_CLUSTER}"
  if [ "${KSAIL_EKS_DISCOVERED_REGION+x}" = "x" ]; then
    discovered_region="$KSAIL_EKS_DISCOVERED_REGION"
  else
    discovered_region="ap-southeast-2"
  fi
  eksctl_created="${KSAIL_EKSCTL_CREATED-True}"
  printf '[{"Name":"%s","Region":"%s",' "$discovered_cluster" "$discovered_region"
  printf '"EksctlCreated":"%s"}]\n' "$eksctl_created"
elif [ "$1 $2" = "get nodegroup" ]; then
	current_desired="${KSAIL_EKS_NODEGROUP_DESIRED:-0}"
	current_min="${KSAIL_EKS_NODEGROUP_MIN:-2}"
	if grep -q -- '--nodes 2 --nodes-min 2' "$KSAIL_EKSCTL_MARKER"; then
		current_desired=2
		current_min=2
	fi
	printf '[{"Cluster":"%s","Name":"workers","Status":"ACTIVE",' "$KSAIL_EKS_CLUSTER"
	printf '"DesiredCapacity":%s,"MinSize":%s,"MaxSize":4,"NodeGroupType":"managed"}]\n' \
		"$current_desired" "$current_min"
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
					fmt.Sprintf(
						"get cluster --name %s --output json --region ap-southeast-2",
						clusterName,
					),
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
						"get cluster --name %s --output json --region ap-southeast-2",
						clusterName,
					),
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
						"get cluster --name %s --output json --region ap-southeast-2",
						clusterName,
					),
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
func TestStandaloneEKSLifecycleCommandsRouteToEksctl(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-routing-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			configureStandaloneEKSNodegroupAction(t, testCase.name)

			if testCase.name == "start" {
				// EksctlCreated is normalized explicitly: case and surrounding whitespace are benign.
				t.Setenv("KSAIL_EKSCTL_CREATED", " true ")
			}

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

// TestStandaloneEKSStopStartRestoresExactCapacity exercises the user-facing commands across the
// persisted boundary and proves start verifies the restored tuple before clearing its snapshot.
func TestStandaloneEKSStopStartRestoresExactCapacity(t *testing.T) {
	const clusterName = "ksail-eks-stop-start-roundtrip-6087"

	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_EKS_NODEGROUP_DESIRED", "2")
	t.Setenv("KSAIL_EKS_NODEGROUP_MIN", "2")
	runStandaloneEKSCommand(t, cluster.NewStopCmd, "--name", clusterName, "--provider", "AWS")

	t.Setenv("KSAIL_EKS_NODEGROUP_DESIRED", "0")
	t.Setenv("KSAIL_EKS_NODEGROUP_MIN", "0")
	runStandaloneEKSCommand(t, cluster.NewStartCmd, "--name", clusterName, "--provider", "AWS")

	assert.Equal(t, []string{
		fmt.Sprintf("get cluster --name %s --output json --region ap-southeast-2", clusterName),
		fmt.Sprintf(
			"get nodegroup --cluster %s --output json --region ap-southeast-2",
			clusterName,
		),
		fmt.Sprintf(
			"scale nodegroup --cluster %s --name workers "+
				"--nodes 0 --nodes-min 0 --nodes-max 4 --region ap-southeast-2",
			clusterName,
		),
		fmt.Sprintf("get cluster --name %s --output json --region ap-southeast-2", clusterName),
		fmt.Sprintf(
			"get nodegroup --cluster %s --output json --region ap-southeast-2",
			clusterName,
		),
		fmt.Sprintf(
			"scale nodegroup --cluster %s --name workers "+
				"--nodes 2 --nodes-min 2 --nodes-max 4 --region ap-southeast-2",
			clusterName,
		),
		fmt.Sprintf(
			"get nodegroup --cluster %s --output json --region ap-southeast-2",
			clusterName,
		),
	}, readStandaloneEKSCalls(t, markerPath))

	_, err := state.LoadEKSNodegroupState(clusterName, "ap-southeast-2")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)
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
			configureStandaloneEKSNodegroupAction(t, testCase.name)
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

// TestStandaloneEKSLifecycleCommandsRejectUnmanagedEKSContext verifies a real
// eksctl kubeconfig context is not mistaken for a same-named cluster managed by
// another provider. EKS ownership must be checked with an exact query using the
// resolved AWS credentials and region, and a target absent from that query is refused
// before delete or nodegroup scaling can mutate it.
func TestStandaloneEKSLifecycleCommandsRejectUnmanagedEKSContext(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-unmanaged-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			writeStandaloneEKSKubeconfig(t, clusterName, "ap-southeast-2")
			t.Setenv("KSAIL_EKSCTL_CLUSTER_ABSENT", "1")

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)

			calls := readStandaloneEKSCalls(t, markerPath)
			assert.Equal(
				t,
				[]string{fmt.Sprintf(
					"get cluster --name %s --output json --region ap-southeast-2",
					clusterName,
				)},
				calls,
			)

			for _, call := range calls {
				assert.NotContains(t, call, "delete cluster")
				assert.NotContains(t, call, "scale nodegroup")
			}

			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSLifecycleCommandsRejectConfigRegionForDifferentExplicitName
// verifies an explicit target cannot silently inherit the region from an
// unrelated local eks.yaml. Without an explicit region, every mutating EKS
// lifecycle command must fail before invoking eksctl.
func TestStandaloneEKSLifecycleCommandsRejectConfigRegionForDifferentExplicitName(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-region-mismatch-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			require.NoError(
				t,
				os.WriteFile(
					"eks.yaml",
					[]byte(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: different-config-cluster-6087
  region: eu-west-1
`),
					0o600,
				),
			)
			t.Setenv("KSAIL_REGION", "")

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
			assert.Contains(t, err.Error(), "does not match EKS config cluster")
			assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSLifecycleCommandsRequireLocalOwnershipEvidence verifies that an explicit AWS
// region and eksctl's generic creation marker are not enough to mutate a colleague's cluster. The
// target must also match a loaded KSail EKS config or have persisted KSail creation state.
func TestStandaloneEKSLifecycleCommandsRequireLocalOwnershipEvidence(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-no-local-owner-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			t.Setenv("HOME", t.TempDir())
			writeStandaloneEKSEksConfig(t, "unrelated-local-cluster-6087")

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
			assert.Contains(t, err.Error(), "no local KSail ownership evidence")
			assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSRejectsSameNamedNonEKSConfig verifies that another distribution's local config
// cannot authorize an AWS mutation merely because it carries the same cluster name.
func TestStandaloneEKSRejectsSameNamedNonEKSConfig(t *testing.T) {
	const clusterName = "same-name-kind-and-eks-6087"

	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("HOME", t.TempDir())
	require.NoError(
		t,
		os.WriteFile(
			"ksail.yaml",
			fmt.Appendf(nil, `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: %s
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    distributionConfig: kind.yaml
    connection:
      kubeconfig: kubeconfig
`, clusterName),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			"kind.yaml",
			fmt.Appendf(nil, `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: %s
`, clusterName),
			0o600,
		),
	)

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
	assert.Contains(t, err.Error(), "no local KSail ownership evidence")
	assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
}

// TestStandaloneEKSRejectsSyntheticNameFromNamelessConfig verifies ConfigManager's fallback
// `eks-default` name is not mistaken for a name explicitly authorized by an actual eks.yaml source.
func TestStandaloneEKSRejectsSyntheticNameFromNamelessConfig(t *testing.T) {
	markerPath := setupStandaloneEKSLifecycleFixture(t, "eks-default")
	t.Setenv("HOME", t.TempDir())
	require.NoError(
		t,
		os.WriteFile(
			"eks.yaml",
			[]byte(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  region: ap-southeast-2
`),
			0o600,
		),
	)

	cmd := cluster.NewStartCmd()
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
	assert.Contains(t, err.Error(), "no local KSail ownership evidence")
	assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
}

// TestStandaloneEKSRejectsMetadataFromWrongConfigKind verifies arbitrary YAML cannot grant local
// EKS ownership merely by carrying matching metadata. The actual distribution source must be an
// eksctl ClusterConfig before its name or region can reach a lifecycle guard.
//
//nolint:paralleltest // setup mutates process environment and the working directory
func TestStandaloneEKSRejectsMetadataFromWrongConfigKind(t *testing.T) {
	testStandaloneEKSLifecycleCommandsRejectConfig(
		t,
		"wrong-kind-provenance",
		func(clusterName string) []byte {
			return fmt.Appendf(nil, `apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  region: ap-southeast-2
`, clusterName)
		},
	)
}

// TestStandaloneEKSRejectsNonCanonicalConfigName verifies a padded raw source name cannot be
// normalized into local ownership evidence for delete, start, or stop.
//
//nolint:paralleltest // setup mutates process environment and the working directory
func TestStandaloneEKSRejectsNonCanonicalConfigName(t *testing.T) {
	testStandaloneEKSLifecycleCommandsRejectConfig(
		t,
		"noncanonical-name",
		func(clusterName string) []byte {
			return fmt.Appendf(nil, `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: "%s "
  region: ap-southeast-2
`, clusterName)
		},
	)
}

func testStandaloneEKSLifecycleCommandsRejectConfig(
	t *testing.T,
	testSuffix string,
	configContent func(string) []byte,
) {
	t.Helper()

	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-" + testSuffix + "-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			require.NoError(t, os.WriteFile("eks.yaml", configContent(clusterName), 0o600))

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
			assert.Contains(t, err.Error(), "invalid EKS config file")
			assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
		})
	}
}

// TestStandaloneEKSLifecycleCommandsIgnoreRegionFromNamelessConfig verifies a local eks.yaml may
// only contribute a region when it also names the target it describes. Persisted state authorizes
// the explicit cluster, while its exact eksctl kubeconfig context supplies that cluster's region;
// borrowing the unrelated nameless file's region would redirect the mutation.
func TestStandaloneEKSLifecycleCommandsIgnoreRegionFromNamelessConfig(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-nameless-region-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			configureStandaloneEKSNodegroupAction(t, testCase.name)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("KSAIL_REGION", "")

			require.NoError(
				t,
				os.WriteFile(
					"eks.yaml",
					[]byte(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  region: eu-west-1
`),
					0o600,
				),
			)
			writeStandaloneEKSKubeconfig(t, clusterName, "ap-southeast-2")
			require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionEKS,
				Provider:     v1alpha1.ProviderAWS,
			}))
			persistStandaloneEKSIdentity(
				t,
				clusterName,
				immutableIdentityTime(),
			)

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			require.NoError(t, cmd.Execute())
			assert.Equal(
				t,
				testCase.expectedCalls(clusterName),
				readStandaloneEKSCalls(t, markerPath),
			)
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSStartAcceptsPersistedOwnershipWithoutConfig preserves the flag-only standalone
// workflow: when no project files exist, persisted creation state is sufficient local evidence and
// the exact cloud query still corroborates the name, region, and eksctl provenance before scaling.
func TestStandaloneEKSStartAcceptsPersistedOwnershipWithoutConfig(t *testing.T) {
	const clusterName = "ksail-eks-start-state-owned-6087"

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	t.Setenv("HOME", t.TempDir())

	binDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "eksctl-calls")
	writeExecutableFixture(t, filepath.Join(binDir, "eksctl"), standaloneEKSEksctlFixture)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSAIL_EKSCTL_MARKER", markerPath)
	t.Setenv("KSAIL_EKS_CLUSTER", clusterName)
	t.Setenv("AWS_PROFILE", "selected-profile")
	t.Setenv("AWS_REGION", "ap-southeast-2")
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-secret")
	t.Setenv("AWS_SESSION_TOKEN", "fixture-session")

	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))
	configureStandaloneEKSIdentityInRegion(t, clusterName, "ap-southeast-2")

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
	assert.Equal(
		t,
		[]string{
			fmt.Sprintf(
				"get cluster --name %s --output json --region ap-southeast-2",
				clusterName,
			),
			fmt.Sprintf(
				"get nodegroup --cluster %s --output json --region ap-southeast-2",
				clusterName,
			),
			fmt.Sprintf(
				"scale nodegroup --cluster %s --name workers --nodes 2 --nodes-min 2 --nodes-max 4 --region ap-southeast-2",
				clusterName,
			),
		},
		readStandaloneEKSCalls(t, markerPath),
	)
}

// setCustomAWSMappingEnvironment points the custom KSAIL_* variables at the values the eksctl
// fixture demands, while every canonical AWS_* variable holds a decoy. A run that resolves through
// the default mapping therefore hands eksctl the ambient values and the fixture rejects it.
func setCustomAWSMappingEnvironment(t *testing.T, region string) {
	t.Helper()

	t.Setenv("KSAIL_PROFILE", "selected-profile")
	t.Setenv("KSAIL_REGION", region)
	t.Setenv("KSAIL_ACCESS", "fixture-access")
	t.Setenv("KSAIL_SECRET", "fixture-secret")
	t.Setenv("KSAIL_SESSION", "fixture-session")
	t.Setenv("AWS_PROFILE", "ambient-profile")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("AWS_SESSION_TOKEN", "ambient-session")
}

// TestStandaloneEKSStartRestoresCustomAWSMappingsFromOwnershipState verifies a no-config
// lifecycle command uses the same custom AWS environment-variable names captured at creation.
func TestStandaloneEKSStartRestoresCustomAWSMappingsFromOwnershipState(t *testing.T) {
	const (
		clusterName = "ksail-eks-start-state-custom-mappings-6227"
		region      = "ap-southeast-2"
	)

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	home := t.TempDir()
	t.Setenv("HOME", home)

	binDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "eksctl-calls")
	writeExecutableFixture(t, filepath.Join(binDir, "eksctl"), standaloneEKSEksctlFixture)

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSAIL_EKSCTL_MARKER", markerPath)
	t.Setenv("KSAIL_EKS_CLUSTER", clusterName)
	setCustomAWSMappingEnvironment(t, region)
	t.Setenv("KUBECONFIG", filepath.Join(workingDir, "kubeconfig"))

	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))
	writeStandaloneEKSKubeconfig(t, clusterName, region)
	configureStandaloneEKSIdentityInRegion(t, clusterName, region)

	ownership, err := state.LoadEKSOwnershipState(clusterName, region)
	require.NoError(t, err)

	ownership.AWSOptions = v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		RegionEnvVar:          "KSAIL_REGION",
		AccessKeyIDEnvVar:     "KSAIL_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_SECRET",
		SessionTokenEnvVar:    "KSAIL_SESSION",
	}
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, region, ownership))

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
	assert.Equal(
		t,
		standaloneEKSLifecycleCases()[1].expectedCalls(clusterName),
		readStandaloneEKSCalls(t, markerPath),
	)
	assert.Equal(t, "ambient-profile", os.Getenv("AWS_PROFILE"))
	assert.Equal(t, "us-east-1", os.Getenv("AWS_REGION"))
	assert.Equal(t, "ambient-access", os.Getenv("AWS_ACCESS_KEY_ID"))
	assert.Equal(t, "ambient-secret", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	assert.Equal(t, "ambient-session", os.Getenv("AWS_SESSION_TOKEN"))
}

// TestLegacyOwnershipRecordDefersToAuthoritativeMigrationError proves a record predating the
// awsOptions schema does not surface an opaque restore failure. The actionable error — the one
// naming the rebind command — belongs to eksidentity.NewVerifier, so the restore step must decline
// quietly rather than shadow it. The command still fails closed downstream.
func TestLegacyOwnershipRecordDefersToAuthoritativeMigrationError(t *testing.T) {
	const (
		clusterName = "legacy-defers-to-verifier-6270"
		region      = "eu-north-1"
	)

	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".ksail", "clusters", clusterName)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	legacy := fmt.Sprintf(`{
  "version": %d,
  "clusterName": %q,
  "region": %q,
  "accountId": "123456789012",
  "clusterArn": "arn:aws:eks:%s:123456789012:cluster/%s",
  "createdAt": "2026-07-18T12:00:00Z"
}`, state.EKSOwnershipStateVersion, clusterName, region, region, clusterName)

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "eks-ownership-"+region+".json"),
		[]byte(legacy),
		0o600,
	))

	resolved := &lifecycle.ResolvedClusterInfo{ClusterName: clusterName, AWSRegion: region}

	require.NoError(t, cluster.ExportRestorePersistedAWSOptions(resolved))
	assert.Empty(t, resolved.AWSOpts.AccessKeyIDEnvVar)
	assert.Empty(t, resolved.AWSOpts.RegionEnvVar)
}

func TestPersistedAWSMappingsDoNotOverrideLoadedConfigDefaults(t *testing.T) {
	t.Parallel()

	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:  "loaded-config-keeps-defaults-6270",
		ConfigSource: true,
		AWSRegion:    "eu-north-1",
	}

	require.NoError(t, cluster.ExportRestorePersistedAWSOptions(resolved))
	assert.Empty(t, resolved.AWSOpts.AccessKeyIDEnvVar)
	assert.Empty(t, resolved.AWSOpts.RegionEnvVar)
}

func TestPersistedRegionAliasSelectsStateBeforeRegionResolution(t *testing.T) {
	const (
		clusterName = "state-region-alias-6270"
		region      = "ap-southeast-2"
	)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("KSAIL_REGION", region)
	ownership := &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      region,
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:" + region + ":123456789012:cluster/" + clusterName,
		CreatedAt:   time.Now().UTC(),
		//nolint:gosec // G101: these are environment-variable names, never credential values.
		AWSOptions: v1alpha1.OptionsAWS{
			ProfileEnvVar:         "AWS_PROFILE",
			RegionEnvVar:          "KSAIL_REGION",
			AccessKeyIDEnvVar:     "KSAIL_ACCESS",
			SecretAccessKeyEnvVar: "AWS_SECRET_ACCESS_KEY",
			SessionTokenEnvVar:    "AWS_SESSION_TOKEN",
		},
	}
	require.NoError(t, state.SaveEKSOwnershipState(clusterName, region, ownership))

	resolved := &lifecycle.ResolvedClusterInfo{ClusterName: clusterName}
	require.NoError(t, cluster.ExportRestorePersistedAWSOptions(resolved))
	assert.Equal(t, region, resolved.AWSRegion)
	assert.Equal(t, "KSAIL_REGION", resolved.AWSOpts.RegionEnvVar)
	assert.Equal(t, "KSAIL_ACCESS", resolved.AWSOpts.AccessKeyIDEnvVar)
}

// TestEKSMutationCommandsRejectNameOverrideMismatch proves create and update cannot claim a name
// that eksctl will ignore because the actual create/delete source is eks.yaml.
//
//nolint:paralleltest // each case changes process environment and working directory
func TestEKSMutationCommandsRejectNameOverrideMismatch(t *testing.T) {
	testCases := []struct {
		name       string
		newCommand func() *cobra.Command
	}{
		{name: "create", newCommand: cluster.NewCreateCmd},
		{name: "update", newCommand: cluster.NewUpdateCmd},
	}

	//nolint:paralleltest // each case changes process environment and working directory
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			const sourceName = "eks-source-mutation-6087"

			markerPath := setupStandaloneEKSLifecycleFixture(t, sourceName)

			cmd := testCase.newCommand()
			cmd.SetArgs([]string{"--name", "different-eks-mutation-6087"})
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot override EKS config cluster")
			assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
		})
	}
}

// TestEKSMutationCommandsRequireNamedConfig verifies create and update reject a valid-shaped but
// nameless eks.yaml before any provisioner or exact ownership call. Eksctl consumes only that file,
// so KSail cannot safely bind creation, recreation, state, or TTL to a synthetic fallback name.
//
//nolint:paralleltest // setup mutates process environment and the working directory
func TestEKSMutationCommandsRequireNamedConfig(t *testing.T) {
	testEKSMutationCommandsRejectConfigSource(
		t,
		`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  region: ap-southeast-2
`,
		"EKS config metadata.name is required",
	)
}

// TestEKSMutationCommandsRejectNonCanonicalConfigName verifies create/update cannot normalize an
// EKS source name for ownership and state while submitting a different raw name to eksctl. That
// split is especially destructive during recreation, where the normalized old target is deleted
// before eksctl consumes the unchanged source.
//
//nolint:paralleltest // setup mutates process environment and the working directory
func TestEKSMutationCommandsRejectNonCanonicalConfigName(t *testing.T) {
	testEKSMutationCommandsRejectConfigSource(
		t,
		`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: "config-file-name "
  region: ap-southeast-2
`,
		"invalid EKS config file",
	)
}

func testEKSMutationCommandsRejectConfigSource(t *testing.T, eksConfig, wantError string) {
	t.Helper()

	testCases := []struct {
		name       string
		newCommand func() *cobra.Command
	}{
		{name: "create", newCommand: cluster.NewCreateCmd},
		{name: "update", newCommand: cluster.NewUpdateCmd},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			markerPath := setupStandaloneEKSLifecycleFixture(t, "config-file-name")
			require.NoError(t, os.WriteFile("eks.yaml", []byte(eksConfig), 0o600))

			cmd := testCase.newCommand()
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), wantError)
			assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
		})
	}
}

// TestStandaloneEKSLifecycleCommandsUseEKSConfigNameWhenMetadataDrifts verifies the actual source
// eks.yaml name wins over top-level metadata when no --name flag is given, even when region comes
// from its configured environment variable.
//
//nolint:paralleltest // each case changes process environment and working directory
func TestStandaloneEKSLifecycleCommandsUseEKSConfigNameWhenMetadataDrifts(t *testing.T) {
	//nolint:paralleltest // each case changes process environment and working directory
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "eks-source-name-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			configureStandaloneEKSNodegroupAction(t, testCase.name)
			require.NoError(
				t,
				os.WriteFile(
					"eks.yaml",
					[]byte(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: eks-source-name-6087
  region: eu-west-1
`),
					0o600,
				),
			)

			cmd := testCase.newCommand()
			cmd.SetArgs(testCase.extraArgs)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			require.NoError(t, cmd.Execute())
			assert.Equal(
				t,
				testCase.expectedCalls(clusterName),
				readStandaloneEKSCalls(t, markerPath),
			)
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSLifecycleCommandsRejectMalformedPresentConfig verifies a present config load
// error cannot discard custom AWS aliases and fall back to ambient credentials for a destructive
// command. A genuinely absent config remains supported by the standalone flag-only path.
//
//nolint:paralleltest // setup changes process environment and working directory
func TestStandaloneEKSLifecycleCommandsRejectMalformedPresentConfig(t *testing.T) {
	//nolint:paralleltest // each case changes process environment and working directory
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-malformed-config-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			require.NoError(t, os.WriteFile("ksail.yaml", []byte("spec: [\n"), 0o600))

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
			assert.Contains(t, err.Error(), "load cluster config")
			assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestEKSUpdateRoutesThroughExactAWSOwnershipContext exercises the real update command through its
// pre-mutation guard. It proves the loaded EKS config, credential aliases, and region reach the AWS
// branch instead of the legacy generic discovery path.
func TestEKSUpdateRoutesThroughExactAWSOwnershipContext(t *testing.T) {
	const clusterName = "source-config-eks-update-6087"

	setupStandaloneEKSLifecycleFixture(t, "config-file-name")
	t.Setenv("KSAIL_REGION", "ap-southeast-2")
	require.NoError(
		t,
		os.WriteFile(
			"eks.yaml",
			[]byte(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: source-config-eks-update-6087
  region: eu-west-1
`),
			0o600,
		),
	)

	var captured *lifecycle.ResolvedClusterInfo

	restore := cluster.ExportSetUpdateUnmanagedGuard(
		func(_ context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
			captured = resolved

			return cluster.ErrUnmanagedCluster
		},
	)
	defer restore()

	cmd := cluster.NewUpdateCmd()
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
	require.NotNil(t, captured)
	assert.Equal(t, clusterName, captured.ClusterName)
	assert.Equal(t, clusterName, captured.ConfigClusterName)
	assert.Equal(t, v1alpha1.ProviderAWS, captured.Provider)
	assert.Equal(t, "ap-southeast-2", captured.AWSRegion)
	assert.Equal(t, "KSAIL_PROFILE", captured.AWSOpts.ProfileEnvVar)
	assert.Equal(t, "KSAIL_ACCESS", captured.AWSOpts.AccessKeyIDEnvVar)
	assert.Equal(t, "KSAIL_SECRET", captured.AWSOpts.SecretAccessKeyEnvVar)
	assert.Equal(t, "KSAIL_SESSION", captured.AWSOpts.SessionTokenEnvVar)

	expectedKubeconfig, pathErr := fsutil.EvalCanonicalPath("kubeconfig")
	require.NoError(t, pathErr)
	assert.Equal(t, expectedKubeconfig, captured.KubeconfigPath)
}

// TestAutoDeleteEKSUsesCreatedTargetAndCreationRegion verifies TTL cleanup deletes the target
// actually created from eks.yaml, retains its creation-time region even if the environment changes,
// and cleans state for that exact target only.
func TestAutoDeleteEKSUsesCreatedTargetAndCreationRegion(t *testing.T) {
	const (
		actualName = "ttl-actual-eks-6087"
		staleAlias = "ttl-stale-alias-6087"
	)

	markerPath := setupStandaloneEKSLifecycleFixture(t, actualName)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KSAIL_REGION", "us-east-1")
	configureStandaloneEKSIdentityInRegion(t, actualName, "ap-southeast-2")
	require.NoError(t, state.SaveClusterSpec(staleAlias, &v1alpha1.ClusterSpec{}))

	clusterCfg := standaloneEKSTTLClusterConfig()
	eksConfig := &clusterprovisioner.EKSConfig{
		Name:           actualName,
		NameFromConfig: true,
		Region:         "ap-southeast-2",
		ConfigPath:     "eks.yaml",
		KubeconfigPath: "kubeconfig",
	}
	cmd := &cobra.Command{Use: "ttl-delete"}

	var output bytes.Buffer

	cmd.SetContext(t.Context())
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	require.NoError(
		t,
		cluster.ExportAutoDeleteCluster(cmd, staleAlias, clusterCfg, eksConfig),
	)
	assert.Equal(
		t,
		[]string{
			fmt.Sprintf(
				"get cluster --name %s --output json --region ap-southeast-2",
				actualName,
			),
			fmt.Sprintf(
				"delete cluster --name %s --region ap-southeast-2 --wait",
				actualName,
			),
		},
		readStandaloneEKSCalls(t, markerPath),
	)
	_, err := state.LoadClusterSpec(actualName)
	require.ErrorIs(t, err, state.ErrStateNotFound)
	_, err = state.LoadClusterSpec(staleAlias)
	require.NoError(t, err)
	assert.Contains(t, output.String(), actualName)
	assert.NotContains(t, output.String(), staleAlias)
	assertParentAWSEnvironmentUnchanged(t)
}

// TestDeleteResolvedEKSStateFailsClosedWithoutRegion proves ordinary deletion cannot erase
// every same-named EKS target's state when exact-region resolution is unavailable.
func TestDeleteResolvedEKSStateFailsClosedWithoutRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "delete-missing-region-eks-6231"
	persistEKSRegionComponentStates(t, clusterName)

	err := cluster.ExportDeleteResolvedClusterState(&lifecycle.ResolvedClusterInfo{
		ClusterName: clusterName,
		Provider:    v1alpha1.ProviderAWS,
	})
	require.ErrorIs(t, err, state.ErrInvalidRegion)
	assertEKSRegionComponentStatesRemain(t, clusterName)
}

// TestDeleteTTLEKSStateFailsClosedWithoutRegion proves TTL cleanup cannot erase every same-named
// EKS target's state when its exact-region configuration is absent or incomplete.
func TestDeleteTTLEKSStateFailsClosedWithoutRegion(t *testing.T) {
	testCases := map[string]*clusterprovisioner.EKSConfig{
		"missing config": nil,
		"blank region":   {},
	}

	for name, eksConfig := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			const clusterName = "ttl-missing-region-eks-6231"
			persistEKSRegionComponentStates(t, clusterName)

			err := cluster.ExportDeleteTTLClusterState(
				clusterName,
				standaloneEKSTTLClusterConfig(),
				eksConfig,
			)
			require.ErrorIs(t, err, state.ErrInvalidRegion)
			assertEKSRegionComponentStatesRemain(t, clusterName)
		})
	}
}

func persistEKSRegionComponentStates(t *testing.T, clusterName string) {
	t.Helper()

	for _, region := range []string{"eu-north-1", "us-east-1"} {
		snapshot := &state.EKSComponentState{
			Version:     state.EKSComponentStateVersion,
			ClusterName: clusterName,
			Region:      region,
		}
		require.NoError(t, state.SaveEKSComponentState(clusterName, region, snapshot))
	}
}

func assertEKSRegionComponentStatesRemain(t *testing.T, clusterName string) {
	t.Helper()

	for _, region := range []string{"eu-north-1", "us-east-1"} {
		_, err := state.LoadEKSComponentState(clusterName, region)
		require.NoError(t, err, "state for region %s must remain", region)
	}
}

// TestAutoDeleteEKSFailsClosedWithoutOwnership verifies TTL never constructs a destructive
// provisioner when ownership provenance is absent or the actual EKS target is unavailable.
//
//nolint:paralleltest // subtests mutate process environment and working directory
func TestAutoDeleteEKSFailsClosedWithoutOwnership(t *testing.T) {
	//nolint:paralleltest // subtest mutates process environment and working directory
	t.Run("unmanaged provenance", testAutoDeleteEKSRejectsUnmanagedProvenance)
	//nolint:paralleltest // subtest mutates process environment and working directory
	t.Run("missing EKS config", testAutoDeleteEKSRejectsMissingConfig)
}

func testAutoDeleteEKSRejectsUnmanagedProvenance(t *testing.T) {
	const actualName = "ttl-unmanaged-eks-6087"

	markerPath := setupStandaloneEKSLifecycleFixture(t, actualName)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KSAIL_EKSCTL_CREATED", "False")
	require.NoError(t, state.SaveClusterSpec(actualName, &v1alpha1.ClusterSpec{}))

	cmd := &cobra.Command{Use: "ttl-delete"}

	var output bytes.Buffer

	cmd.SetContext(t.Context())
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cluster.ExportAutoDeleteCluster(
		cmd,
		"stale-alias",
		standaloneEKSTTLClusterConfig(),
		&clusterprovisioner.EKSConfig{
			Name:           actualName,
			NameFromConfig: true,
			Region:         "ap-southeast-2",
			ConfigPath:     "eks.yaml",
			KubeconfigPath: "kubeconfig",
		},
	)
	require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
	assert.Equal(
		t,
		[]string{fmt.Sprintf(
			"get cluster --name %s --output json --region ap-southeast-2",
			actualName,
		)},
		readStandaloneEKSCalls(t, markerPath),
	)

	_, stateErr := state.LoadClusterSpec(actualName)
	require.NoError(t, stateErr)
	assert.NotContains(t, output.String(), "auto-destroyed")
}

func testAutoDeleteEKSRejectsMissingConfig(t *testing.T) {
	const actualName = "ttl-missing-config-eks-6087"

	markerPath := setupStandaloneEKSLifecycleFixture(t, actualName)
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, state.SaveClusterSpec(actualName, &v1alpha1.ClusterSpec{}))

	cmd := &cobra.Command{Use: "ttl-delete"}

	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cluster.ExportAutoDeleteCluster(
		cmd,
		actualName,
		standaloneEKSTTLClusterConfig(),
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EKS configuration")
	assert.Empty(t, readStandaloneEKSCalls(t, markerPath))

	_, stateErr := state.LoadClusterSpec(actualName)
	require.NoError(t, stateErr)
}

func standaloneEKSTTLClusterConfig() *v1alpha1.Cluster {
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Cluster.Connection.Kubeconfig = "kubeconfig"
	clusterCfg.Spec.Provider.AWS = v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		RegionEnvVar:          "KSAIL_REGION",
		AccessKeyIDEnvVar:     "KSAIL_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_SECRET",
		SessionTokenEnvVar:    "KSAIL_SESSION",
	}

	return clusterCfg
}

// TestStandaloneEKSLifecycleCommandsFailClosedWhenAWSOwnershipCheckFails
// verifies a failed exact AWS ownership query cannot fall through to a mutating eksctl
// command. The ownership lookup itself is read-only and uses the same resolved
// credential aliases and region as the later lifecycle operation.
func TestStandaloneEKSLifecycleCommandsFailClosedWhenAWSOwnershipCheckFails(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-ownership-error-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			t.Setenv("KSAIL_EKSCTL_FAIL", "get")

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
			assert.Contains(t, err.Error(), "verify AWS cluster ownership")
			assert.Equal(
				t,
				[]string{fmt.Sprintf(
					"get cluster --name %s --output json --region ap-southeast-2",
					clusterName,
				)},
				readStandaloneEKSCalls(t, markerPath),
			)
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

// TestStandaloneEKSStartRejectsOwnershipRegionMismatch verifies an exact AWS
// lookup that returns the requested name in a different region is a hard stop;
// the command must not continue to nodegroup discovery or scaling.
func TestStandaloneEKSStartRejectsOwnershipRegionMismatch(t *testing.T) {
	clusterName := "ksail-eks-start-region-query-mismatch-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_EKS_DISCOVERED_REGION", "us-east-1")

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		`exact AWS ownership query returned a different region: got "us-east-1", want "ap-southeast-2"`,
	)
	assert.Equal(
		t,
		[]string{fmt.Sprintf(
			"get cluster --name %s --output json --region ap-southeast-2",
			clusterName,
		)},
		readStandaloneEKSCalls(t, markerPath),
	)
	assertParentAWSEnvironmentUnchanged(t)
}

func TestStandaloneEKSStartRejectsOwnershipNameMismatch(t *testing.T) {
	clusterName := "ksail-eks-start-name-query-mismatch-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_EKS_DISCOVERED_CLUSTER", "different-visible-cluster")

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		`exact AWS ownership query returned a different cluster: got "different-visible-cluster"`,
	)
	assert.Equal(
		t,
		[]string{fmt.Sprintf(
			"get cluster --name %s --output json --region ap-southeast-2",
			clusterName,
		)},
		readStandaloneEKSCalls(t, markerPath),
	)
	assertParentAWSEnvironmentUnchanged(t)
}

func TestStandaloneEKSStartUsesUniqueBareContextRegionFallback(t *testing.T) {
	clusterName := "ksail-eks-start-context-region-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_REGION", "")
	t.Setenv("KSAIL_EKS_DISCOVERED_REGION", "us-west-2")
	writeStandaloneEKSEksConfig(t, clusterName)
	writeStandaloneEKSKubeconfigContexts(
		t,
		clusterName,
		[]string{clusterName + ".us-west-2.eksctl.io"},
	)
	configureStandaloneEKSIdentityInRegion(t, clusterName, "us-west-2")

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
	assert.Equal(
		t,
		[]string{
			fmt.Sprintf(
				"get cluster --name %s --output json --region us-west-2",
				clusterName,
			),
			fmt.Sprintf(
				"get nodegroup --cluster %s --output json --region us-west-2",
				clusterName,
			),
			fmt.Sprintf(
				"scale nodegroup --cluster %s --name workers --nodes 2 --nodes-min 2 --nodes-max 4 --region us-west-2",
				clusterName,
			),
		},
		readStandaloneEKSCalls(t, markerPath),
	)
	assertParentAWSEnvironmentUnchanged(t)
}

func TestStandaloneEKSStartRejectsAmbiguousContextRegions(t *testing.T) {
	clusterName := "ksail-eks-start-ambiguous-context-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_REGION", "")
	writeStandaloneEKSEksConfig(t, clusterName)
	writeStandaloneEKSKubeconfigContexts(
		t,
		clusterName,
		[]string{
			"arn:aws:iam::123456789012:role/first@" + clusterName + ".us-east-1.eksctl.io",
			clusterName + ".us-west-2.eksctl.io",
		},
	)

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple kubeconfig regions")
	assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
	assertParentAWSEnvironmentUnchanged(t)
}

func TestStandaloneEKSStartBindsRegionFromExactQueryWithoutContext(t *testing.T) {
	clusterName := "ksail-eks-start-query-region-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_REGION", "")
	t.Setenv("KSAIL_EKS_DISCOVERED_REGION", "eu-north-1")
	writeStandaloneEKSEksConfig(t, clusterName)
	configureStandaloneEKSIdentityInRegion(t, clusterName, "eu-north-1")

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
	assert.Equal(
		t,
		[]string{
			fmt.Sprintf("get cluster --name %s --output json", clusterName),
			fmt.Sprintf(
				"get nodegroup --cluster %s --output json --region eu-north-1",
				clusterName,
			),
			fmt.Sprintf(
				"scale nodegroup --cluster %s --name workers --nodes 2 --nodes-min 2 --nodes-max 4 --region eu-north-1",
				clusterName,
			),
		},
		readStandaloneEKSCalls(t, markerPath),
	)
	assertParentAWSEnvironmentUnchanged(t)
}

func TestStandaloneEKSStartRejectsEmptyRegionFromExactQuery(t *testing.T) {
	clusterName := "ksail-eks-start-empty-query-region-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	t.Setenv("KSAIL_REGION", "")
	t.Setenv("KSAIL_EKS_DISCOVERED_REGION", "")
	writeStandaloneEKSEksConfig(t, clusterName)

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not report a region")
	assert.Equal(
		t,
		[]string{fmt.Sprintf("get cluster --name %s --output json", clusterName)},
		readStandaloneEKSCalls(t, markerPath),
	)
	assertParentAWSEnvironmentUnchanged(t)
}

//nolint:paralleltest // setup mutates process environment and the working directory
func TestStandaloneEKSExplicitRegionWinsContextFallback(t *testing.T) {
	clusterName := "ksail-eks-start-explicit-region-test-6087"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	writeStandaloneEKSKubeconfigContexts(
		t,
		clusterName,
		[]string{clusterName + ".us-west-2.eksctl.io"},
	)

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	for _, call := range readStandaloneEKSCalls(t, markerPath) {
		assert.Contains(t, call, "--region ap-southeast-2")
		assert.NotContains(t, call, "us-west-2")
	}

	assertParentAWSEnvironmentUnchanged(t)
}

func TestEksctlContextTargetParsing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		contextName string
		wantName    string
		wantRegion  string
		wantOK      bool
	}{
		{
			name:        "role ARN identity",
			contextName: "arn:aws:iam::123456789012:role/operator@demo.eu-west-1.eksctl.io",
			wantName:    "demo",
			wantRegion:  "eu-west-1",
			wantOK:      true,
		},
		{
			name:        "identity containing at signs",
			contextName: "user@example.com@demo.us-east-2.eksctl.io",
			wantName:    "demo",
			wantRegion:  "us-east-2",
			wantOK:      true,
		},
		{
			name:        "bare eksctl context",
			contextName: "demo.ap-southeast-1.eksctl.io",
			wantName:    "demo",
			wantRegion:  "ap-southeast-1",
			wantOK:      true,
		},
		{
			name:        "similar cluster name remains exact",
			contextName: "demo-extra.ap-southeast-1.eksctl.io",
			wantName:    "demo-extra",
			wantRegion:  "ap-southeast-1",
			wantOK:      true,
		},
		{name: "missing suffix", contextName: "demo.eu-west-1", wantOK: false},
		{name: "missing region", contextName: "demo.eksctl.io", wantOK: false},
		{name: "malformed dotted region", contextName: "demo.us.east.eksctl.io", wantOK: false},
		{name: "empty target", contextName: ".eksctl.io", wantOK: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			name, region, ok := cluster.ExportParseEksctlContextTarget(testCase.contextName)
			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.wantName, name)
			assert.Equal(t, testCase.wantRegion, region)
		})
	}
}

// TestStandaloneEKSStartRejectsMissingEksctlOwnership verifies the exact AWS
// target is still unmanaged unless eksctl explicitly reports that it created
// the cluster. False, empty, and unknown provenance all stop before scaling.
func TestStandaloneEKSStartRejectsMissingEksctlOwnership(t *testing.T) {
	testCases := []struct {
		name    string
		created string
	}{
		{name: "false", created: "False"},
		{name: "empty", created: ""},
		{name: "unknown", created: "unknown"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-start-" + testCase.name + "-ownership-test-6087"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			t.Setenv("KSAIL_EKSCTL_CREATED", testCase.created)

			cmd := cluster.NewStartCmd()
			cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, cluster.ErrUnmanagedCluster)
			assert.Contains(t, err.Error(), "did not report an eksctl-created cluster")
			assert.Equal(
				t,
				[]string{fmt.Sprintf(
					"get cluster --name %s --output json --region ap-southeast-2",
					clusterName,
				)},
				readStandaloneEKSCalls(t, markerPath),
			)
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
	eksConfig := strings.ReplaceAll(standaloneEKSEksConfigFixture, "config-file-name", clusterName)
	require.NoError(t, os.WriteFile(eksConfigPath, []byte(eksConfig), 0o600))
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
	t.Setenv("HOME", t.TempDir())
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

	persistStandaloneEKSIdentity(t, clusterName, immutableIdentityTime())
	setEKSIdentityClient(t, &fakeEKSIdentityClient{
		accountID: "123456789012",
		cluster: immutableEKSCluster(
			clusterName,
			immutableIdentityTime(),
		),
	})

	return markerPath
}

func configureStandaloneEKSNodegroupAction(t *testing.T, action string) {
	t.Helper()

	if action == "stop" {
		t.Setenv("KSAIL_EKS_NODEGROUP_DESIRED", "2")
		t.Setenv("KSAIL_EKS_NODEGROUP_MIN", "2")
	}
}

func runStandaloneEKSCommand(
	t *testing.T,
	newCommand func() *cobra.Command,
	args ...string,
) {
	t.Helper()

	cmd := newCommand()
	cmd.SetArgs(args)
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())
}

func assertParentAWSEnvironmentUnchanged(t *testing.T) {
	t.Helper()

	assert.Equal(t, "stale-profile", os.Getenv("AWS_PROFILE"))
	assert.Equal(t, "stale-region", os.Getenv("AWS_REGION"))
	assert.Equal(t, "stale-access", os.Getenv("AWS_ACCESS_KEY_ID"))
	assert.Equal(t, "stale-secret", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	assert.Equal(t, "stale-session", os.Getenv("AWS_SESSION_TOKEN"))
}

func writeStandaloneEKSKubeconfig(t *testing.T, clusterName, region string) {
	t.Helper()

	contextName := fmt.Sprintf(
		"arn:aws:iam::123456789012:role/ksail-test@%s.%s.eksctl.io",
		clusterName,
		region,
	)
	writeStandaloneEKSKubeconfigContexts(t, clusterName, []string{contextName})
}

func writeStandaloneEKSKubeconfigContexts(
	t *testing.T,
	clusterName string,
	contextNames []string,
) {
	t.Helper()

	var contexts strings.Builder
	for _, contextName := range contextNames {
		fmt.Fprintf(
			&contexts,
			"- name: %s\n  context:\n    cluster: %s\n    user: eksctl-user\n",
			contextName,
			clusterName,
		)
	}

	currentContext := ""
	if len(contextNames) > 0 {
		currentContext = contextNames[0]
	}

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: https://example.invalid
contexts:
%s
current-context: %s
users:
- name: eksctl-user
  user: {}
`, clusterName, contexts.String(), currentContext)

	require.NoError(t, os.WriteFile("kubeconfig", []byte(kubeconfig), 0o600))
}

func writeStandaloneEKSEksConfig(t *testing.T, clusterName string) {
	t.Helper()

	config := fmt.Sprintf(`apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: %s
`, clusterName)

	require.NoError(t, os.WriteFile("eks.yaml", []byte(config), 0o600))
}

func readStandaloneEKSCalls(t *testing.T, markerPath string) []string {
	t.Helper()

	calls, err := os.ReadFile(markerPath) //nolint:gosec // test-private path
	if os.IsNotExist(err) {
		return nil
	}

	require.NoError(t, err)

	trimmed := strings.TrimSpace(string(calls))
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}
