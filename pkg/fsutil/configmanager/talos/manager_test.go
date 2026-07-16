package talos_test

import (
	"os"
	"path/filepath"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const registryTokenPatchWithDefault = `machine:
  registries:
    config:
      registry.example.com:
        auth:
          username: user
          password: ${REGISTRY_CLUSTER_TOKEN:-forbidden-fallback}
`

func TestNewConfigManager_WithAllParameters(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("custom-talos", "my-cluster", "1.31.0", "10.6.0.0/24")

	require.NotNil(t, manager)
}

func TestNewConfigManager_WithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		patchesDir        string
		kubernetesVersion string
		networkCIDR       string
	}{
		{
			name:              "default patches dir",
			patchesDir:        "",
			kubernetesVersion: "1.30.0",
			networkCIDR:       "10.5.0.0/24",
		},
		{
			name:              "default kubernetes version",
			patchesDir:        "talos",
			kubernetesVersion: "",
			networkCIDR:       "10.5.0.0/24",
		},
		{
			name:              "default network CIDR",
			patchesDir:        "talos",
			kubernetesVersion: "1.32.0",
			networkCIDR:       "",
		},
		{
			name:              "all defaults",
			patchesDir:        "",
			kubernetesVersion: "",
			networkCIDR:       "",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			manager := talos.NewConfigManager(
				testCase.patchesDir,
				"test-cluster",
				testCase.kubernetesVersion,
				testCase.networkCIDR,
			)

			require.NotNil(t, manager)
		})
	}
}

func TestConfigManager_WithEnvLookupKeepsEmptySourceAuthoritative(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(clusterDir, "registry-auth.yaml"),
			[]byte(registryTokenPatchWithDefault),
			0o600,
		),
	)

	manager := talos.NewConfigManager(tmpDir, "test-cluster", "", "").
		WithEnvLookup(func(name string) (string, bool) {
			return "", name == "REGISTRY_CLUSTER_TOKEN"
		})

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	auth := configs.ControlPlane().RegistryAuthConfigs()["registry.example.com"]
	require.NotNil(t, auth)
	assert.Empty(t, auth.Password())
}

func TestConfigManager_WithAdditionalPatches(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("talos", "test", "1.32.0", "10.5.0.0/24")

	patches := []talos.Patch{
		{
			Path:    "runtime-patch",
			Scope:   talos.PatchScopeCluster,
			Content: []byte("machine:\n  network:\n    hostname: test"),
		},
	}

	result := manager.WithAdditionalPatches(patches)

	assert.NotNil(t, result)
	assert.Equal(t, manager, result, "WithAdditionalPatches should return the same manager")
}

func TestConfigManager_ValidatePatchDirectory_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("nonexistent-dir", "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.NoError(t, err)
	assert.Contains(t, warning, "Patch directory")
	assert.Contains(t, warning, "not found")
}

func TestConfigManager_ValidatePatchDirectory_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.NoError(t, err)
	assert.Empty(t, warning)
}

func TestConfigManager_ValidatePatchDirectory_ValidYAMLFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	validYAML := []byte("machine:\n  network:\n    hostname: test\n")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "patch.yaml"), validYAML, 0o600))

	manager := talos.NewConfigManager(tmpDir, "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.NoError(t, err)
	assert.Empty(t, warning)
}

func TestConfigManager_ValidatePatchDirectory_InvalidYAMLFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	invalidYAML := []byte("invalid: yaml: content: [")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "bad.yaml"), invalidYAML, 0o600))

	manager := talos.NewConfigManager(tmpDir, "test", "1.32.0", "10.5.0.0/24")

	warning, err := manager.ValidatePatchDirectory()

	require.Error(t, err)
	assert.Empty(t, warning)
}

func TestConfigManager_LoadConfig_BasicLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "test-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})

	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "test-cluster", configs.GetClusterName())
}

func TestConfigManager_LoadConfig_Caching(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "cached-cluster", "1.32.0", "10.5.0.0/24")

	// First load
	configs1, err1 := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err1)
	require.NotNil(t, configs1)

	// Second load should return cached result
	configs2, err2 := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err2)
	require.NotNil(t, configs2)

	// Should be the same instance
	assert.Same(t, configs1, configs2, "LoadConfig should cache results")
}

func TestConfigManager_LoadConfig_WithPatches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	patchYAML := []byte("machine:\n  network:\n    hostname: test-node\n")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "hostname.yaml"), patchYAML, 0o600))

	manager := talos.NewConfigManager(tmpDir, "patched-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})

	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "patched-cluster", configs.GetClusterName())
}

func TestConfigManager_ValidateConfigs_ValidConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "valid-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.ValidateConfigs()

	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "valid-cluster", configs.GetClusterName())
}

func TestConfigManager_ValidateConfigs_WithInvalidYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	invalidYAML := []byte("invalid: yaml: [")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "bad.yaml"), invalidYAML, 0o600))

	manager := talos.NewConfigManager(tmpDir, "invalid-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.ValidateConfigs()

	require.Error(t, err)
	assert.Nil(t, configs)
}

func TestConfigManager_ValidateConfigs_NonExistentPatchDir(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("nonexistent", "test", "1.32.0", "10.5.0.0/24")

	// Should still succeed since patches are optional
	configs, err := manager.ValidateConfigs()

	require.NoError(t, err)
	require.NotNil(t, configs)
}

func TestConfigManager_WithVersionContract_PropagatesContractToGeneratedConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// An explicit lower contract (TalosVersion1_11) must not emit grubUseUKICmdline,
	// which is gated at contracts greater than 1.11.
	manager111 := talos.NewConfigManager(tmpDir, "test-cluster", "1.32.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_11)
	configs111, err := manager111.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	cp111 := configs111.ControlPlane()
	require.NotNil(t, cp111)

	cfgYAML111, err := cp111.EncodeString()
	require.NoError(t, err)
	assert.NotContains(t, cfgYAML111, "grubUseUKICmdline",
		"TalosVersion1_11 should not emit grubUseUKICmdline")

	// The default contract (TalosVersion1_12) must emit grubUseUKICmdline, which the
	// default Hetzner bootstrap ISO (Talos 1.12.4) understands.
	managerDefault := talos.NewConfigManager(tmpDir, "test-cluster", "1.32.0", "10.5.0.0/24")
	configsDefault, err := managerDefault.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	cpDefault := configsDefault.ControlPlane()
	require.NotNil(t, cpDefault)

	cfgYAMLDefault, err := cpDefault.EncodeString()
	require.NoError(t, err)
	assert.Contains(
		t,
		cfgYAMLDefault,
		"grubUseUKICmdline",
		"default contract (TalosVersion1_12) should emit grubUseUKICmdline; got:\n%s",
		cfgYAMLDefault,
	)
}

func TestConfigManager_WithVersionContract_InvalidatesCachedConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	manager := talos.NewConfigManager(tmpDir, "test-cluster", "1.32.0", "10.5.0.0/24")

	// First load caches the config.
	configs1, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs1)

	// Changing the version contract must invalidate the cache.
	manager.WithVersionContract(talosconfig.TalosVersion1_13)

	configs2, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs2)

	assert.NotSame(t, configs1, configs2,
		"WithVersionContract should invalidate the cached config so Load regenerates it")
}

func TestConfigManager_Load_MigratesLegacyCNIPatchForMultiDocumentConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "disable-default-cni.yaml"),
		[]byte("cluster:\n  network:\n    cni:\n      name: none\n"),
		0o600,
	))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	controlPlane := configs.ControlPlane()
	require.NotNil(t, controlPlane)
	require.NotNil(t, controlPlane.K8sNetworkConfig())
	assert.Nil(t, controlPlane.K8sFlannelCNIConfig())
}

func TestConfigManager_Load_MigratesVariantLegacyCNIPatchForMultiDocumentConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	// A hand-written variant: a leading comment means the patch is not byte-for-byte the
	// canonical disable-CNI patch, so it must be detected structurally (ksail#6167).
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "disable-default-cni.yaml"),
		[]byte(
			"# disable the built-in Flannel CNI\ncluster:\n  network:\n    cni:\n      name: none\n",
		),
		0o600,
	))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	controlPlane := configs.ControlPlane()
	require.NotNil(t, controlPlane)
	require.NotNil(t, controlPlane.K8sNetworkConfig())
	assert.Nil(t, controlPlane.K8sFlannelCNIConfig(),
		"a variant disable-CNI patch must still drop the Flannel document under Talos 1.14")
	assert.True(t, configs.IsCNIDisabled())
}

func TestConfigManager_Load_MigratesLegacyAPIServerPatchWithoutOIDC(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyAPIServer := []byte(`cluster:
  apiServer:
    certSANs:
      - api.example.com
    extraArgs:
      audit-log-maxage: "30"
`)
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "api-server.yaml"),
		legacyAPIServer,
		0o600,
	))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	apiServer := configs.ControlPlane().K8sAPIServerConfig()
	assert.Contains(t, apiServer.CertSANs(), "api.example.com")
	assert.Equal(t, map[string][]string{"audit-log-maxage": {"30"}}, apiServer.ExtraArgs())
}

func TestConfigManager_Load_RejectsDuplicateLegacyAPIServerDocuments(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyAPIServer := []byte(`cluster:
  apiServer:
    certSANs:
      - api.example.com
---
cluster:
  apiServer:
    extraArgs:
      audit-log-maxage: "30"
`)
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "api-server.yaml"),
		legacyAPIServer,
		0o600,
	))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	_, err := manager.Load(configmanager.LoadOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "multiple legacy cluster.apiServer documents are not supported")
}

func TestConfigManager_Load_PreservesLegacyAPIServerPatchBeforeMultiDocumentConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyAPIServer := []byte(`cluster:
  apiServer:
    certSANs:
      - api.example.com
    extraArgs:
      audit-log-maxage: "30"
`)
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "api-server.yaml"),
		legacyAPIServer,
		0o600,
	))

	manager := talos.NewConfigManager(tmpDir, "talos-113", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_13)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	patches := configs.Patches()
	require.Len(t, patches, 1)
	assert.Equal(t, legacyAPIServer, patches[0].Content)

	apiServer := configs.ControlPlane().K8sAPIServerConfig()
	assert.Contains(t, apiServer.CertSANs(), "api.example.com")
	assert.Equal(t, map[string][]string{"audit-log-maxage": {"30"}}, apiServer.ExtraArgs())
}

func TestConfigManager_Load_MigratesLegacyOIDCPatchForMultiDocumentConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyOIDC := []byte(`machine:
  files:
    - path: "/etc/kubernetes/oidc/ca.crt"
      content: |
        -----BEGIN CERTIFICATE-----
        test-ca
        -----END CERTIFICATE-----
---
cluster:
  apiServer:
    extraArgs:
      oidc-issuer-url: "https://dex.example.com"
      oidc-client-id: "ksail"
      oidc-username-claim: "email"
      oidc-username-prefix: "oidc:"
      oidc-groups-claim: "groups"
      oidc-groups-prefix: "oidc:"
      oidc-ca-file: "/etc/kubernetes/oidc/ca.crt"
`)
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "oidc.yaml"), legacyOIDC, 0o600))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	controlPlane := configs.ControlPlane()
	require.NotNil(t, controlPlane)
	authConfig := controlPlane.K8sAuthenticationConfig().Configuration()
	jwt, found := authConfig["jwt"].([]any)
	require.True(t, found)
	require.Len(t, jwt, 1)
	authenticator, found := jwt[0].(map[string]any)
	require.True(t, found)
	issuer, found := authenticator["issuer"].(map[string]any)
	require.True(t, found)
	assert.Equal(t, "https://dex.example.com", issuer["url"])
	assert.Equal(t, []any{"ksail"}, issuer["audiences"])
	assert.Contains(t, issuer["certificateAuthority"], "test-ca")
	assert.Empty(t, controlPlane.K8sAPIServerConfig().ExtraArgs())
}

func TestConfigManager_Load_RejectsUnsupportedLegacyOIDCExtraArg(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyOIDC := []byte(`cluster:
  apiServer:
    extraArgs:
      oidc-issuer-url: "https://dex.example.com"
      oidc-client-id: "ksail"
      oidc-required-claim: "tenant=engineering"
`)
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "oidc.yaml"), legacyOIDC, 0o600))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	_, err := manager.Load(configmanager.LoadOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, `unsupported legacy OIDC extra argument "oidc-required-claim"`)
}

func TestConfigManager_Load_RejectsOrphanedLegacyOIDCExtraArg(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyOIDC := []byte(`cluster:
  apiServer:
    extraArgs:
      oidc-required-claim: "tenant=engineering"
`)
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "oidc.yaml"), legacyOIDC, 0o600))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	_, err := manager.Load(configmanager.LoadOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, `unsupported legacy OIDC extra argument "oidc-required-claim"`)
}

func TestConfigManager_Load_MigratesLegacyOIDCAndAPIServerFields(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyOIDC := []byte(`cluster:
  apiServer:
    certSANs:
      - api.example.com
    extraArgs:
      feature-gates: MutatingAdmissionPolicy=true
      oidc-issuer-url: "https://dex.example.com"
      oidc-client-id: "ksail"
`)
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "oidc.yaml"), legacyOIDC, 0o600))

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	apiServer := configs.ControlPlane().K8sAPIServerConfig()
	assert.Contains(t, apiServer.CertSANs(), "api.example.com")
	assert.Equal(t, map[string][]string{
		"feature-gates": {"MutatingAdmissionPolicy=true"},
	}, apiServer.ExtraArgs())
}

const legacyStructuredAPIServerPatch = `cluster:
  apiServer:
    admissionControl:
      - name: EventRateLimit
        configuration:
          apiVersion: eventratelimit.admission.k8s.io/v1alpha1
          kind: Configuration
          limits:
            - type: Server
              qps: 100
              burst: 200
    auditPolicy:
      apiVersion: audit.k8s.io/v1
      kind: Policy
      rules:
        - level: RequestResponse
    authorizationConfig:
      - name: custom-webhook
        type: Webhook
        webhook:
          timeout: 3s
          subjectAccessReviewVersion: v1
          matchConditionSubjectAccessReviewVersion: v1
          failurePolicy: Deny
          connectionInfo:
            type: InClusterConfig
`

func TestConfigManager_Load_MigratesLegacyStructuredAPIServerFields(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(clusterDir, "structured-api-server.yaml"),
			[]byte(legacyStructuredAPIServerPatch),
			0o600,
		),
	)

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	controlPlane := configs.ControlPlane()
	require.NotNil(t, controlPlane)
	assertMigratedStructuredAPIServerFields(t, controlPlane)
}

func assertMigratedStructuredAPIServerFields(
	t *testing.T,
	controlPlane talosconfig.Provider,
) {
	t.Helper()

	admissionConfigs := make(map[string]map[string]any)
	for _, config := range controlPlane.K8sAdmissionControlPluginConfigs() {
		admissionConfigs[config.Name()] = config.Configuration()
	}

	assert.Equal(t, map[string]any{
		"apiVersion": "eventratelimit.admission.k8s.io/v1alpha1",
		"kind":       "Configuration",
		"limits": []any{
			map[string]any{"type": "Server", "qps": 100, "burst": 200},
		},
	}, admissionConfigs["EventRateLimit"])
	assert.Equal(t, map[string]any{
		"apiVersion": "audit.k8s.io/v1",
		"kind":       "Policy",
		"rules": []any{
			map[string]any{"level": "RequestResponse"},
		},
	}, controlPlane.K8sAuditPolicyConfig().Configuration())

	authorizers := make(map[string]struct {
		kind    string
		webhook map[string]any
	})
	for _, config := range controlPlane.K8sAuthorizerConfigs() {
		authorizers[config.Name()] = struct {
			kind    string
			webhook map[string]any
		}{kind: config.Type(), webhook: config.Webhook()}
	}

	require.Len(t, authorizers, 3)
	assert.Equal(t, "Node", authorizers["node"].kind)
	assert.Equal(t, "RBAC", authorizers["rbac"].kind)
	assert.Equal(t, "Webhook", authorizers["custom-webhook"].kind)
	assert.Equal(t, map[string]any{
		"timeout":                    "3s",
		"subjectAccessReviewVersion": "v1",
		"matchConditionSubjectAccessReviewVersion": "v1",
		"failurePolicy": "Deny",
		"connectionInfo": map[string]any{
			"type": "InClusterConfig",
		},
	}, authorizers["custom-webhook"].webhook)
}

func TestConfigManager_Load_RejectsLegacyAPIServerExtraVolumesForMultiDocumentConfig(
	t *testing.T,
) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyAPIServer := []byte(`cluster:
  apiServer:
    extraVolumes:
      - hostPath: /var/lib/example
        mountPath: /var/lib/example
        readonly: true
`)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "extra-volumes.yaml"), legacyAPIServer, 0o600),
	)

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	_, err := manager.Load(configmanager.LoadOptions{})
	require.Error(t, err)
	require.ErrorContains(t, err, `field "extraVolumes"`)
	require.ErrorContains(t, err, "has no Talos 1.14 KubeAPIServerConfig equivalent")
}

func TestConfigManager_Load_MigratesCustomNamedDefaultAuthorizers(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, talos.PatchSubdirCluster)
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	legacyAuthorizers := []byte(`cluster:
  apiServer:
    authorizationConfig:
      - type: Node
        name: custom-node
      - type: RBAC
        name: custom-rbac
`)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "authorizers.yaml"), legacyAuthorizers, 0o600),
	)

	manager := talos.NewConfigManager(tmpDir, "talos-114", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14)

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	authorizers := make(map[string]string)
	for _, config := range configs.ControlPlane().K8sAuthorizerConfigs() {
		authorizers[config.Name()] = config.Type()
	}

	assert.Equal(t, map[string]string{
		"custom-node": "Node",
		"custom-rbac": "RBAC",
	}, authorizers)
}
