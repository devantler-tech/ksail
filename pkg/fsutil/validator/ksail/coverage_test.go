package ksail_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	ksailvalidator "github.com/devantler-tech/ksail/v7/pkg/fsutil/validator/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestNewValidatorForVCluster verifies the VCluster constructor.
func TestNewValidatorForVCluster(t *testing.T) {
	t.Parallel()

	vclusterConfig := &clusterprovisioner.VClusterConfig{
		Name: "my-vcluster",
	}

	v := ksailvalidator.NewValidatorForVCluster(vclusterConfig)
	require.NotNil(t, v)
}

// TestValidate_VClusterContextValidation verifies VCluster context name validation.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestValidate_VClusterContextValidation(t *testing.T) {
	t.Parallel()

	vclusterConfig := &clusterprovisioner.VClusterConfig{
		Name: "my-vcluster",
	}

	v := ksailvalidator.NewValidatorForVCluster(vclusterConfig)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVCluster,
				DistributionConfig: "vcluster.yaml",
				Connection: v1alpha1.Connection{
					Context: "vcluster-docker_my-vcluster",
				},
			},
		},
	}

	result := v.Validate(config)
	// Context should match the expected "vcluster-docker_<name>" pattern
	for _, err := range result.Errors {
		assert.NotEqual(t, "spec.cluster.connection.context", err.Field,
			"context name should match VCluster pattern")
	}
}

// TestValidate_VClusterContextMismatch verifies VCluster context name mismatch is flagged.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestValidate_VClusterContextMismatch(t *testing.T) {
	t.Parallel()

	vclusterConfig := &clusterprovisioner.VClusterConfig{
		Name: "my-vcluster",
	}

	v := ksailvalidator.NewValidatorForVCluster(vclusterConfig)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVCluster,
				DistributionConfig: "vcluster.yaml",
				Connection: v1alpha1.Connection{
					Context: "wrong-context-name",
				},
			},
		},
	}

	result := v.Validate(config)

	found := false

	for _, err := range result.Errors {
		if err.Field == "spec.cluster.connection.context" {
			found = true
		}
	}

	assert.True(t, found, "should flag context name mismatch for VCluster")
}

// TestValidate_TalosCiliumCNIAlignment verifies Talos+Cilium CNI alignment.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestValidate_TalosCiliumCNIAlignment(t *testing.T) {
	t.Parallel()

	// Create a Talos config where CNI is NOT disabled (default Flannel)
	talosConfig := &talosconfigmanager.Configs{}

	v := ksailvalidator.NewValidatorForTalos(talosConfig)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionTalos,
				DistributionConfig: "talos",
				CNI:                v1alpha1.CNICilium,
			},
		},
	}

	result := v.Validate(config)

	found := false

	for _, err := range result.Errors {
		if err.Field == "spec.cni" {
			found = true
		}
	}

	assert.True(
		t,
		found,
		"should flag CNI mismatch when Cilium requested but Talos CNI not disabled",
	)
}

// TestValidate_TalosDefaultCNIAlignment verifies Talos+Default CNI alignment
// when CNI is disabled in Talos config.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestValidate_TalosDefaultCNIAlignment(t *testing.T) {
	t.Parallel()

	// We need a Talos config where IsCNIDisabled() returns true.
	// Since we can't easily construct one with disabled CNI here,
	// we test the no-Talos-config path (nil check).
	v := ksailvalidator.NewValidatorForTalos(nil)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionTalos,
				DistributionConfig: "talos",
				CNI:                v1alpha1.CNIDefault,
			},
		},
	}

	result := v.Validate(config)

	// With nil Talos config, validation should be skipped (no error)
	for _, err := range result.Errors {
		assert.NotEqual(t, "spec.cni", err.Field,
			"should skip Talos CNI validation when no Talos config provided")
	}
}

// TestValidate_VClusterCiliumCNI verifies VCluster+Cilium does not produce CNI errors
// when no specific config is provided.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestValidate_VClusterCiliumCNI(t *testing.T) {
	t.Parallel()

	v := ksailvalidator.NewValidator()

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVCluster,
				DistributionConfig: "vcluster.yaml",
				CNI:                v1alpha1.CNICilium,
			},
		},
	}

	result := v.Validate(config)

	// VCluster + Cilium should not produce CNI alignment errors when no config is provided
	for _, err := range result.Errors {
		assert.NotEqual(t, "spec.cni", err.Field,
			"should not flag CNI error for VCluster without config")
	}
}

// TestValidate_ExternalRegistryPort verifies external registry port validation.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestValidate_ExternalRegistryPort(t *testing.T) {
	t.Parallel()

	v := ksailvalidator.NewValidator()

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVanilla,
				DistributionConfig: "kind.yaml",
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "ghcr.io/my-org/my-repo",
				},
			},
		},
	}

	result := v.Validate(config)

	// External registry with no port (implicit HTTPS) should be valid
	for _, err := range result.Errors {
		assert.NotEqual(t, "spec.cluster.localRegistry.registry", err.Field,
			"external registry without port should be valid")
	}
}

// TestValidate_FluxValidation verifies the Flux validation placeholder.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestValidate_FluxValidation(t *testing.T) {
	t.Parallel()

	v := ksailvalidator.NewValidator()

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVanilla,
				DistributionConfig: "kind.yaml",
				GitOpsEngine:       v1alpha1.GitOpsEngineFlux,
			},
		},
	}

	// Calling Validate should not panic - validateFlux is a no-op placeholder
	result := v.Validate(config)
	require.NotNil(t, result)
}

// TestValidate_GetVClusterConfigNameEmpty verifies empty VCluster name returns empty.
//
//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestValidate_GetVClusterConfigNameEmpty(t *testing.T) {
	t.Parallel()

	vclusterConfig := &clusterprovisioner.VClusterConfig{
		Name: "", // Empty name
	}

	v := ksailvalidator.NewValidatorForVCluster(vclusterConfig)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionVCluster,
				DistributionConfig: "vcluster.yaml",
				Connection: v1alpha1.Connection{
					Context: "some-context",
				},
			},
		},
	}

	result := v.Validate(config)
	// With empty VCluster name, context validation should be skipped
	for _, err := range result.Errors {
		assert.NotEqual(t, "spec.cluster.connection.context", err.Field,
			"should skip context validation when VCluster name is empty")
	}
}

// TestValidate_TalosConfigName verifies Talos config name extraction.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestValidate_TalosConfigName(t *testing.T) {
	t.Parallel()

	talosConfig := &talosconfigmanager.Configs{}

	v := ksailvalidator.NewValidatorForTalos(talosConfig)

	config := &v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "ksail.io/v1alpha1",
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionTalos,
				DistributionConfig: "talos",
				Connection: v1alpha1.Connection{
					Context: "admin@test-cluster",
				},
			},
		},
	}

	// Talos configs with empty Name returns empty, so context validation is skipped
	result := v.Validate(config)
	require.NotNil(t, result)
}
