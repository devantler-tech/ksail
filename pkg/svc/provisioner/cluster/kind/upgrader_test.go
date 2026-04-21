package kindprovisioner_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Compile-time check that Provisioner implements Upgrader.
var _ clusterupdate.Upgrader = (*kindprovisioner.Provisioner)(nil)

func TestUpgradeKubernetes_ReturnsRecreationRequired(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha4.Cluster{}
	provisioner := kindprovisioner.NewProvisioner(cfg, "", nil, nil)

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

	cfg := &v1alpha4.Cluster{}
	provisioner := kindprovisioner.NewProvisioner(cfg, "", nil, nil)

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

	cfg := &v1alpha4.Cluster{}
	cfg.Nodes = []v1alpha4.Node{
		{Image: "kindest/node:v1.31.2"},
	}

	provisioner := kindprovisioner.NewProvisioner(cfg, "", nil, nil)

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KubernetesVersion != "v1.31.2" {
		t.Errorf("KubernetesVersion = %q, want %q", info.KubernetesVersion, "v1.31.2")
	}

	if info.DistributionVersion != "v1.31.2" {
		t.Errorf("DistributionVersion = %q, want %q", info.DistributionVersion, "v1.31.2")
	}
}

func TestGetCurrentVersions_DigestPinned(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha4.Cluster{}
	cfg.Nodes = []v1alpha4.Node{
		{
			Image: "kindest/node:v1.35.1@sha256:05d7bcdefbda08b4e038f644c4df690cdac3fba8b06f8289f30e10026720a1ab",
		},
	}

	provisioner := kindprovisioner.NewProvisioner(cfg, "", nil, nil)

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KubernetesVersion != "v1.35.1" {
		t.Errorf("KubernetesVersion = %q, want %q", info.KubernetesVersion, "v1.35.1")
	}
}

func TestGetCurrentVersions_DefaultImage(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha4.Cluster{}
	provisioner := kindprovisioner.NewProvisioner(cfg, "", nil, nil)

	info, err := provisioner.GetCurrentVersions(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KubernetesVersion == "" {
		t.Error("expected non-empty default KubernetesVersion")
	}
}

func TestPrepareConfigForVersion_UpdatesAllNodes(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha4.Cluster{}
	cfg.Nodes = []v1alpha4.Node{
		{Image: "kindest/node:v1.30.0"},
		{Image: "kindest/node:v1.30.0"},
	}

	provisioner := kindprovisioner.NewProvisioner(cfg, "", nil, nil)

	err := provisioner.PrepareConfigForVersion("Kubernetes", "v1.31.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for idx, node := range cfg.Nodes {
		if node.Image != "kindest/node:v1.31.0" {
			t.Errorf("node %d image = %q, want %q", idx, node.Image, "kindest/node:v1.31.0")
		}
	}
}

func TestKubernetesImageRef(t *testing.T) {
	t.Parallel()

	provisioner := kindprovisioner.NewProvisioner(nil, "", nil, nil)

	if got := provisioner.KubernetesImageRef(); got != "kindest/node" {
		t.Errorf("KubernetesImageRef() = %q, want %q", got, "kindest/node")
	}
}

func TestDistributionImageRef_Empty(t *testing.T) {
	t.Parallel()

	provisioner := kindprovisioner.NewProvisioner(nil, "", nil, nil)

	if got := provisioner.DistributionImageRef(); got != "" {
		t.Errorf("DistributionImageRef() = %q, want %q", got, "")
	}
}

func TestVersionSuffix_Empty(t *testing.T) {
	t.Parallel()

	provisioner := kindprovisioner.NewProvisioner(nil, "", nil, nil)

	if got := provisioner.VersionSuffix(); got != "" {
		t.Errorf("VersionSuffix() = %q, want %q", got, "")
	}
}
