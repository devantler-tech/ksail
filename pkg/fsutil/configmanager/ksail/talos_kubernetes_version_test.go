package configmanager_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
)

func TestWarnKubernetesVersionCapped_WarnsWhenDefaultCapped(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.Version = "v1.12.4"

	var out bytes.Buffer
	// Resolved version differs from the built-in default => the default was capped.
	configmanager.WarnKubernetesVersionCappedForTest(cfg, "1.35.0", &out)

	assert.Contains(t, out.String(), "too new for the pinned Talos version v1.12.4")
	assert.Contains(t, out.String(), "1.35.0")
	assert.Contains(t, out.String(), "spec.cluster.kubernetesVersion")
}

func TestWarnKubernetesVersionCapped_SilentWhenExplicitlyPinned(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.Version = "v1.12.4"
	cfg.Spec.Cluster.KubernetesVersion = "1.34.0"

	var out bytes.Buffer
	configmanager.WarnKubernetesVersionCappedForTest(cfg, "1.34.0", &out)

	assert.Empty(t, out.String(), "explicit pin should suppress the capping notice")
}

func TestWarnKubernetesVersionCapped_SilentWhenNoTalosVersionPinned(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos

	var out bytes.Buffer
	configmanager.WarnKubernetesVersionCappedForTest(cfg, "1.35.0", &out)

	assert.Empty(t, out.String(), "no Talos version pin means no capping decision to explain")
}

func TestWarnKubernetesVersionCapped_SilentWhenDefaultNotCapped(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NewCluster()
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	cfg.Spec.Cluster.Talos.Version = "v1.13.2"

	var out bytes.Buffer
	// Resolved equals the default => nothing was capped, so nothing is reported.
	configmanager.WarnKubernetesVersionCappedForTest(
		cfg, talosconfigmanager.DefaultKubernetesVersion, &out,
	)

	assert.Empty(t, out.String(), "an uncapped default needs no notice")
}
