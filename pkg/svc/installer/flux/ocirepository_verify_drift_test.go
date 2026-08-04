package fluxinstaller_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
)

// keylessVerifySpec is the shape the platform actually configures: cosign
// keyless with pinned OIDC identity matchers.
func keylessVerifySpec() v1alpha1.FluxVerifySpec {
	return v1alpha1.FluxVerifySpec{
		Provider: "cosign",
		MatchOIDCIdentity: []v1alpha1.FluxVerifyOIDCIdentity{
			{
				Issuer:  `^https://token\.actions\.githubusercontent\.com$`,
				Subject: `^https://github\.com/devantler-tech/platform/.+$`,
			},
		},
	}
}

// TestVerifyDriftUnverifiedRepositoryIsDrift is the core of platform#2922: the
// configuration carries a verification policy and the live OCIRepository carries
// no spec.verify at all. That is exactly the state read off the prod cluster —
// config half correct, enforcement half off — and it must register as drift, or
// cluster update has nothing to act on and the infrastructure source stays
// unverified indefinitely.
func TestVerifyDriftUnverifiedRepositoryIsDrift(t *testing.T) {
	t.Parallel()

	drifted := fluxinstaller.VerifyDrift(nil, true, keylessVerifySpec())

	assert.True(
		t,
		drifted,
		"a configured verification policy over a live OCIRepository with no spec.verify must be drift",
	)
}

// TestVerifyDriftMatchingBlockIsNotDrift is the discriminating control for the
// test above: the same spec against a live block that already matches must NOT
// report drift. Without this, a VerifyDrift that simply returned true would pass
// the defect test and make every update re-patch an already-correct resource.
func TestVerifyDriftMatchingBlockIsNotDrift(t *testing.T) {
	t.Parallel()

	spec := keylessVerifySpec()
	live := fluxinstaller.BuildVerifyPatch(spec)

	drifted := fluxinstaller.VerifyDrift(live, true, spec)

	assert.False(t, drifted,
		"a live spec.verify already equal to the configured block must not report drift")
}

// TestVerifyDriftChangedMatcherIsDrift pins that the comparison actually reads
// the matcher contents rather than merely checking that some verify block is
// present. A relaxed or repointed signer identity is a security-relevant change
// and must be repaired.
func TestVerifyDriftChangedMatcherIsDrift(t *testing.T) {
	t.Parallel()

	spec := keylessVerifySpec()
	live := fluxinstaller.BuildVerifyPatch(spec)

	// Same shape, different signer subject — the live policy trusts someone else.
	matchers, isSlice := live["matchOIDCIdentity"].([]any)
	if !isSlice || len(matchers) == 0 {
		t.Fatalf("expected rendered matchOIDCIdentity, got %#v", live["matchOIDCIdentity"])
	}

	matcher, isMap := matchers[0].(map[string]any)
	if !isMap {
		t.Fatalf("expected matcher map, got %#v", matchers[0])
	}

	matcher["subject"] = `^https://github\.com/someone-else/.+$`

	drifted := fluxinstaller.VerifyDrift(live, true, spec)

	assert.True(t, drifted, "a live matcher trusting a different signer must report drift")
}

// TestVerifyDriftAbsentRepositoryIsNotDrift pins the bootstrap guard. Before the
// flux-operator has generated the OCIRepository there is nothing an update could
// patch, so reporting drift would fabricate a change that no handler can satisfy.
func TestVerifyDriftAbsentRepositoryIsNotDrift(t *testing.T) {
	t.Parallel()

	drifted := fluxinstaller.VerifyDrift(nil, false, keylessVerifySpec())

	assert.False(t, drifted,
		"an OCIRepository that does not exist yet is a bootstrap gap, not drift")
}

// TestVerifyDriftAgreesWithApplyVerify pins the two halves of the loop against
// each other.
//
// The detector decides whether cluster update reports a change; applyVerify
// decides whether the resulting patch actually writes anything. They compare the
// same two values today, and nothing but this test stops them drifting apart. If
// the detector ever calls drift on a block applyVerify considers settled, every
// update reports a verification change forever and the handler quietly no-ops —
// a permanently dirty diff that no amount of reconciling clears.
//
// The states are asserted in both directions, so a detector hardwired to either
// answer fails.
func TestVerifyDriftAgreesWithApplyVerify(t *testing.T) {
	t.Parallel()

	spec := keylessVerifySpec()

	for name, live := range map[string]map[string]any{
		"already verified": fluxinstaller.BuildVerifyPatch(spec),
		"unverified":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			obj := map[string]any{"spec": map[string]any{}}
			if live != nil {
				obj["spec"] = map[string]any{"verify": live}
			}

			settled, err := fluxinstaller.ApplyVerify(obj, fluxinstaller.BuildVerifyPatch(spec))
			if err != nil {
				t.Fatalf("applyVerify: %v", err)
			}

			drifted := fluxinstaller.VerifyDrift(live, true, spec)

			assert.Equal(
				t,
				settled,
				!drifted,
				"detector and patcher disagree on %q: applyVerify settled=%v, VerifyDrift drifted=%v",
				name,
				settled,
				drifted,
			)
		})
	}
}
