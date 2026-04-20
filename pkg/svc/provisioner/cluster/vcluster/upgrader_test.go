package vclusterprovisioner_test

import (
	"context"
	"errors"
	"testing"

	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
)

// Compile-time check that Provisioner implements Upgrader.
var _ clusterupdate.Upgrader = (*vclusterprovisioner.Provisioner)(nil)

func TestUpgradeKubernetes_ReturnsRecreationRequired(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	err := provisioner.UpgradeKubernetes(context.Background(), "test", "v1.30.0", "v1.31.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, clustererr.ErrRecreationRequired) {
		t.Errorf("expected ErrRecreationRequired, got: %v", err)
	}
}

func TestUpgradeDistribution_ReturnsRecreationRequired(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	err := provisioner.UpgradeDistribution(context.Background(), "test", "v1.30.0", "v1.31.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, clustererr.ErrRecreationRequired) {
		t.Errorf("expected ErrRecreationRequired, got: %v", err)
	}
}

func TestGetCurrentVersions(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KubernetesVersion != vclusterconfigmanager.DefaultKubernetesVersion {
		t.Errorf(
			"KubernetesVersion = %q, want %q",
			info.KubernetesVersion,
			vclusterconfigmanager.DefaultKubernetesVersion,
		)
	}

	if info.DistributionVersion == "" {
		t.Error("expected non-empty DistributionVersion")
	}
}

func TestKubernetesImageRef(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	if got := provisioner.KubernetesImageRef(); got != "ghcr.io/loft-sh/kubernetes" {
		t.Errorf("KubernetesImageRef() = %q, want %q", got, "ghcr.io/loft-sh/kubernetes")
	}
}

func TestDistributionImageRef(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	if got := provisioner.DistributionImageRef(); got != "ghcr.io/loft-sh/vcluster-pro" {
		t.Errorf("DistributionImageRef() = %q, want %q", got, "ghcr.io/loft-sh/vcluster-pro")
	}
}

func TestVersionSuffix_Empty(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	if got := provisioner.VersionSuffix(); got != "" {
		t.Errorf("VersionSuffix() = %q, want %q", got, "")
	}
}

func TestPrepareConfigForVersion_NoOp(t *testing.T) {
	t.Parallel()

	provisioner := vclusterprovisioner.NewProvisioner("test", "", false, nil)

	err := provisioner.PrepareConfigForVersion("Kubernetes", "v1.31.0")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
