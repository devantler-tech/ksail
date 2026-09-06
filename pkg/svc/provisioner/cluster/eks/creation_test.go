package eksprovisioner_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const creationConfig = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata: {name: stale-name, region: stale-region}
iam: {withOIDC: true}
vpc: {id: vpc-existing}
nodeGroups:
  - name: unmanaged-do-not-create
managedNodeGroups:
  - name: workers
    desiredCapacity: 1
    minSize: 1
    maxSize: 3
    instanceType: t3.medium
    privateNetworking: true
    labels: {workload: batch}
    iam: {instanceRoleARN: arn:aws:iam::123456789012:role/workers}
`

var errCreationFailed = errors.New("node-group creation failed")

type creationRunner struct {
	eksprovisioner.AWSClusterAPI

	stackExists bool
	stackErr    error
	markers     map[string]string
	t           *testing.T
	live        []eksctl.NodegroupSummary
	output      *string
	gets        int
	creates     int
	scales      int
	scaleArgs   []string
	paths       []string
	beforeGet   func(int)
	create      func(map[string]any) error
	environment []string
}

func (r *creationRunner) NodegroupStackExists(context.Context, string, string) (bool, error) {
	return r.stackExists, r.stackErr
}

func (r *creationRunner) Run(
	ctx context.Context, binary string, args []string, stdin io.Reader,
) ([]byte, []byte, error) {
	return r.RunWithEnvironment(ctx, binary, args, stdin, nil)
}

func (r *creationRunner) RunWithEnvironment(
	_ context.Context, _ string, args []string, _ io.Reader, environment []string,
) ([]byte, []byte, error) {
	r.t.Helper()
	assert.Equal(r.t, r.environment, environment, "every read and mutation uses the frozen client")

	switch args[0] {
	case "get":
		r.gets++
		if r.beforeGet != nil {
			r.beforeGet(r.gets)
		}

		if r.output != nil {
			return []byte(*r.output), nil, nil
		}

		data, err := json.Marshal(r.live)
		require.NoError(r.t, err)

		return data, nil, nil
	case "create":
		r.creates++
		require.Len(r.t, args, 4)
		assert.Equal(r.t, []string{"create", "nodegroup", "--config-file"}, args[:3])
		path := args[3]
		r.paths = append(r.paths, path)
		info, err := os.Stat(path)
		require.NoError(r.t, err)
		assert.Equal(r.t, os.FileMode(0o600), info.Mode().Perm())
		//nolint:gosec // the provisioner creates this private temporary config.
		data, err := os.ReadFile(path)
		require.NoError(r.t, err)

		var config map[string]any
		require.NoError(r.t, yaml.Unmarshal(data, &config))

		if r.create != nil {
			groups, _ := config["managedNodeGroups"].([]any)
			group, _ := groups[0].(map[string]any)
			tags, _ := group["tags"].(map[string]any)
			name, _ := group["name"].(string)
			marker, _ := tags["ksail.io/nodegroup-creation-id"].(string)
			r.markers[name] = marker

			return nil, nil, r.create(config)
		}
	case "scale":
		r.scales++
		r.scaleArgs = args
	default:
		r.t.Fatalf("unexpected eksctl command: %v", args)
	}

	return nil, nil, nil
}

func activeCreationGroup(name string) eksctl.NodegroupSummary {
	return eksctl.NodegroupSummary{
		Cluster: "ksail-test", Name: name, Status: "ACTIVE", NodeGroupType: "managed",
		DesiredCap: 1, MinSize: 1, MaxSize: 3, InstanceType: "t3.medium",
	}
}

func newCreationProvisioner(
	t *testing.T, config string, verifier eksidentity.Verifier,
) (*eksprovisioner.UpdatableProvisioner, *creationRunner, string) {
	t.Helper()

	path := writeUpdateTestConfig(t, config)
	runner := &creationRunner{
		markers: make(map[string]string),
		t:       t,
		live:    []eksctl.NodegroupSummary{},
		environment: []string{
			"AWS_ACCESS_KEY_ID=frozen-test-key", "AWS_SECRET_ACCESS_KEY=frozen-test-secret",
			"AWS_REGION=us-east-1",
		},
	}
	client := eksctl.NewClient(eksctl.WithBinary(testBinary), eksctl.WithRunner(runner),
		eksctl.WithEnvironment(runner.environment))
	base, err := eksprovisioner.NewProvisioner("ksail-test", "us-east-1", path, client, nil,
		eksprovisioner.WithOwnershipVerifier(verifier), eksprovisioner.WithAWSClusterAPI(runner))
	require.NoError(t, err)

	return eksprovisioner.NewUpdatableProvisioner(
		base,
		eksprovisioner.WithManagedNodegroupCreation(true),
	), runner, path
}

func allowCreation(context.Context) error { return nil }

func runCreationUpdate(
	t *testing.T, provisioner *eksprovisioner.UpdatableProvisioner, dryRun bool,
) (*clusterupdate.UpdateResult, error) {
	t.Helper()

	//nolint:wrapcheck // test helper preserves the production error for assertions.
	return provisioner.Update(t.Context(), "", &v1alpha1.ClusterSpec{}, &v1alpha1.ClusterSpec{},
		clusterupdate.UpdateOptions{DryRun: dryRun})
}

func TestManagedNodegroupCreationUsesPrivateVerifiedSnapshotAndRetryIsNoOp(t *testing.T) {
	t.Parallel()

	verified := 0
	provisioner, runner, source := newCreationProvisioner(
		t,
		creationConfig,
		func(context.Context) error {
			verified++

			return nil
		},
	)
	runner.create = func(config map[string]any) error {
		assert.Equal(t, 1, verified)
		assert.Equal(
			t,
			map[string]any{"name": "ksail-test", "region": "us-east-1"},
			config["metadata"],
		)
		assert.Equal(t, map[string]any{"withOIDC": true}, config["iam"])
		assert.Equal(t, map[string]any{"id": "vpc-existing"}, config["vpc"])
		assert.NotContains(t, config, "nodeGroups")
		groups, groupsOK := config["managedNodeGroups"].([]any)
		require.True(t, groupsOK)
		require.Len(t, groups, 1)
		group, groupOK := groups[0].(map[string]any)
		require.True(t, groupOK)
		assert.Equal(t, "workers", group["name"])
		assert.Equal(t, true, group["privateNetworking"])
		assert.Equal(t, map[string]any{"workload": "batch"}, group["labels"])
		assert.Equal(
			t,
			map[string]any{"instanceRoleARN": "arn:aws:iam::123456789012:role/workers"},
			group["iam"],
		)

		runner.live = append(runner.live, activeCreationGroup("workers"))

		return nil
	}

	result, err := runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "eks.managedNodeGroups[workers]", result.AppliedChanges[0].Field)
	assert.Empty(t, result.FailedChanges)
	assert.Equal(t, 3, runner.gets, "plan, refresh, then verify actual creation")
	require.Len(t, runner.paths, 1)
	assert.NoFileExists(t, runner.paths[0])
	//nolint:gosec // source is a fixture in this test's private directory.
	unchanged, err := os.ReadFile(source)
	require.NoError(t, err)
	assert.Equal(t, creationConfig, string(unchanged))

	result, err = runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	assert.Zero(t, result.TotalChanges())
	assert.Empty(t, result.AppliedChanges)
	assert.Equal(t, 1, runner.creates)
}

func TestManagedNodegroupCreationRejectsAmbiguousInventory(t *testing.T) {
	t.Parallel()

	for _, output := range []string{
		"", "null", "{", `[{"Cluster":"other","Name":"workers","Type":"managed","Status":"ACTIVE"}]`,
		`[{"Cluster":"ksail-test","Name":"workers","Type":"unmanaged"}]`,
		`[{"Cluster":"ksail-test","Name":"workers","Type":"managed","Status":"CREATING"}]`,
		`[{"Cluster":"ksail-test","Name":"workers","Type":"managed","Status":"CREATE_FAILED"}]`,
		`[{"Cluster":"ksail-test","Name":"workers","Status":"ACTIVE"}]`,
		`[{"Cluster":"ksail-test","Name":"workers","Type":"managed","Status":"ACTIVE"},` +
			`{"Cluster":"ksail-test","Name":"workers","Type":"managed","Status":"ACTIVE"}]`,
	} {
		t.Run(output, func(t *testing.T) {
			t.Parallel()
			provisioner, runner, _ := newCreationProvisioner(t, creationConfig, allowCreation)
			runner.output = &output
			_, err := runCreationUpdate(t, provisioner, false)
			require.Error(t, err)
			assert.Zero(t, runner.creates)
			assert.Zero(t, runner.scales)
		})
	}
}

func TestManagedNodegroupCreationRejectsInvalidDeclarationBeforeEksctl(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]string{
		"duplicate names": creationConfig + "  - name: workers\n",
		"duplicate YAML key": strings.Replace(creationConfig, "    desiredCapacity: 1",
			"    desiredCapacity: 1\n    desiredCapacity: 2", 1),
		"glob name":    strings.Replace(creationConfig, "name: workers", "name: 'workers*'", 1),
		"empty name":   strings.Replace(creationConfig, "name: workers", "name: ''", 1),
		"numeric name": strings.Replace(creationConfig, "name: workers", "name: 123", 1),
		"malformed tags": strings.Replace(creationConfig, "    desiredCapacity: 1",
			"    tags: invalid\n    desiredCapacity: 1", 1),
		"wrong group shape": "apiVersion: eksctl.io/v1alpha5\nkind: ClusterConfig\nmetadata: {}\nmanagedNodeGroups: {}\n",
		"missing metadata":  "managedNodeGroups: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			provisioner, runner, _ := newCreationProvisioner(t, config, allowCreation)
			_, err := runCreationUpdate(t, provisioner, false)
			require.Error(t, err)
			assert.Zero(t, runner.gets)
			assert.Zero(t, runner.creates)
		})
	}
}

func TestManagedNodegroupCreationPreservesUpdateGates(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"dry run", "removal", "immutable", "nil spec"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			provisioner, runner, _ := newCreationProvisioner(t, creationConfig, nil)
			if scenario == "removal" {
				runner.live = append(runner.live, activeCreationGroup("old"))
			}

			if scenario == "immutable" {
				old := activeCreationGroup("workers")
				old.InstanceType = "t3.large"
				runner.live = append(runner.live, old)
			}

			if scenario == "nil spec" {
				_, err := provisioner.Update(
					t.Context(),
					"",
					nil,
					nil,
					clusterupdate.UpdateOptions{},
				)
				require.NoError(t, err)
				assert.Zero(t, runner.gets)
			} else {
				result, err := runCreationUpdate(t, provisioner, scenario == "dry run")
				if scenario == "dry run" {
					require.NoError(t, err)
					assert.Len(t, result.InPlaceChanges, 1)
				} else {
					require.ErrorIs(t, err, clustererr.ErrRecreationRequired)
				}
			}

			assert.Zero(t, runner.creates)
			assert.Zero(t, runner.scales)
		})
	}
}

func TestManagedNodegroupCreationVerificationFailuresDoNotMutate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		verifier func(*string) eksidentity.Verifier
		wantErr  error
	}{
		{name: "missing verifier", verifier: func(*string) eksidentity.Verifier { return nil }},
		{
			name: "ownership changed", wantErr: eksidentity.ErrIdentityMismatch,
			verifier: func(*string) eksidentity.Verifier {
				return func(context.Context) error { return eksidentity.ErrIdentityMismatch }
			},
		},
		{name: "source changed", verifier: func(path *string) eksidentity.Verifier {
			return func(context.Context) error { return os.WriteFile(*path, []byte(creationConfig+"# changed\n"), 0o600) }
		}},
		{
			name:     "canceled",
			wantErr:  context.Canceled,
			verifier: func(*string) eksidentity.Verifier { return func(context.Context) error { return context.Canceled } },
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var source string

			provisioner, runner, path := newCreationProvisioner(
				t,
				creationConfig,
				testCase.verifier(&source),
			)
			source = path
			result, err := runCreationUpdate(t, provisioner, false)
			require.Error(t, err)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}

			assert.Empty(t, result.AppliedChanges)
			assert.Len(t, result.FailedChanges, 1)
			assert.Zero(t, runner.creates)
		})
	}
}

func TestManagedNodegroupCreationCommandFailuresDoNotReportApplied(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		change func(*creationRunner)
		err    error
	}{
		{name: "command failure", err: errCreationFailed},
		{name: "missing after create"},
		{name: "settings differ", change: func(runner *creationRunner) {
			group := activeCreationGroup("workers")
			group.DesiredCap = 2
			runner.live = append(runner.live, group)
		}},
		{name: "concurrent creation lacks our marker", change: func(runner *creationRunner) {
			runner.live = append(runner.live, activeCreationGroup("workers"))
			delete(runner.markers, "workers")
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			provisioner, runner, _ := newCreationProvisioner(t, creationConfig, allowCreation)
			runner.create = func(map[string]any) error {
				if testCase.change != nil {
					testCase.change(runner)
				}

				return testCase.err
			}
			result, err := runCreationUpdate(t, provisioner, false)
			require.Error(t, err)
			assert.Empty(t, result.AppliedChanges)
			assert.Len(t, result.FailedChanges, 1)

			for _, path := range runner.paths {
				assert.NoFileExists(t, path)
			}
		})
	}
}

func TestManagedNodegroupCreationReportsPartialSuccessAndRetriesOnlyMissingGroup(t *testing.T) {
	t.Parallel()

	config := creationConfig + "  - name: second\n    desiredCapacity: 1\n"
	provisioner, runner, _ := newCreationProvisioner(t, config, allowCreation)
	runner.create = func(config map[string]any) error {
		groups, groupsOK := config["managedNodeGroups"].([]any)
		require.True(t, groupsOK)
		require.Len(t, groups, 1)
		group, groupOK := groups[0].(map[string]any)
		require.True(t, groupOK)

		name, nameOK := group["name"].(string)
		require.True(t, nameOK)

		if name == "second" && runner.creates == 2 {
			return errCreationFailed
		}

		runner.live = append(runner.live, activeCreationGroup(name))

		return nil
	}
	result, err := runCreationUpdate(t, provisioner, false)
	require.ErrorIs(t, err, errCreationFailed)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "workers", result.AppliedChanges[0].NewValue)
	require.Len(t, result.FailedChanges, 1)
	assert.Equal(t, "eks.managedNodeGroups[second]", result.FailedChanges[0].Field)

	result, err = runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "second", result.AppliedChanges[0].NewValue)
	assert.Equal(t, 3, runner.creates)

	for _, path := range runner.paths {
		assert.NoFileExists(t, path)
	}
}

func TestManagedNodegroupCreationRejectsExistingGroupDisappearance(t *testing.T) {
	t.Parallel()

	provisioner, runner, _ := newCreationProvisioner(t,
		creationConfig+"  - name: new\n    desiredCapacity: 1\n", allowCreation)
	runner.live = append(runner.live, activeCreationGroup("workers"))
	runner.beforeGet = func(call int) {
		if call == 2 {
			runner.live = []eksctl.NodegroupSummary{}
		}
	}
	_, err := runCreationUpdate(t, provisioner, false)
	require.ErrorContains(t, err, "disappeared")
	assert.Zero(t, runner.creates)
}

func TestManagedNodegroupCreationRefreshesAutoscalerCapacityAfterSlowCreate(t *testing.T) {
	t.Parallel()

	config := creationConfig + "  - name: autoscaled\n    minSize: 1\n    maxSize: 10\n"
	provisioner, runner, _ := newCreationProvisioner(t, config, allowCreation)
	runner.live = append(runner.live, activeCreationGroup("autoscaled"))
	runner.create = func(map[string]any) error {
		// An autoscaler can change this group while eksctl creates another one.
		runner.live[0].DesiredCap = 3
		runner.live = append(runner.live, activeCreationGroup("workers"))

		return nil
	}
	_, err := runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"scale", "nodegroup", "--cluster", "ksail-test",
		"--name", "autoscaled", "--nodes", "3", "--nodes-max", "10", "--region", "us-east-1",
	}, runner.scaleArgs)
}

func (r *creationRunner) ListManagedNodegroups(
	context.Context,
	string,
) ([]ekstypes.Nodegroup, error) {
	groups := make([]ekstypes.Nodegroup, 0, len(r.live))
	for _, summary := range r.live {
		if summary.NodeGroupType != "managed" {
			continue
		}

		groups = append(groups, ekstypes.Nodegroup{
			ClusterName:   aws.String(summary.Cluster),
			NodegroupName: aws.String(summary.Name),
			Status: ekstypes.NodegroupStatus(
				summary.Status,
			),
			InstanceTypes: []string{summary.InstanceType},
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				DesiredSize: fixtureCapacity(
					summary.DesiredCap,
				),
				MinSize: fixtureCapacity(summary.MinSize),
				MaxSize: fixtureCapacity(summary.MaxSize),
			},
			Tags: map[string]string{"ksail.io/nodegroup-creation-id": r.markers[summary.Name]},
		})
	}

	return groups, nil
}

func TestManagedNodegroupCreationRejectsUnexpectedAppearance(t *testing.T) {
	t.Parallel()
	provisioner, runner, _ := newCreationProvisioner(t, creationConfig, allowCreation)
	runner.beforeGet = func(call int) {
		if call == 2 {
			runner.live = append(runner.live, activeCreationGroup("workers"))
		}
	}
	result, err := runCreationUpdate(t, provisioner, false)
	require.ErrorContains(t, err, "appeared after planning")
	assert.Empty(t, result.AppliedChanges)
	assert.Zero(t, runner.creates)
}

func TestManagedNodegroupCreationCleansSnapshotOnOwnershipRefusal(t *testing.T) {
	provisioner, runner, _ := newCreationProvisioner(
		t,
		creationConfig,
		func(context.Context) error {
			return eksidentity.ErrIdentityMismatch
		},
	)
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	t.Setenv("TMP", tempRoot)
	t.Setenv("TEMP", tempRoot)
	_, err := runCreationUpdate(t, provisioner, false)
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	files, err := os.ReadDir(tempRoot)
	require.NoError(t, err)
	assert.Empty(t, files)
	assert.Zero(t, runner.creates)
}

func fixtureCapacity(value int) *int32 {
	//nolint:gosec // fixture capacities range from zero to ten.
	converted := int32(value)

	return &converted
}

func TestManagedNodegroupCreationUsesSDKWhenEksctlOmitsManagedGroups(t *testing.T) {
	t.Parallel()
	provisioner, runner, _ := newCreationProvisioner(t, creationConfig, allowCreation)
	empty := "[]"
	runner.output = &empty
	runner.live = append(runner.live, activeCreationGroup("workers"))
	result, err := runCreationUpdate(t, provisioner, false)
	require.NoError(t, err)
	assert.Zero(t, result.TotalChanges())
	assert.Zero(t, runner.creates)
}

func TestManagedNodegroupCreationRequiresAuthoritativeStackAbsence(t *testing.T) {
	t.Parallel()

	for _, lookupFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(lookupFailure), func(t *testing.T) {
			t.Parallel()
			provisioner, runner, _ := newCreationProvisioner(t, creationConfig, allowCreation)

			runner.stackExists = !lookupFailure
			if lookupFailure {
				runner.stackErr = errCreationFailed
			}

			result, err := runCreationUpdate(t, provisioner, false)
			require.Error(t, err)
			assert.Empty(t, result.AppliedChanges)
			assert.Zero(t, runner.creates)
		})
	}
}
