package cluster_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	awsprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errEksctlStubEmptyArgs is returned when the stub runner is invoked with no
// positional arguments (should never happen in real test cases).
var errEksctlStubEmptyArgs = errors.New("eksctl stub: empty args")

// stubEndpoint is the EKS control-plane endpoint the stub describer returns so
// the AWS status tests never resolve real AWS credentials.
const stubEndpoint = "https://ABCDEF.gr7.us-east-1.eks.amazonaws.com"

const mappedAWSEksctlFixture = `#!/bin/sh
[ "${AWS_PROFILE-}" = "selected-profile" ] || exit 41
[ "${AWS_ACCESS_KEY_ID-}" = "fixture-access" ] || exit 42
[ "${AWS_SECRET_ACCESS_KEY-}" = "fixture-secret" ] || exit 43
[ "${AWS_SESSION_TOKEN-}" = "fixture-session" ] || exit 44
[ -z "${KSAIL_PROFILE+x}" ] || exit 45
[ -z "${KSAIL_ACCESS+x}" ] || exit 46
[ -z "${KSAIL_SECRET+x}" ] || exit 47
[ -z "${KSAIL_SESSION+x}" ] || exit 48
printf mapped > "$KSAIL_EKSCTL_MARKER"
printf 'null\n'
`

const mappedAWSClusterFixture = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: mapped-eks
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
      accessKeyIdEnvVar: KSAIL_ACCESS
      secretAccessKeyEnvVar: KSAIL_SECRET
      sessionTokenEnvVar: KSAIL_SESSION
`

const mappedAWSEksConfigFixture = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: mapped-eks
  region: eu-west-1
`

// stubDescriber is a credential-free stand-in for the EKS DescribeCluster
// seam, returning a cluster carrying stubEndpoint.
type stubDescriber struct{}

func (stubDescriber) DescribeCluster(
	_ context.Context,
	_ string,
) (*ekstypes.Cluster, error) {
	return &ekstypes.Cluster{Endpoint: awssdk.String(stubEndpoint)}, nil
}

// eksctlStubRunner replays canned eksctl responses keyed by the first two
// positional arguments (e.g. "get cluster", "get nodegroup"), mirroring the
// scripted-runner pattern used by the aws provider tests.
type eksctlStubRunner struct {
	t         *testing.T
	responses map[string][]byte
	// gotArgs records every argument slice the runner was invoked with, so tests
	// can assert on the flags the eksctl client built (e.g. --region).
	gotArgs [][]string
}

func (s *eksctlStubRunner) Run(
	_ context.Context,
	_ string,
	args []string,
	_ io.Reader,
) ([]byte, []byte, error) {
	s.t.Helper()

	s.gotArgs = append(s.gotArgs, args)

	if len(args) == 0 {
		return nil, nil, errEksctlStubEmptyArgs
	}

	key := args[0]
	if len(args) >= 2 {
		key = args[0] + " " + args[1]
	}

	out, ok := s.responses[key]
	if !ok {
		s.t.Fatalf("eksctl stub: no response configured for %q (args=%v)", key, args)
	}

	return out, nil, nil
}

// newEksctlClient builds an eksctl client whose binary points at the test
// executable (so CheckAvailable's exec.LookPath succeeds hermetically) and
// whose runner replays the supplied canned responses instead of shelling out.
func newEksctlClient(t *testing.T, responses map[string][]byte) *eksctlclient.Client {
	t.Helper()

	return eksctlclient.NewClient(
		eksctlclient.WithBinary(os.Args[0]),
		eksctlclient.WithRunner(&eksctlStubRunner{t: t, responses: responses}),
	)
}

func TestAWSProviderStatus_AggregatesNodegroups(t *testing.T) {
	t.Parallel()

	client := newEksctlClient(t, map[string][]byte{
		"get cluster": []byte(`[{"Name":"demo","Region":"us-east-1","EksctlCreated":"True"}]`),
		"get nodegroup": []byte(
			`[{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE"},{"Cluster":"demo","Name":"ng-2","Status":"ACTIVE"}]`,
		),
	})

	status, err := cluster.ExportAWSProviderStatus(
		t.Context(), client, "demo", "",
		awsprovider.WithClusterDescriber(stubDescriber{}),
	)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 2, status.NodesReady)
	assert.True(t, status.Ready)
	assert.Equal(t, stubEndpoint, status.Endpoint)
}

func TestAWSProviderStatus_ClusterNotFound(t *testing.T) {
	t.Parallel()

	client := newEksctlClient(t, map[string][]byte{
		"get cluster": []byte("null"),
	})

	status, err := cluster.ExportAWSProviderStatus(t.Context(), client, "missing", "")
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
	assert.Nil(t, status)
}

func TestAWSProviderStatus_EksctlUnavailable(t *testing.T) {
	t.Parallel()

	// A binary that does not exist on PATH makes CheckAvailable fail, which
	// must surface as the soft errProviderNotConfigured so 'cluster info' falls
	// back to kubectl rather than erroring out.
	client := eksctlclient.NewClient(
		eksctlclient.WithBinary("ksail-nonexistent-eksctl-binary"),
	)

	status, err := cluster.ExportAWSProviderStatus(t.Context(), client, "demo", "")
	require.ErrorIs(t, err, cluster.ErrProviderNotConfigured)
	assert.Nil(t, status)
}

// TestAWSProviderStatus_ForwardsRegion asserts the resolved region is threaded
// into eksctl's calls (as --region), so 'cluster info' targets the cluster's
// configured region instead of only eksctl's AWS_REGION/profile default.
func TestAWSProviderStatus_ForwardsRegion(t *testing.T) {
	t.Parallel()

	runner := &eksctlStubRunner{
		t: t,
		responses: map[string][]byte{
			"get cluster": []byte(
				`[{"Name":"demo","Region":"eu-west-1","EksctlCreated":"True"}]`,
			),
			"get nodegroup": []byte(`[{"Cluster":"demo","Name":"ng-1","Status":"ACTIVE"}]`),
		},
	}
	client := eksctlclient.NewClient(
		eksctlclient.WithBinary(os.Args[0]),
		eksctlclient.WithRunner(runner),
	)

	status, err := cluster.ExportAWSProviderStatus(
		t.Context(), client, "demo", "eu-west-1",
		awsprovider.WithClusterDescriber(stubDescriber{}),
	)
	require.NoError(t, err)
	require.NotNil(t, status)

	var sawRegion bool

	for _, args := range runner.gotArgs {
		for i := range len(args) - 1 {
			if args[i] == "--region" && args[i+1] == "eu-west-1" {
				sawRegion = true
			}
		}
	}

	assert.True(
		t,
		sawRegion,
		"expected eksctl to be invoked with --region eu-west-1, got %v",
		runner.gotArgs,
	)
}

func TestInfoCommandMapsCustomAWSCredentialsIntoEksctl(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	binDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "mapped")
	eksctlPath := filepath.Join(binDir, "eksctl")
	writeExecutableFixture(t, eksctlPath, mappedAWSEksctlFixture)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workingDir, "ksail.yaml"),
			[]byte(mappedAWSClusterFixture),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workingDir, "eks.yaml"),
			[]byte(mappedAWSEksConfigFixture),
			0o600,
		),
	)
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
	t.Setenv("KSAIL_PROFILE", "selected-profile")
	t.Setenv("KSAIL_ACCESS", "fixture-access")
	t.Setenv("KSAIL_SECRET", "fixture-secret")
	t.Setenv("KSAIL_SESSION", "fixture-session")
	t.Setenv("AWS_PROFILE", "stale-profile")
	t.Setenv("AWS_ACCESS_KEY_ID", "stale-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "stale-secret")
	t.Setenv("AWS_SESSION_TOKEN", "stale-session")

	cmd := cluster.NewInfoCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	_ = cmd.Execute()

	marker, err := os.ReadFile(markerPath) //nolint:gosec // path is test-private.
	require.NoError(t, err)
	assert.Equal(t, "mapped", string(marker))
	assert.Equal(t, "stale-profile", os.Getenv("AWS_PROFILE"))
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
