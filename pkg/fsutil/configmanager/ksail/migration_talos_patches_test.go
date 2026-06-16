package configmanager_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarnDeprecatedTalosPatchFields_CDIDisabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.CDI = v1alpha1.CDIDisabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Contains(t, out.String(), "spec.cluster.cdi is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_CDIEnabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.CDI = v1alpha1.CDIEnabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Contains(t, out.String(), "spec.cluster.cdi is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_CDIIgnoredForVanilla(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionVanilla
	cfg.Spec.Cluster.CDI = v1alpha1.CDIDisabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Empty(t, out.String())
}

func TestWarnDeprecatedTalosPatchFields_CDIDefault(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.CDI = v1alpha1.CDIDefault

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.NotContains(t, out.String(), "spec.cluster.cdi is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_OIDCEnabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.OIDC.IssuerURL = "https://dex.example.com"

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Contains(t, out.String(), "spec.cluster.oidc is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_OIDCDisabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.NotContains(t, out.String(), "spec.cluster.oidc is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_IngressFirewallDisabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cfg.Spec.Provider.Hetzner.IngressFirewall = v1alpha1.IngressFirewallDisabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Contains(t, out.String(), "spec.provider.hetzner.ingressFirewall is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_IngressFirewallEnabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderHetzner
	cfg.Spec.Provider.Hetzner.IngressFirewall = v1alpha1.IngressFirewallEnabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.NotContains(t, out.String(), "spec.provider.hetzner.ingressFirewall is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_IngressFirewallNonHetzner(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Provider = v1alpha1.ProviderDocker

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.NotContains(t, out.String(), "spec.provider.hetzner.ingressFirewall is deprecated")
}

// setLegacyImageVerification sets the deprecated Talos-scoped imageVerification
// field, isolating the staticcheck deprecation nolint to one place.
func setLegacyImageVerification(cfg *v1alpha1.Cluster, value v1alpha1.ImageVerification) {
	//nolint:staticcheck // intentional: exercising the deprecated alias under migration test
	cfg.Spec.Cluster.Talos.ImageVerification = value
}

// legacyImageVerification reads the deprecated Talos-scoped imageVerification
// field, isolating the staticcheck deprecation nolint to one place.
func legacyImageVerification(cfg *v1alpha1.Cluster) v1alpha1.ImageVerification {
	//nolint:staticcheck // intentional: exercising the deprecated alias under migration test
	return cfg.Spec.Cluster.Talos.ImageVerification
}

// TestMigrateDeprecatedImageVerification_PromotesLegacyValue verifies the legacy
// Talos-scoped field is copied into the promoted cluster-level field, the legacy
// field is zeroed, and a deprecation warning is emitted.
func TestMigrateDeprecatedImageVerification_PromotesLegacyValue(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	setLegacyImageVerification(cfg, v1alpha1.ImageVerificationEnabled)

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedImageVerificationForTest(cfg, &out))

	assert.Equal(t, v1alpha1.ImageVerificationEnabled, cfg.Spec.Cluster.ImageVerification)
	assert.Empty(t, legacyImageVerification(cfg))
	assert.Contains(t, out.String(), "spec.cluster.talos.imageVerification is deprecated")
}

// TestMigrateDeprecatedImageVerification_NoLegacyValue verifies a no-op when the
// legacy field is unset.
func TestMigrateDeprecatedImageVerification_NoLegacyValue(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ImageVerification = v1alpha1.ImageVerificationEnabled

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedImageVerificationForTest(cfg, &out))

	assert.Equal(t, v1alpha1.ImageVerificationEnabled, cfg.Spec.Cluster.ImageVerification)
	assert.NotContains(t, out.String(), "deprecated")
}

// TestMigrateDeprecatedImageVerification_EquivalentValuesNoWarn verifies that
// setting both fields to the same value silently zeroes the legacy field without
// a warning (no conflict).
func TestMigrateDeprecatedImageVerification_EquivalentValuesNoWarn(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ImageVerification = v1alpha1.ImageVerificationEnabled
	setLegacyImageVerification(cfg, v1alpha1.ImageVerificationEnabled)

	var out bytes.Buffer

	require.NoError(t, configmanager.MigrateDeprecatedImageVerificationForTest(cfg, &out))

	assert.Equal(t, v1alpha1.ImageVerificationEnabled, cfg.Spec.Cluster.ImageVerification)
	assert.Empty(t, legacyImageVerification(cfg))
	assert.NotContains(t, out.String(), "deprecated")
}

// TestMigrateDeprecatedImageVerification_ConflictErrors verifies that setting both
// fields to conflicting values returns an error.
func TestMigrateDeprecatedImageVerification_ConflictErrors(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.ImageVerification = v1alpha1.ImageVerificationDisabled
	setLegacyImageVerification(cfg, v1alpha1.ImageVerificationEnabled)

	var out bytes.Buffer

	require.Error(t, configmanager.MigrateDeprecatedImageVerificationForTest(cfg, &out))
}

func TestWarnDeprecatedTalosPatchFields_AllDefaults(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Empty(t, out.String())
}

func TestWarnDeprecatedTalosPatchFields_NilConfig(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(nil, &out)
	assert.Empty(t, out.String())
}

func TestWarnDeprecatedTalosPatchFields_NilWriter(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.CDI = v1alpha1.CDIDisabled

	// Should not panic with nil writer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, nil)
}
