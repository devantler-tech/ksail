package configmanager_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
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

func TestWarnDeprecatedTalosPatchFields_ImageVerificationEnabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.ImageVerification = v1alpha1.ImageVerificationEnabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.Contains(t, out.String(), "spec.cluster.talos.imageVerification is deprecated")
}

func TestWarnDeprecatedTalosPatchFields_ImageVerificationDisabled(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.ImageVerification = v1alpha1.ImageVerificationDisabled

	var out bytes.Buffer
	configmanager.WarnDeprecatedTalosPatchFieldsForTest(cfg, &out)
	assert.NotContains(t, out.String(), "spec.cluster.talos.imageVerification is deprecated")
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
