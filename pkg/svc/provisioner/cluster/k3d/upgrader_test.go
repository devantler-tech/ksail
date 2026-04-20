package k3dprovisioner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// Compile-time check that Provisioner implements Upgrader.
var _ clusterupdate.Upgrader = (*k3dprovisioner.Provisioner)(nil)

func TestUpgradeKubernetes_ReturnsRecreationRequired(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha5.SimpleConfig{}
	provisioner := k3dprovisioner.NewProvisioner(cfg, "")

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

	cfg := &v1alpha5.SimpleConfig{}
	provisioner := k3dprovisioner.NewProvisioner(cfg, "")

	err := provisioner.UpgradeDistribution(context.Background(), "test", "v1.30.0", "v1.31.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, clustererr.ErrRecreationRequired) {
		t.Errorf("expected ErrRecreationRequired, got: %v", err)
	}
}

func TestGetCurrentVersions_WithConfig(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha5.SimpleConfig{}
	cfg.Image = "rancher/k3s:v1.35.3-k3s1"

	provisioner := k3dprovisioner.NewProvisioner(cfg, "")

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedTag := "v1.35.3-k3s1"

	if info.KubernetesVersion != expectedTag {
		t.Errorf("KubernetesVersion = %q, want %q", info.KubernetesVersion, expectedTag)
	}

	if info.DistributionVersion != expectedTag {
		t.Errorf("DistributionVersion = %q, want %q", info.DistributionVersion, expectedTag)
	}
}

func TestGetCurrentVersions_DigestPinned(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha5.SimpleConfig{}
	cfg.Image = "rancher/k3s:v1.35.3-k3s1@sha256:abc123def456"

	provisioner := k3dprovisioner.NewProvisioner(cfg, "")

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KubernetesVersion != "v1.35.3-k3s1" {
		t.Errorf("KubernetesVersion = %q, want %q", info.KubernetesVersion, "v1.35.3-k3s1")
	}
}

func TestGetCurrentVersions_DefaultImage(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha5.SimpleConfig{}
	provisioner := k3dprovisioner.NewProvisioner(cfg, "")

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KubernetesVersion == "" {
		t.Error("expected non-empty default KubernetesVersion")
	}
}

func TestPrepareConfigForVersion_UpdatesImage(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha5.SimpleConfig{}
	cfg.Image = "rancher/k3s:v1.30.0-k3s1"

	provisioner := k3dprovisioner.NewProvisioner(cfg, "")

	err := provisioner.PrepareConfigForVersion("Kubernetes", "v1.31.0-k3s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Image != "rancher/k3s:v1.31.0-k3s1" {
		t.Errorf("Image = %q, want %q", cfg.Image, "rancher/k3s:v1.31.0-k3s1")
	}
}

func TestKubernetesImageRef(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")

	if got := provisioner.KubernetesImageRef(); got != "rancher/k3s" {
		t.Errorf("KubernetesImageRef() = %q, want %q", got, "rancher/k3s")
	}
}

func TestDistributionImageRef_Empty(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")

	if got := provisioner.DistributionImageRef(); got != "" {
		t.Errorf("DistributionImageRef() = %q, want %q", got, "")
	}
}

func TestVersionSuffix_K3s(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewProvisioner(nil, "")

	if got := provisioner.VersionSuffix(); got != "k3s" {
		t.Errorf("VersionSuffix() = %q, want %q", got, "k3s")
	}
}
