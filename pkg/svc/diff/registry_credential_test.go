package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

func newCredentialEngine() *specdiff.Engine {
	return specdiff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
}

// TestCheckRegistryCredentialDetectsRotation is the core of issue #6107: the
// structural diff redacts registry passwords, so a rotated token behind an
// otherwise identical configuration must still surface as an in-place change or
// the cluster keeps authenticating with the revoked value.
func TestCheckRegistryCredentialDetectsRotation(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newCredentialEngine().CheckRegistryCredential(true, result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf(
			"a rotated credential must produce exactly one in-place change, got %d",
			len(result.InPlaceChanges),
		)
	}

	change := result.InPlaceChanges[0]
	if change.Field != specdiff.RegistryCredentialField {
		t.Errorf("field = %q, want %q", change.Field, specdiff.RegistryCredentialField)
	}

	if change.Category != clusterupdate.ChangeCategoryInPlace {
		t.Errorf(
			"category = %q, want in-place — a credential rotation must never demand recreation",
			change.Category,
		)
	}
}

// TestCheckRegistryCredentialRendersOnlyRedactedPlaceholders pins the redaction
// property.
//
// The engine now receives a single bit rather than the credential or a digest of
// it, so no input it is given can reach a rendered value — asserting that some
// secret literal is absent would be unfalsifiable here. What is still worth
// pinning, and what this asserts, is that the rendered values are the fixed
// placeholders: reintroducing a value-carrying parameter and rendering it fails
// this test.
func TestCheckRegistryCredentialRendersOnlyRedactedPlaceholders(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newCredentialEngine().CheckRegistryCredential(true, result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf("expected one in-place change, got %d", len(result.InPlaceChanges))
	}

	change := result.InPlaceChanges[0]
	if change.OldValue != "stale (redacted)" {
		t.Errorf("old value = %q, want the fixed redacted placeholder", change.OldValue)
	}

	if change.NewValue != "rotated (redacted)" {
		t.Errorf("new value = %q, want the fixed redacted placeholder", change.NewValue)
	}
}

// TestCheckRegistryCredentialSuppressesNonDrift covers every reason the drift
// check reports false: an unrotated credential, a Secret KSail does not own, and
// a configuration with no credential to write. The flux installer collapses all
// three into "not drifted" so no credential material reaches this package; the
// per-reason coverage lives with that comparison, in the flux installer tests.
func TestCheckRegistryCredentialSuppressesNonDrift(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newCredentialEngine().CheckRegistryCredential(false, result)

	total := len(result.InPlaceChanges) +
		len(result.RecreateRequired) +
		len(result.RebootRequired) +
		len(result.WipeRequired) +
		len(result.RollingRecreate) +
		len(result.UnknownBaseline)
	if total != 0 {
		t.Errorf("expected no change when the credential has not drifted, got %d", total)
	}
}
