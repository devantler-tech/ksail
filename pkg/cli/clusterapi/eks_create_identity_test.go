package clusterapi_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The names a spec can point AWS resolution at. They are deliberately not the canonical AWS_* ones,
// because the whole point is to tell the two sources apart.
const (
	customRegionEnvVar  = "KSAIL_TEST_CUSTOM_AWS_REGION"
	customRegionValue   = "eu-west-9"
	canonicalRegionEnv  = "AWS_REGION"
	canonicalRegionVal  = "us-east-1"
	rebindingRegionVal  = "ap-south-7"
)

// midCreateProvisioner runs onCreate from inside Create, so a test can act at a point that is
// provably between runCreate's snapshot and the capture that follows.
type midCreateProvisioner struct {
	*fakeProvisioner

	onCreate func()
}

func (p *midCreateProvisioner) Create(ctx context.Context, name string) error {
	if p.onCreate != nil {
		p.onCreate()
	}

	return p.fakeProvisioner.Create(ctx, name)
}

type midCreateFactory struct {
	provisioner *midCreateProvisioner
}

func (f midCreateFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

// captureRecord is what the create pinned for the capture that follows it.
type captureRecord struct {
	name            string
	pinned          bool
	awsOptions      v1alpha1.OptionsAWS
	selectionRegion string
}

// recordCaptureForCreate runs an EKS create whose spec resolves AWS through awsOptions, and returns
// the identity the create pinned for its capture.
func recordCaptureForCreate(
	t *testing.T,
	clusterName string,
	awsOptions v1alpha1.OptionsAWS,
	beforeCapture func(),
) captureRecord {
	t.Helper()

	// A provisioner that runs beforeCapture from inside Create. That point is, in program order,
	// strictly after runCreate takes its snapshot and strictly before the capture — which is what
	// makes the mid-create assertion deterministic instead of a race against the create goroutine.
	provisioner := &midCreateProvisioner{
		fakeProvisioner: &fakeProvisioner{},
		onCreate:        beforeCapture,
	}
	service := clusterapi.NewTestService(func(
		_ v1alpha1.Distribution,
		_ string,
	) (clusterprovisioner.Factory, error) {
		return midCreateFactory{provisioner: provisioner}, nil
	})

	records := make(chan captureRecord, 1)

	service.SetEKSOwnershipCaptureRecorderForTest(
		func(name string, pinned bool, options v1alpha1.OptionsAWS, region string) {
			records <- captureRecord{
				name:            name,
				pinned:          pinned,
				awsOptions:      options,
				selectionRegion: region,
			}
		},
	)

	cluster := clusterFor(clusterName, v1alpha1.DistributionEKS)
	cluster.Spec.Provider.AWS = awsOptions

	_, err := service.Create(context.Background(), cluster)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)

	select {
	case got := <-records:
		return got
	default:
		t.Fatal("a successful EKS create pinned no identity for its ownership capture")

		return captureRecord{}
	}
}

// TestCreateEKSCapturesUnderItsOwnCredentialNames is the regression this file exists for.
//
// The create provisioner resolves AWS through the cluster spec's own variable names; the capture
// that follows used to resolve through this backend's ambient resolver, which reads the canonical
// AWS_* names. Those are different sources, so a spec naming custom variables had its cluster
// created in one account and its immutable ownership identity recorded from another.
//
// That does not fail closed. The capture finds whatever same-named cluster lives in the ambient
// account and records it as this cluster's identity, so every later guarded delete, start or stop
// verifies successfully against an unrelated incarnation and then mutates it.
func TestCreateEKSCapturesUnderItsOwnCredentialNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Both are set, to distinct values. Only a capture reading the spec's name can tell them apart:
	// reading either name in isolation would pass whichever source it happened to use.
	t.Setenv(canonicalRegionEnv, canonicalRegionVal)
	t.Setenv(customRegionEnvVar, customRegionValue)

	got := recordCaptureForCreate(t, "custom-names-eks", v1alpha1.OptionsAWS{
		RegionEnvVar: customRegionEnvVar,
	}, nil)

	require.True(t, got.pinned,
		"the create supplied no pinned identity, so the capture would resolve AWS independently")

	assert.Equal(t, customRegionValue, got.selectionRegion,
		"the capture resolved through the canonical AWS_* names instead of the spec's own, so it "+
			"would record a same-named cluster in a different account as this cluster's identity")

	assert.Equal(t, customRegionEnvVar, got.awsOptions.RegionEnvVar,
		"the recorded identity does not carry the variable names it was resolved through, so a "+
			"later verification cannot resolve the same identity this capture observed")
}

// TestCreateEKSCaptureSnapshotSurvivesAMidCreateEnvironmentChange pins the timing half.
//
// eksctl runs asynchronously, so an operator changing Settings while a create works would otherwise
// move the values the capture reads afterwards. The snapshot is taken before the create starts,
// which is what makes the recorded identity describe the cluster the create actually made.
func TestCreateEKSCaptureSnapshotSurvivesAMidCreateEnvironmentChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(canonicalRegionEnv, canonicalRegionVal)

	got := recordCaptureForCreate(t, "rebound-mid-create-eks", v1alpha1.OptionsAWS{}, func() {
		t.Setenv(canonicalRegionEnv, rebindingRegionVal)
	})

	require.True(t, got.pinned)
	assert.Equal(t, canonicalRegionVal, got.selectionRegion,
		"the capture re-read the environment after the create, so a Settings change during an "+
			"asynchronous create redirects the identity that gets recorded")
}

// TestCreateEKSWithDefaultOptionsStillResolvesTheCanonicalNames is the control.
//
// It must fail if the fix merely swapped one hard-coded source for another: a spec that names no
// custom variables has to keep resolving exactly the canonical AWS_* names it always did. Without
// this, the two tests above would also pass an implementation that ignored the ambient environment
// entirely, and the default path — every cluster in the portfolio — would silently change.
func TestCreateEKSWithDefaultOptionsStillResolvesTheCanonicalNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(canonicalRegionEnv, canonicalRegionVal)
	t.Setenv(customRegionEnvVar, customRegionValue)

	got := recordCaptureForCreate(t, "default-names-eks", v1alpha1.OptionsAWS{}, nil)

	require.True(t, got.pinned)
	assert.Equal(t, canonicalRegionVal, got.selectionRegion,
		"an empty spec must fall back to the canonical AWS_* names")
}
