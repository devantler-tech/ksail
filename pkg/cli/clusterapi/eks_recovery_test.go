package clusterapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecoveredEKSLifecycleUsesOwnershipWithoutCreationState drives all local API mutations with
// only the record eks-bind writes. The guard must still be carried into the provisioner.
func TestRecoveredEKSLifecycleUsesOwnershipWithoutCreationState(t *testing.T) {
	for _, action := range []string{"delete", "start", "stop"} {
		t.Run(action, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			name := "recovered-" + action
			require.NoError(t, state.SaveEKSOwnershipState(name, "eu-north-1",
				ownershipRecordFor(name, "eu-north-1")))

			provisioner := &fakeProvisioner{}
			recorder := &guardRecorder{}
			service := newGuardRecordingService(
				t,
				v1alpha1.DistributionEKS,
				provisioner,
				recorder,
				nil,
			)
			invoke, observed := recoveredEKSAction(service, provisioner, action)

			require.NoError(t, invoke(t.Context(), "default", name))
			require.Eventually(
				t,
				func() bool { return len(observed()) == 1 && !service.JobPresentForTest(name) },
				eventuallyTimeout,
				eventuallyTick,
			)
			assert.Equal(t, []string{name}, observed())
			assert.True(t, recorder.wasGuarded())

			_, err := state.LoadClusterSpec(name)
			require.ErrorIs(
				t,
				err,
				state.ErrStateNotFound,
				"recovery must not synthesize a sanitized spec",
			)
		})
	}
}

// recoveredEKSAction pairs an API operation with the corresponding fake's observable mutation.
func recoveredEKSAction(service *clusterapi.Service, provisioner *fakeProvisioner, action string) (
	func(context.Context, string, string) error, func() []string,
) {
	switch action {
	case "start":
		return service.Start, provisioner.startedNames
	case "stop":
		return service.Stop, provisioner.stoppedNames
	default:
		return service.Delete, provisioner.deletedNames
	}
}

// TestRecoveredEKSConfigUsesRecordedRegion ensures missing spec.json and eks.yaml never let the
// currently selected AWS region replace the region recovered by eks-bind.
func TestRecoveredEKSConfigUsesRecordedRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "us-west-2")

	const name = "recovered-config"
	require.NoError(t, state.SaveEKSOwnershipState(name, "eu-north-1",
		ownershipRecordFor(name, "eu-north-1")))

	path, region, err := clusterapi.ExportEKSConfigForCreate(name)
	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", region)

	data, err := os.ReadFile(path) //nolint:gosec // generated under the test's temporary home.
	require.NoError(t, err)
	assert.Contains(t, string(data), "region: eu-north-1")
	assert.NotContains(t, string(data), "us-west-2")
}

// TestRecoveredEKSRefusesAmbiguousRegions preserves the local API's name-only target boundary.
func TestRecoveredEKSRefusesAmbiguousRegions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "recovered-ambiguous"
	for _, region := range []string{"eu-north-1", "us-east-1"} {
		require.NoError(
			t,
			state.SaveEKSOwnershipState(name, region, ownershipRecordFor(name, region)),
		)
	}

	_, _, err := clusterapi.ExportEKSConfigForCreate(name)
	require.ErrorIs(t, err, api.ErrInvalid)
	require.ErrorContains(t, err, "more than one region")
}

// TestRecoveredEKSPreventsCreateOverExistingIdentity refuses to reinterpret a bound target as a
// new create merely because the original creation snapshot was lost.
func TestRecoveredEKSPreventsCreateOverExistingIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "recovered-create"
	require.NoError(t, state.SaveEKSOwnershipState(name, "eu-north-1",
		ownershipRecordFor(name, "eu-north-1")))

	service := newTestService(
		map[v1alpha1.Distribution]*fakeProvisioner{v1alpha1.DistributionEKS: {}},
	)
	_, err := service.Create(t.Context(), clusterFor(name, v1alpha1.DistributionEKS))
	require.ErrorIs(t, err, api.ErrAlreadyExists)
}

// TestRecoveredEKSStillRequiresLiveVerification ensures the new local evidence never bypasses a
// failed account or incarnation check.
func TestRecoveredEKSStillRequiresLiveVerification(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "recovered-mismatch"
	require.NoError(t, state.SaveEKSOwnershipState(name, "eu-north-1",
		ownershipRecordFor(name, "eu-north-1")))

	provisioner := &fakeProvisioner{}
	service := newGuardRecordingService(
		t,
		v1alpha1.DistributionEKS,
		provisioner,
		&guardRecorder{},
		errGuardRefused,
	)
	require.NoError(t, service.Stop(t.Context(), "default", name))
	requireEventuallyPhase(t, service, name, v1alpha1.ClusterPhaseFailed)
	assert.Empty(t, provisioner.stoppedNames())
}

// TestRecoveredEKSAfterFailedCreateIsNotClearedLocally keeps the recovered identity authoritative
// even when an old failed-create job remains in the web server's memory.
func TestRecoveredEKSAfterFailedCreateIsNotClearedLocally(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "recovered-after-failure"

	provisioner := &fakeProvisioner{createErr: errSimulatedCreateFailure}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)
	_, err := service.Create(t.Context(), clusterFor(name, v1alpha1.DistributionEKS))
	require.NoError(t, err)
	requireEventuallyPhase(t, service, name, v1alpha1.ClusterPhaseFailed)
	require.NoError(t, state.SaveEKSOwnershipState(name, "eu-north-1",
		ownershipRecordFor(name, "eu-north-1")))

	require.NoError(t, service.Delete(t.Context(), "default", name))
	require.Eventually(
		t,
		func() bool { return len(provisioner.deletedNames()) == 1 && !service.JobPresentForTest(name) },
		eventuallyTimeout,
		eventuallyTick,
	)
	assert.True(t, recorder.wasGuarded())
}

// TestRecoveredEKSDefaultGuardChecksLiveIdentity reaches the real EKS/STS SDK path with local HTTP
// fixtures. A valid recovered target succeeds; a same-name replacement never reaches mutation.
func TestRecoveredEKSDefaultGuardChecksLiveIdentity(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprintf("replaced=%t", replaced), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("AWS_PROFILE", "")
			t.Setenv("AWS_REGION", "us-west-2")
			t.Setenv("AWS_ACCESS_KEY_ID", "recovery-fixture")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "recovery-secret")
			t.Setenv("AWS_SESSION_TOKEN", "")
			t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")

			const name = "recovered-live-identity"

			record := ownershipRecordFor(name, "eu-north-1")
			require.NoError(t, state.SaveEKSOwnershipState(name, record.Region, record))
			requests := serveRecoveredEKSIdentity(t, record, replaced)
			provisioner := &fakeProvisioner{}
			recorder := &guardRecorder{}
			service := newGuardRecordingService(
				t,
				v1alpha1.DistributionEKS,
				provisioner,
				recorder,
				nil,
			)
			service.UseDefaultEKSOwnershipGuardForTest()
			require.NoError(t, service.Stop(t.Context(), "default", name))

			if replaced {
				requireEventuallyPhase(t, service, name, v1alpha1.ClusterPhaseFailed)
				assert.Empty(t, provisioner.stoppedNames())
				assert.False(t, recorder.wasGuarded())
			} else {
				require.Eventually(t, func() bool {
					return len(provisioner.stoppedNames()) == 1 && !service.JobPresentForTest(name)
				}, eventuallyTimeout, eventuallyTick)
				assert.True(t, recorder.wasGuarded())
			}

			assert.Equal(t, int32(2), requests.Load(), "both STS and EKS identity must be checked")
		})
	}
}

// serveRecoveredEKSIdentity answers only read-only identity requests and verifies the actual signed
// requests use the recorded region despite ambient region drift.
func serveRecoveredEKSIdentity(
	t *testing.T,
	record *state.EKSOwnershipState,
	replaced bool,
) *atomic.Int32 {
	t.Helper()

	requests := &atomic.Int32{}

	created := record.CreatedAt.Unix()
	if replaced {
		created++
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests.Add(1)
			assert.Contains(t, request.Header.Get("Authorization"), "Credential=recovery-fixture/")
			assert.Contains(t, request.Header.Get("Authorization"), "/eu-north-1/")

			if request.URL.Path == "/" {
				response.Header().Set("Content-Type", "text/xml")
				_, _ = fmt.Fprintf(
					response,
					`<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">`+
						`<GetCallerIdentityResult><Account>%s</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`,
					record.AccountID,
				)

				return
			}

			assert.Equal(t, "/clusters/"+record.ClusterName, request.URL.Path)
			assert.Equal(t, http.MethodGet, request.Method)
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"cluster":{"name":%q,"arn":%q,"createdAt":%d}}`,
				record.ClusterName, record.ClusterARN, created)
		}),
	)
	t.Cleanup(server.Close)
	t.Setenv("AWS_ENDPOINT_URL_EKS", server.URL)
	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

	return requests
}
