package talos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talos "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sharedOIDCWithCA = "oidc-issuer-url: https://dex.example.com\n      oidc-client-id: shared\n" +
	"      oidc-ca-file: /etc/oidc.crt"

func TestKubernetesPatchSetRejectsJSON6902AtMigration(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"yaml":                "- op: replace\n  path: /cluster/network/cni/name\n  value: none\n",
		"json":                `[{"op":"replace","path":"/cluster/apiServer/extraArgs","value":{"audit-log-maxage":"30"}}]`,
		"move":                `[{"op":"move","from":"/cluster/apiServer","path":"/machine/ignored"}]`,
		"copy root":           `[{"op":"copy","from":"","path":"/copy"}]`,
		"replace root":        `[{"op":"replace","path":"","value":{}}]`,
		"hostname":            `[{"op":"add","path":"/machine/network/hostname","value":"node"}]`,
		"malformed operation": `[{"op":"invalid"}]`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			patches := []talos.Patch{{Path: "variant.yaml", Content: []byte(content)}}
			_, err := talos.MigrateKubernetesPatchesForContract(
				patches,
				talosconfig.TalosVersion1_14,
			)
			require.Error(t, err)
			require.ErrorContains(t, err, "variant.yaml")
			require.ErrorContains(t, err, "RFC 6902")
			require.ErrorContains(t, err, "strategic-merge")
			preserved, err := talos.MigrateKubernetesPatchesForContract(
				patches,
				talosconfig.TalosVersion1_13,
			)
			require.NoError(t, err)
			assert.Equal(t, patches, preserved)
		})
	}
}

func TestKubernetesPatchSetPreservesDocumentOrder(t *testing.T) {
	t.Parallel()
	configs, err := loadVariantPatches(t, map[string]string{
		"cluster/api.yaml": `cluster:
  apiServer:
    certSANs: [first.example.com]
    extraArgs:
      audit-log-maxage: "10"
---
apiVersion: v1alpha1
kind: KubeAPIServerConfig
extraArgs:
  audit-log-maxage: "20"
  audit-log-maxbackup: "5"
---
cluster:
  apiServer:
    certSANs: [last.example.com]
    extraArgs:
      audit-log-maxage: "30"
`,
	}, nil)
	require.NoError(t, err)

	api := configs.ControlPlane().K8sAPIServerConfig()
	assert.Contains(t, api.CertSANs(), "first.example.com")
	assert.Contains(t, api.CertSANs(), "last.example.com")
	assert.Equal(t, []string{"30"}, api.ExtraArgs()["audit-log-maxage"])
	assert.Equal(t, []string{"5"}, api.ExtraArgs()["audit-log-maxbackup"])
}

func TestKubernetesPatchSetResolvesSplitOIDC(t *testing.T) {
	t.Parallel()

	for _, reverse := range []bool{false, true} {
		name := "issuer first"
		if reverse {
			name = "client first"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			issuer := legacyOIDCArgs("oidc-issuer-url: https://dex.example.com")

			client := legacyOIDCArgs("oidc-client-id: ksail\n      oidc-username-claim: email")
			if reverse {
				issuer, client = client, issuer
			}

			configs, err := loadVariantPatches(t, map[string]string{
				"control-planes/01.yaml": issuer, "control-planes/02.yaml": client,
			}, nil)
			require.NoError(t, err)
			assertJWTIssuer(t, configs.ControlPlane(), "ksail", "")
			assert.Empty(t, configs.ControlPlane().K8sAPIServerConfig().ExtraArgs())
			assert.Nil(t, configs.Worker().K8sAuthenticationConfig())
		})
	}
}

func TestKubernetesPatchSetResolvesOIDCByScopeAndRuntimeOrder(t *testing.T) {
	t.Parallel()
	configs, err := loadVariantPatches(t, map[string]string{
		"cluster/oidc.yaml": legacyOIDCArgs(
			"oidc-issuer-url: https://dex.example.com\n      oidc-client-id: shared",
		),
		"control-planes/client.yaml": legacyOIDCArgs("oidc-client-id: control-plane"),
		"workers/client.yaml":        legacyOIDCArgs("oidc-client-id: worker"),
	}, []talos.Patch{{
		Path: "runtime shared", Scope: talos.PatchScopeCluster,
		Content: []byte(legacyOIDCArgs("oidc-client-id: runtime")),
	}})
	require.NoError(t, err)
	assertJWTIssuer(t, configs.ControlPlane(), "control-plane", "")
	assertJWTIssuer(t, configs.Worker(), "worker", "")
}

func TestKubernetesPatchSetRejectsCrossScopeOIDCCompletion(t *testing.T) {
	t.Parallel()
	_, err := loadVariantPatches(t, map[string]string{
		"control-planes/issuer.yaml": legacyOIDCArgs("oidc-issuer-url: https://dex.example.com"),
		"workers/client.yaml":        legacyOIDCArgs("oidc-client-id: worker"),
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "issuer URL and client ID are required")
}

func loadVariantPatches(
	t *testing.T,
	files map[string]string,
	additional []talos.Patch,
) (*talos.Configs, error) {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	configs, err := talos.NewConfigManager(dir, "variants", "1.36.0", "10.5.0.0/24").
		WithVersionContract(talosconfig.TalosVersion1_14).WithAdditionalPatches(additional).
		Load(configmanager.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("load variant patches: %w", err)
	}

	return configs, nil
}

func legacyOIDCArgs(args string) string {
	return "cluster:\n  apiServer:\n    extraArgs:\n      " + args + "\n"
}

func assertJWTIssuer(
	t *testing.T,
	provider talosconfig.Provider,
	audience, certificateAuthority string,
) {
	t.Helper()

	auth := provider.K8sAuthenticationConfig().Configuration()
	jwt, found := auth["jwt"].([]any)
	require.True(t, found)
	require.Len(t, jwt, 1)
	authenticator, found := jwt[0].(map[string]any)
	require.True(t, found)
	issuer, found := authenticator["issuer"].(map[string]any)
	require.True(t, found)
	assert.Equal(t, "https://dex.example.com", issuer["url"])
	assert.Equal(t, []any{audience}, issuer["audiences"])

	if certificateAuthority != "" {
		assert.Equal(t, certificateAuthority+"\n", issuer["certificateAuthority"])
	}
}

func TestKubernetesPatchSetResolvesRoleCertificateOverrides(t *testing.T) {
	t.Parallel()
	configs, err := loadVariantPatches(t, map[string]string{
		"cluster/oidc.yaml": legacyOIDCArgs(
			sharedOIDCWithCA,
		),
		"cluster/ca.yaml": oidcCAFile(
			"shared-ca",
			"overwrite",
		),
		"control-planes/ca.yaml": oidcCAFile(
			"control-plane-ca",
			"overwrite",
		),
	}, nil)
	require.NoError(t, err)
	assertJWTIssuer(t, configs.ControlPlane(), "shared", "control-plane-ca")
	assertJWTIssuer(t, configs.Worker(), "shared", "shared-ca")
}

func TestKubernetesPatchSetRejectsAmbiguousOIDC(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, patch, want string }{
		{
			"empty client",
			legacyOIDCArgs(`oidc-client-id: ""`),
			"issuer URL and client ID are required",
		},
		{"list client", legacyOIDCArgs(`oidc-client-id: [one, two]`), "must be strings"},
		{
			"structured auth",
			"apiVersion: v1alpha1\nkind: KubeAuthenticationConfig\nconfiguration: {}\n",
			"overlap KubeAuthenticationConfig",
		},
		{"append CA", oidcCAFile("appended", "append"), "complete CA content"},
		{"empty CA", oidcCAFile("", "overwrite"), "CA content is missing"},
		{"delete API server", "cluster:\n  apiServer:\n    $patch: delete\n", "deletion"},
		{
			"delete CA by selector",
			"machine:\n  files:\n    - op: overwrite\n      $patch: delete\n",
			"deletion",
		},
		{"delete machine", "machine:\n  $patch: delete\n", "deletion"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadVariantPatches(t, map[string]string{
				"cluster/oidc.yaml": legacyOIDCArgs(
					sharedOIDCWithCA,
				),
				"cluster/ca.yaml": oidcCAFile(
					"shared-ca",
					"overwrite",
				),
				"control-planes/override.yaml": testCase.patch,
			}, nil)
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestKubernetesPatchSetPreservesEmptyFileList(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"null", "[]"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			configs, err := loadVariantPatches(t, map[string]string{
				"cluster/oidc.yaml": legacyOIDCArgs(
					sharedOIDCWithCA,
				),
				"cluster/ca.yaml": oidcCAFile(
					"shared-ca",
					"overwrite",
				),
				"control-planes/empty.yaml": "machine:\n  files: " + value + "\n",
			}, nil)
			require.NoError(t, err)
			assertJWTIssuer(t, configs.ControlPlane(), "shared", "shared-ca")
		})
	}
}

func TestKubernetesPatchSetPreservesInputAndSplitDocumentOIDC(t *testing.T) {
	t.Parallel()

	content := legacyOIDCArgs(
		"oidc-client-id: shared",
	) + "---\n" + legacyOIDCArgs(
		"oidc-issuer-url: https://dex.example.com",
	)
	original := []talos.Patch{{Path: "split.yaml", Content: []byte(content)}}
	first, err := talos.MigrateKubernetesPatchesForContract(original, talosconfig.TalosVersion1_14)
	require.NoError(t, err)
	second, err := talos.MigrateKubernetesPatchesForContract(original, talosconfig.TalosVersion1_14)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, content, string(original[0].Content))
	configs, err := loadVariantPatches(
		t,
		map[string]string{"control-planes/split.yaml": content},
		nil,
	)
	require.NoError(t, err)
	assertJWTIssuer(t, configs.ControlPlane(), "shared", "")
}

func oidcCAFile(content, operation string) string {
	return "machine:\n  files:\n    - path: /etc/oidc.crt\n      op: " + operation +
		"\n      content: \"" + content + "\"\n"
}

func TestKubernetesPatchSetPreservesDeletionSelectorOrder(t *testing.T) {
	t.Parallel()
	configs, err := loadVariantPatches(t, map[string]string{
		"cluster/files.yaml": `machine:
  files:
    - path: /first
      op: overwrite
      content: first
    - path: /second
      op: create
      content: second
---
machine:
  files:
    - op: create
      path: /first
      $patch: delete
`,
	}, nil)
	require.NoError(t, err)
	files, err := configs.ControlPlane().Machine().Files()
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, "/first", files[0].Path())
}

func TestKubernetesPatchSetRejectsWorkerOnlyCertificate(t *testing.T) {
	t.Parallel()
	_, err := loadVariantPatches(t, map[string]string{
		"control-planes/oidc.yaml": legacyOIDCArgs(sharedOIDCWithCA),
		"workers/ca.yaml":          oidcCAFile("worker-ca", "overwrite"),
	}, nil)
	require.ErrorContains(t, err, "CA content is missing")
}

func TestKubernetesPatchSetRejectsIncompleteSharedOIDC(t *testing.T) {
	t.Parallel()
	_, err := loadVariantPatches(t, map[string]string{
		"cluster/issuer.yaml":        legacyOIDCArgs("oidc-issuer-url: https://dex.example.com"),
		"control-planes/client.yaml": legacyOIDCArgs("oidc-client-id: control-plane"),
	}, nil)
	require.ErrorContains(t, err, "shared settings must be complete")
	require.ErrorContains(t, err, "control-planes/")
}

func TestKubernetesPatchSetRejectsCrossDocumentAliases(t *testing.T) {
	t.Parallel()

	patches := []talos.Patch{{Path: "aliases.yaml", Content: []byte(`&base
cluster:
  apiServer:
    extraArgs:
      audit-log-maxage: "10"
---
*base
`)}}

	require.NotPanics(t, func() {
		_, err := talos.MigrateKubernetesPatchesForContract(patches, talosconfig.TalosVersion1_14)
		require.ErrorContains(t, err, "aliases.yaml")
		require.ErrorContains(t, err, "alias")
	})
}
