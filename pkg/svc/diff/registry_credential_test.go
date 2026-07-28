package diff_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// theSecretPassword is the credential a rotation moves away from. Every
// assertion below checks it never reaches a Change, because Change values are
// rendered by the cluster-update output.
//
// It is a fabricated literal that has never been a real credential — it exists
// precisely so a test can prove this value does NOT escape into output.
//
//nolint:gosec // G101: an intentional fake credential, and the subject of the leak assertions.
const theSecretPassword = "ghp_rotated_token_value_9f3a"

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

// TestCheckRegistryCredentialNeverRendersTheCredential pins the redaction
// property. The engine is given a single bit rather than the credential or a
// digest of it, so the only thing it can render is its own fixed placeholder —
// this test is what proves that placeholder stayed placeholder.
func TestCheckRegistryCredentialNeverRendersTheCredential(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newCredentialEngine().CheckRegistryCredential(true, result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf("expected one in-place change, got %d", len(result.InPlaceChanges))
	}

	change := result.InPlaceChanges[0]
	for _, rendered := range []string{change.OldValue, change.NewValue, change.Reason} {
		if strings.Contains(rendered, theSecretPassword) {
			t.Errorf("rendered value %q leaks the credential", rendered)
		}
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
