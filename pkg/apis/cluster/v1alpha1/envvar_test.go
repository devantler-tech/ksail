package v1alpha1_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestCluster_ExpandEnvVars_EmptyStrings(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()
	cluster.ExpandEnvVars()

	// Should not panic or error with empty strings
	assert.Equal(t, "", cluster.Spec.Editor)
	assert.Equal(t, "", cluster.Spec.Cluster.DistributionConfig)
}

func TestCluster_ExpandEnvVars_NoPlaceholders(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Editor = "code --wait"
	cluster.Spec.Cluster.Connection.Kubeconfig = "~/.kube/config"
	cluster.Spec.Cluster.Connection.Context = "my-cluster"
	cluster.Spec.Cluster.DistributionConfig = "kind.yaml"
	cluster.Spec.Workload.SourceDirectory = "k8s"

	cluster.ExpandEnvVars()

	// Values without placeholders should remain unchanged
	assert.Equal(t, "code --wait", cluster.Spec.Editor)
	assert.Equal(t, "~/.kube/config", cluster.Spec.Cluster.Connection.Kubeconfig)
	assert.Equal(t, "my-cluster", cluster.Spec.Cluster.Connection.Context)
	assert.Equal(t, "kind.yaml", cluster.Spec.Cluster.DistributionConfig)
	assert.Equal(t, "k8s", cluster.Spec.Workload.SourceDirectory)
}

func TestCluster_ExpandEnvVars_BasicFields(t *testing.T) {
	t.Setenv("TEST_EDITOR", "vim")
	t.Setenv("TEST_KUBE_CONFIG", "/custom/kubeconfig")
	t.Setenv("TEST_CONTEXT", "prod-cluster")
	t.Setenv("TEST_CONFIG_DIR", "/etc/cluster")
	t.Setenv("TEST_SOURCE_DIR", "/workloads")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Editor = "${TEST_EDITOR}"
	cluster.Spec.Cluster.Connection.Kubeconfig = "${TEST_KUBE_CONFIG}"
	cluster.Spec.Cluster.Connection.Context = "${TEST_CONTEXT}"
	cluster.Spec.Cluster.DistributionConfig = "${TEST_CONFIG_DIR}/kind.yaml"
	cluster.Spec.Workload.SourceDirectory = "${TEST_SOURCE_DIR}"

	cluster.ExpandEnvVars()

	assert.Equal(t, "vim", cluster.Spec.Editor)
	assert.Equal(t, "/custom/kubeconfig", cluster.Spec.Cluster.Connection.Kubeconfig)
	assert.Equal(t, "prod-cluster", cluster.Spec.Cluster.Connection.Context)
	assert.Equal(t, "/etc/cluster/kind.yaml", cluster.Spec.Cluster.DistributionConfig)
	assert.Equal(t, "/workloads", cluster.Spec.Workload.SourceDirectory)
}

func TestCluster_ExpandEnvVars_ChatModel(t *testing.T) {
	t.Setenv("TEST_CHAT_MODEL", "gpt-4")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Chat.Model = "${TEST_CHAT_MODEL}"

	cluster.ExpandEnvVars()

	assert.Equal(t, "gpt-4", cluster.Spec.Chat.Model)
}

func TestCluster_ExpandEnvVars_LocalRegistry(t *testing.T) {
	t.Setenv("TEST_REGISTRY_USER", "testuser")
	t.Setenv("TEST_REGISTRY_PASS", "testpass")
	t.Setenv("TEST_REGISTRY_HOST", "ghcr.io")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.LocalRegistry.Registry = "${TEST_REGISTRY_USER}:${TEST_REGISTRY_PASS}@${TEST_REGISTRY_HOST}/myorg/myrepo"

	cluster.ExpandEnvVars()

	assert.Equal(t, "testuser:testpass@ghcr.io/myorg/myrepo", cluster.Spec.Cluster.LocalRegistry.Registry)
}

func TestCluster_ExpandEnvVars_VanillaOptions(t *testing.T) {
	t.Setenv("TEST_MIRRORS_DIR", "/custom/mirrors")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Vanilla.MirrorsDir = "${TEST_MIRRORS_DIR}"

	cluster.ExpandEnvVars()

	assert.Equal(t, "/custom/mirrors", cluster.Spec.Cluster.Vanilla.MirrorsDir)
}

func TestCluster_ExpandEnvVars_TalosOptions(t *testing.T) {
	t.Setenv("TEST_TALOS_CONFIG", "/custom/talosconfig")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Talos.Config = "${TEST_TALOS_CONFIG}"

	cluster.ExpandEnvVars()

	assert.Equal(t, "/custom/talosconfig", cluster.Spec.Cluster.Talos.Config)
}

func TestCluster_ExpandEnvVars_HetznerOptions(t *testing.T) {
	t.Setenv("TEST_SSH_KEY", "my-ssh-key")
	t.Setenv("TEST_NETWORK", "my-network")
	t.Setenv("TEST_PLACEMENT_GROUP", "my-placement")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Hetzner.SSHKeyName = "${TEST_SSH_KEY}"
	cluster.Spec.Cluster.Hetzner.NetworkName = "${TEST_NETWORK}"
	cluster.Spec.Cluster.Hetzner.PlacementGroup = "${TEST_PLACEMENT_GROUP}"
	// TokenEnvVar should NOT be expanded as it's the name of the env var
	cluster.Spec.Cluster.Hetzner.TokenEnvVar = "HCLOUD_TOKEN"

	cluster.ExpandEnvVars()

	assert.Equal(t, "my-ssh-key", cluster.Spec.Cluster.Hetzner.SSHKeyName)
	assert.Equal(t, "my-network", cluster.Spec.Cluster.Hetzner.NetworkName)
	assert.Equal(t, "my-placement", cluster.Spec.Cluster.Hetzner.PlacementGroup)
	// TokenEnvVar should remain unchanged
	assert.Equal(t, "HCLOUD_TOKEN", cluster.Spec.Cluster.Hetzner.TokenEnvVar)
}

func TestCluster_ExpandEnvVars_UndefinedVariables(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Editor = "${UNDEFINED_VAR}"
	cluster.Spec.Cluster.Connection.Context = "prefix-${UNDEFINED_VAR}-suffix"
	cluster.Spec.Workload.SourceDirectory = "${UNDEFINED_DIR}/k8s"

	cluster.ExpandEnvVars()

	// Undefined variables should expand to empty string
	assert.Equal(t, "", cluster.Spec.Editor)
	assert.Equal(t, "prefix--suffix", cluster.Spec.Cluster.Connection.Context)
	assert.Equal(t, "/k8s", cluster.Spec.Workload.SourceDirectory)
}

func TestCluster_ExpandEnvVars_MixedDefinedAndUndefined(t *testing.T) {
	t.Setenv("TEST_DEFINED", "value")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Editor = "${TEST_DEFINED} ${TEST_UNDEFINED}"

	cluster.ExpandEnvVars()

	// Defined should expand, undefined should become empty
	assert.Equal(t, "value ", cluster.Spec.Editor)
}

func TestCluster_ExpandEnvVars_PathWithMultipleVars(t *testing.T) {
	t.Setenv("TEST_HOME", "/home/user")
	t.Setenv("TEST_CLUSTER_NAME", "dev-cluster")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Connection.Kubeconfig = "${TEST_HOME}/.kube/${TEST_CLUSTER_NAME}/config"
	cluster.Spec.Cluster.DistributionConfig = "${TEST_HOME}/clusters/${TEST_CLUSTER_NAME}/kind.yaml"

	cluster.ExpandEnvVars()

	assert.Equal(t, "/home/user/.kube/dev-cluster/config", cluster.Spec.Cluster.Connection.Kubeconfig)
	assert.Equal(t, "/home/user/clusters/dev-cluster/kind.yaml", cluster.Spec.Cluster.DistributionConfig)
}

func TestCluster_ExpandEnvVars_ComplexRegistry(t *testing.T) {
	t.Setenv("REGISTRY_USER", "github-user")
	t.Setenv("REGISTRY_TOKEN", "ghp_secrettoken123")
	t.Setenv("REGISTRY_ORG", "my-org")
	t.Setenv("REGISTRY_REPO", "my-repo")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.LocalRegistry.Registry = "${REGISTRY_USER}:${REGISTRY_TOKEN}@ghcr.io:443/${REGISTRY_ORG}/${REGISTRY_REPO}"

	cluster.ExpandEnvVars()

	assert.Equal(t, "github-user:ghp_secrettoken123@ghcr.io:443/my-org/my-repo",
		cluster.Spec.Cluster.LocalRegistry.Registry)
}

func TestCluster_ExpandEnvVars_AllFieldsAtOnce(t *testing.T) {
	// Set all test environment variables
	t.Setenv("TEST_EDITOR", "nano")
	t.Setenv("TEST_KUBECONFIG", "/test/kubeconfig")
	t.Setenv("TEST_CONTEXT", "test-ctx")
	t.Setenv("TEST_DIST_CONFIG", "/test/kind.yaml")
	t.Setenv("TEST_REGISTRY", "localhost:5000")
	t.Setenv("TEST_SOURCE_DIR", "/test/k8s")
	t.Setenv("TEST_CHAT_MODEL", "claude")
	t.Setenv("TEST_MIRRORS_DIR", "/test/mirrors")
	t.Setenv("TEST_TALOS_CONFIG", "/test/talosconfig")
	t.Setenv("TEST_SSH_KEY", "test-ssh")
	t.Setenv("TEST_NETWORK", "test-net")
	t.Setenv("TEST_PLACEMENT", "test-placement")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Editor = "${TEST_EDITOR}"
	cluster.Spec.Cluster.Connection.Kubeconfig = "${TEST_KUBECONFIG}"
	cluster.Spec.Cluster.Connection.Context = "${TEST_CONTEXT}"
	cluster.Spec.Cluster.DistributionConfig = "${TEST_DIST_CONFIG}"
	cluster.Spec.Cluster.LocalRegistry.Registry = "${TEST_REGISTRY}"
	cluster.Spec.Workload.SourceDirectory = "${TEST_SOURCE_DIR}"
	cluster.Spec.Chat.Model = "${TEST_CHAT_MODEL}"
	cluster.Spec.Cluster.Vanilla.MirrorsDir = "${TEST_MIRRORS_DIR}"
	cluster.Spec.Cluster.Talos.Config = "${TEST_TALOS_CONFIG}"
	cluster.Spec.Cluster.Hetzner.SSHKeyName = "${TEST_SSH_KEY}"
	cluster.Spec.Cluster.Hetzner.NetworkName = "${TEST_NETWORK}"
	cluster.Spec.Cluster.Hetzner.PlacementGroup = "${TEST_PLACEMENT}"

	cluster.ExpandEnvVars()

	// Verify all fields were expanded
	assert.Equal(t, "nano", cluster.Spec.Editor)
	assert.Equal(t, "/test/kubeconfig", cluster.Spec.Cluster.Connection.Kubeconfig)
	assert.Equal(t, "test-ctx", cluster.Spec.Cluster.Connection.Context)
	assert.Equal(t, "/test/kind.yaml", cluster.Spec.Cluster.DistributionConfig)
	assert.Equal(t, "localhost:5000", cluster.Spec.Cluster.LocalRegistry.Registry)
	assert.Equal(t, "/test/k8s", cluster.Spec.Workload.SourceDirectory)
	assert.Equal(t, "claude", cluster.Spec.Chat.Model)
	assert.Equal(t, "/test/mirrors", cluster.Spec.Cluster.Vanilla.MirrorsDir)
	assert.Equal(t, "/test/talosconfig", cluster.Spec.Cluster.Talos.Config)
	assert.Equal(t, "test-ssh", cluster.Spec.Cluster.Hetzner.SSHKeyName)
	assert.Equal(t, "test-net", cluster.Spec.Cluster.Hetzner.NetworkName)
	assert.Equal(t, "test-placement", cluster.Spec.Cluster.Hetzner.PlacementGroup)
}

func TestCluster_ExpandEnvVars_InvalidSyntax(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()
	// These should NOT be expanded (invalid syntax)
	cluster.Spec.Editor = "$PLAIN_VAR"           // Missing braces
	cluster.Spec.Cluster.Connection.Context = "${" // Incomplete
	cluster.Spec.Workload.SourceDirectory = "${}" // Empty placeholder

	cluster.ExpandEnvVars()

	// Invalid syntax should remain unchanged
	assert.Equal(t, "$PLAIN_VAR", cluster.Spec.Editor)
	assert.Equal(t, "${", cluster.Spec.Cluster.Connection.Context)
	assert.Equal(t, "${}", cluster.Spec.Workload.SourceDirectory)
}

func TestCluster_ExpandEnvVars_AdjacentPlaceholders(t *testing.T) {
	t.Setenv("TEST_A", "value")
	t.Setenv("TEST_B", "123")
	t.Setenv("TEST_C", "end")

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Connection.Context = "${TEST_A}${TEST_B}${TEST_C}"

	cluster.ExpandEnvVars()

	assert.Equal(t, "value123end", cluster.Spec.Cluster.Connection.Context)
}

func TestCluster_ExpandEnvVars_PreservesStructure(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.NewCluster()
	// Set some non-string fields
	cluster.Spec.Cluster.Talos.ControlPlanes = 3
	cluster.Spec.Cluster.Talos.Workers = 5
	cluster.Spec.Workload.ValidateOnPush = true

	cluster.ExpandEnvVars()

	// Non-string fields should not be affected
	assert.Equal(t, int32(3), cluster.Spec.Cluster.Talos.ControlPlanes)
	assert.Equal(t, int32(5), cluster.Spec.Cluster.Talos.Workers)
	assert.True(t, cluster.Spec.Workload.ValidateOnPush)
}
